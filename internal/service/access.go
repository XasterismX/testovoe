package service

import (
	"strings"

	"taskmanager/internal/domain"
)

// Чистые функции проверки прав — без БД, покрыты unit-тестами.

// CanInvite: owner приглашает как admin/member, admin — только как member.
// Роль owner через приглашение не выдаётся никому.
func CanInvite(actor, target domain.Role) bool {
	switch actor {
	case domain.RoleOwner:
		return target == domain.RoleAdmin || target == domain.RoleMember
	case domain.RoleAdmin:
		return target == domain.RoleMember
	}
	return false
}

// CanChangeRoles: менять роли участников может только создатель команды.
func CanChangeRoles(actor domain.Role) bool { return actor == domain.RoleOwner }

// CanViewStats: аналитика доступна owner и admin.
func CanViewStats(role domain.Role) bool {
	return role == domain.RoleOwner || role == domain.RoleAdmin
}

// CanEditTask проверяет право применить patch к задаче t.
//   - owner/admin: любые задачи команды;
//   - создатель задачи: название, описание, статус, исполнитель;
//   - исполнитель (не создатель): только статус;
//   - остальные участники: нельзя.
func CanEditTask(role domain.Role, actorID int64, t *domain.Task, p domain.TaskPatch) error {
	if !role.Valid() {
		return domain.Forbiddenf("you are not a member of this team")
	}
	if role == domain.RoleOwner || role == domain.RoleAdmin {
		return nil
	}
	if t.CreatedBy == actorID {
		return nil
	}
	if t.AssigneeID != nil && *t.AssigneeID == actorID {
		titleChanged := p.Title != nil && strings.TrimSpace(*p.Title) != t.Title
		descChanged := p.Description != nil && *p.Description != t.Description
		assigneeChanged := p.AssigneeID.Set && !sameID(p.AssigneeID.Value, t.AssigneeID)
		if titleChanged || descChanged || assigneeChanged {
			return domain.Forbiddenf("assignee may only change the task status")
		}
		return nil
	}
	return domain.Forbiddenf("you can edit only tasks you created or are assigned to")
}

func sameID(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
