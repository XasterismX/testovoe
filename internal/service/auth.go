package service

import (
	"context"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"taskmanager/internal/domain"
)

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 255 {
		return "", domain.Validationf("email is invalid")
	}
	return email, nil
}

func (s *Service) Register(ctx context.Context, email, name, password string) (*domain.User, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return nil, err
	}
	name = trim(name)
	if err := validateName("name", name, 255); err != nil {
		return nil, err
	}
	if len(password) < 8 || len(password) > 72 {
		return nil, domain.Validationf("password must be 8..72 bytes long")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u, err := s.repo.CreateUser(ctx, email, string(hash), name)
	if err != nil {
		if errorsIs(err, domain.ErrConflict) {
			return nil, domain.Conflictf("email is already registered")
		}
		return nil, err
	}
	return u, nil
}

// Login проверяет пароль и выдаёт JWT (HS256).
func (s *Service) Login(ctx context.Context, email, password string) (string, time.Time, error) {
	invalid := domain.Unauthorizedf("invalid email or password")
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		if errorsIs(err, domain.ErrNotFound) {
			return "", time.Time{}, invalid
		}
		return "", time.Time{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", time.Time{}, invalid
	}
	exp := time.Now().Add(s.jwtTTL).UTC()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(u.ID, 10),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(exp),
	})
	signed, err := tok.SignedString(s.jwtSecret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, exp, nil
}

// ParseToken валидирует JWT и возвращает id пользователя.
func (s *Service) ParseToken(token string) (int64, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(token, claims,
		func(*jwt.Token) (any, error) { return s.jwtSecret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil {
		return 0, domain.Unauthorizedf("invalid or expired token")
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || id <= 0 {
		return 0, domain.Unauthorizedf("invalid token subject")
	}
	return id, nil
}
