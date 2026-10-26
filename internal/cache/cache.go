// Package cache — кеш списков задач в Redis с версионной инвалидацией и circuit breaker.
//
// Схема: ключ списка = tasks:list:{team}:{version}:{filters}, где version хранится в
// tasks:ver:{team}. Инвалидация записывает новую версию, и все прежние ключи команды
// становятся недостижимыми (затем истекают по TTL). Это атомарно и не требует SCAN/KEYS.
// Если Redis недоступен, сервис продолжает работать напрямую с MySQL.
package cache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// TaskCache — кеш списков задач.
type TaskCache interface {
	// Load возвращает слот (ключ, под которым нужно сохранить результат) и данные.
	// Пустой слот — кеш недоступен, Store вызывать не нужно. data == nil — промах.
	Load(ctx context.Context, teamID int64, filterKey string) (slot string, data []byte)
	Store(ctx context.Context, slot string, data []byte)
	Invalidate(ctx context.Context, teamID int64)
}

// Nop — заглушка без кеширования (тесты, отключённый Redis).
type Nop struct{}

func (Nop) Load(context.Context, int64, string) (string, []byte) { return "", nil }
func (Nop) Store(context.Context, string, []byte)                {}
func (Nop) Invalidate(context.Context, int64)                    {}

const (
	verTTL           = time.Hour
	breakerThreshold = 5
	breakerCooldown  = 30 * time.Second
)

// Redis — реализация TaskCache.
type Redis struct {
	rdb *redis.Client
	ttl time.Duration
	log *slog.Logger
	br  breaker
}

func NewRedis(rdb *redis.Client, ttl time.Duration, log *slog.Logger) *Redis {
	return &Redis{rdb: rdb, ttl: ttl, log: log}
}

func verKey(teamID int64) string { return fmt.Sprintf("tasks:ver:%d", teamID) }

func (c *Redis) Load(ctx context.Context, teamID int64, filterKey string) (string, []byte) {
	if !c.br.allow() {
		return "", nil
	}
	ver, err := c.rdb.Get(ctx, verKey(teamID)).Result()
	if errors.Is(err, redis.Nil) {
		ver, err = "0", nil
	}
	if err != nil {
		c.fail("load version", err)
		return "", nil
	}
	slot := fmt.Sprintf("tasks:list:%d:%s:%s", teamID, ver, filterKey)
	data, err := c.rdb.Get(ctx, slot).Bytes()
	if errors.Is(err, redis.Nil) {
		c.br.success()
		return slot, nil
	}
	if err != nil {
		c.fail("load list", err)
		return "", nil
	}
	c.br.success()
	return slot, data
}

func (c *Redis) Store(ctx context.Context, slot string, data []byte) {
	if slot == "" {
		return
	}
	if err := c.rdb.Set(ctx, slot, data, c.ttl).Err(); err != nil {
		c.fail("store list", err)
	}
}

// Invalidate вызывается после успешного коммита изменения задач команды.
func (c *Redis) Invalidate(ctx context.Context, teamID int64) {
	v := strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := c.rdb.Set(ctx, verKey(teamID), v, verTTL).Err(); err != nil {
		c.fail("invalidate", err)
	}
}

func (c *Redis) fail(op string, err error) {
	c.br.failure()
	c.log.Warn("redis cache error", "op", op, "error", err)
}

// breaker — простой circuit breaker: после N подряд ошибок кеш отключается на cooldown.
type breaker struct {
	mu        sync.Mutex
	fails     int
	openUntil time.Time
}

func (b *breaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return time.Now().After(b.openUntil)
}

func (b *breaker) success() {
	b.mu.Lock()
	b.fails = 0
	b.mu.Unlock()
}

func (b *breaker) failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails++
	if b.fails >= breakerThreshold {
		b.openUntil = time.Now().Add(breakerCooldown)
		b.fails = 0
	}
}
