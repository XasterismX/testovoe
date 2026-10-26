package service

import (
	"context"

	"taskmanager/internal/domain"
	"taskmanager/internal/repository"
)

// CreateTeam атомарно создаёт команду и добавляет создателя участником с ролью owner.
func (s *Service) CreateTeam(ctx context.Context, userID int64, name string) (*domain.Team, error) {
	name = trim(name)
	if err := validateName("name", name, 255); err != nil {
		return nil, err
	}
	var team *domain.Team
	err := s.repo.InTx(ctx, func(tx *repository.Repo) error {
		t, err := tx.CreateTeam(ctx, name, userID)
		if err != nil {
			return err
		}
		if err := tx.AddMember(ctx, t.ID, userID, domain.RoleOwner); err != nil {
			return err
		}
		team = t
		return nil
	})
	if err != nil {
		return nil, err
	}
	team.Role = domain.RoleOwner
	return team, nil
}

func (s *Service) ListTeams(ctx context.Context, userID int64) ([]domain.Team, error) {
	return s.repo.ListTeams(ctx, userID)
}

// Invite добавляет существующего пользователя в команду. Роль по умолчанию — member.
func (s *Service) Invite(ctx context.Context, actorID, teamID int64, email string, role domain.Role) (*domain.Member, error) {
	if role == "" {
		role = domain.RoleMember
	}
	if !role.Valid() {
		return nil, domain.Validationf("role must be one of: admin, member")
	}
	if role == domain.RoleOwner {
		return nil, domain.Validationf("owner role cannot be granted via invite")
	}
	actorRole, err := s.requireMember(ctx, teamID, actorID)
	if err != nil {
		return nil, err
	}
	if !CanInvite(actorRole, role) {
		return nil, domain.Forbiddenf("your role %q cannot invite users with role %q", actorRole, role)
	}
	email, err = normalizeEmail(email)
	if err != nil {
		return nil, err
	}
	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		if errorsIs(err, domain.ErrNotFound) {
			return nil, domain.NotFoundf("user with this email is not registered")
		}
		return nil, err
	}
	if err := s.repo.AddMember(ctx, teamID, user.ID, role); err != nil {
		if errorsIs(err, domain.ErrConflict) {
			return nil, domain.Conflictf("user is already a member of this team")
		}
		return nil, err
	}
	return &domain.Member{TeamID: teamID, UserID: user.ID, Role: role}, nil
}

// ChangeRole — только owner; роль owner изменить или выдать нельзя.
func (s *Service) ChangeRole(ctx context.Context, actorID, teamID, targetID int64, role domain.Role) (*domain.Member, error) {
	if role != domain.RoleAdmin && role != domain.RoleMember {
		return nil, domain.Validationf("role must be one of: admin, member")
	}
	actorRole, err := s.requireMember(ctx, teamID, actorID)
	if err != nil {
		return nil, err
	}
	if !CanChangeRoles(actorRole) {
		return nil, domain.Forbiddenf("only the team owner can change roles")
	}
	targetRole, err := s.repo.GetMemberRole(ctx, teamID, targetID)
	if err != nil {
		if errorsIs(err, domain.ErrNotFound) {
			return nil, domain.NotFoundf("user is not a member of this team")
		}
		return nil, err
	}
	if targetRole == domain.RoleOwner {
		return nil, domain.Forbiddenf("the owner's role cannot be changed")
	}
	if err := s.repo.SetMemberRole(ctx, teamID, targetID, role); err != nil {
		return nil, err
	}
	return &domain.Member{TeamID: teamID, UserID: targetID, Role: role}, nil
}

// TeamStats — SQL-отчёт по одной команде, доступен owner и admin.
func (s *Service) TeamStats(ctx context.Context, actorID, teamID int64) (*domain.TeamStats, error) {
	role, err := s.requireMember(ctx, teamID, actorID)
	if err != nil {
		return nil, err
	}
	if !CanViewStats(role) {
		return nil, domain.Forbiddenf("only the owner or an admin can view team stats")
	}
	return s.repo.TeamStats(ctx, teamID)
}
