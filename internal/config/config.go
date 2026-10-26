// Package config читает конфигурацию приложения из переменных окружения.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config — все настройки сервиса. Хардкода секретов нет: значения берутся из окружения.
type Config struct {
	HTTPAddr       string
	MySQLDSN       string
	RedisAddr      string
	RedisPassword  string
	RedisDB        int
	JWTSecret      string
	JWTTTL         time.Duration
	CacheTTL       time.Duration
	RateLimitRPS   float64
	RateLimitBurst int
}

// Load собирает и валидирует конфигурацию.
func Load() (*Config, error) {
	c := &Config{
		HTTPAddr:      env("HTTP_ADDR", ":8080"),
		MySQLDSN:      os.Getenv("MYSQL_DSN"),
		RedisAddr:     env("REDIS_ADDR", "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
	}
	if c.MySQLDSN == "" {
		return nil, errors.New("MYSQL_DSN is required")
	}
	if c.JWTSecret == "" {
		return nil, errors.New("JWT_SECRET is required")
	}
	var err error
	if c.RedisDB, err = envInt("REDIS_DB", 0); err != nil {
		return nil, err
	}
	if c.RateLimitBurst, err = envInt("RATE_LIMIT_BURST", 40); err != nil {
		return nil, err
	}
	if c.JWTTTL, err = envDuration("JWT_TTL", 24*time.Hour); err != nil {
		return nil, err
	}
	if c.CacheTTL, err = envDuration("CACHE_TTL", 5*time.Minute); err != nil {
		return nil, err
	}
	rps, err := envInt("RATE_LIMIT_RPS", 20)
	if err != nil {
		return nil, err
	}
	c.RateLimitRPS = float64(rps)
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}
