// Package service — бизнес-логика: аутентификация, команды, задачи, права доступа.
package service

import (
	"context"
	"strings"
	"time"

	"taskmanager/internal/cache"
	"taskmanager/internal/domain"
	"taskmanager/internal/repository"
)

type Service struct {
	repo      *repository.Repo
	cache     cache.TaskCache
	jwtSecret []byte
	jwtTTL    time.Duration
}

func New(repo *repository.Repo, c cache.TaskCache, jwtSecret []byte, jwtTTL time.Duration) *Service {
	if c == nil {
		c = cache.Nop{}
	}
	return &Service{repo: repo, cache: c, jwtSecret: jwtSecret, jwtTTL: jwtTTL}
}

// requireMember: для операций, привязанных к team_id. Не участник — 403.
func (s *Service) requireMember(ctx context.Context, teamID, userID int64) (domain.Role, error) {
	role, err := s.repo.GetMemberRole(ctx, teamID, userID)
	if err != nil {
		if errorsIs(err, domain.ErrNotFound) {
			return "", domain.Forbiddenf("you are not a member of this team")
		}
		return "", err
	}
	return role, nil
}

func validateName(field, v string, max int) error {
	if v == "" {
		return domain.Validationf("%s is required", field)
	}
	if len([]rune(v)) > max {
		return domain.Validationf("%s must be at most %d characters", field, max)
	}
	return nil
}

func trim(s string) string { return strings.TrimSpace(s) }
