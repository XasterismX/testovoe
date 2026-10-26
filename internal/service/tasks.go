package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"taskmanager/internal/domain"
	"taskmanager/internal/repository"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

func errorsIs(err, target error) bool { return errors.Is(err, target) }

func validateTitle(t string) error { return validateName("title", t, 255) }

func (s *Service) invalidate(ctx context.Context, teamID int64) { s.cache.Invalidate(ctx, teamID) }

// checkAssignee: исполнитель обязан быть участником той же команды.
func checkAssignee(ctx context.Context, r *repository.Repo, teamID, userID int64) error {
	if _, err := r.GetMemberRole(ctx, teamID, userID); err != nil {
		if errorsIs(err, domain.ErrNotFound) {
			return domain.Validationf("assignee must be a member of the same team")
		}
		return err
	}
	return nil
}

func idVal(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func timeVal(p *time.Time) any {
	if p == nil {
		return nil
	}
	return *p
}

// CreateTask: задача и запись истории создаются в одной транзакции.
func (s *Service) CreateTask(ctx context.Context, actorID int64, in domain.CreateTaskInput) (*domain.Task, error) {
	in.Title = trim(in.Title)
	if err := validateTitle(in.Title); err != nil {
		return nil, err
	}
	if in.TeamID <= 0 {
		return nil, domain.Validationf("team_id is required")
	}
	if in.Status == "" {
		in.Status = domain.StatusTodo
	}
	if !in.Status.Valid() {
		return nil, domain.Validationf("status must be one of: todo, in_progress, done")
	}
	if _, err := s.requireMember(ctx, in.TeamID, actorID); err != nil {
		return nil, err
	}

	var task *domain.Task
	err := s.repo.InTx(ctx, func(tx *repository.Repo) error {
		if in.AssigneeID != nil {
			if err := checkAssignee(ctx, tx, in.TeamID, *in.AssigneeID); err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		t := &domain.Task{
			TeamID: in.TeamID, Title: in.Title, Description: in.Description, Status: in.Status,
			CreatedBy: actorID, AssigneeID: in.AssigneeID, CreatedAt: now, UpdatedAt: now, Version: 1,
		}
		if t.Status == domain.StatusDone {
			t.ClosedAt = &now
		}
		if err := tx.InsertTask(ctx, t); err != nil {
			return err
		}
		changes := map[string]domain.Change{
			"title":       {New: t.Title},
			"description": {New: t.Description},
			"status":      {New: t.Status},
			"assignee_id": {New: idVal(t.AssigneeID)},
		}
		if err := tx.InsertHistory(ctx, t.ID, actorID, changes); err != nil {
			return err
		}
		task = t
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidate(ctx, task.TeamID)
	return task, nil
}

// applyPatch строит новое состояние задачи и diff «старое → новое» от АКТУАЛЬНОГО состояния cur.
func applyPatch(cur *domain.Task, p domain.TaskPatch, now time.Time) (*domain.Task, map[string]domain.Change) {
	next := *cur
	changes := map[string]domain.Change{}
	if p.Title != nil && *p.Title != cur.Title {
		changes["title"] = domain.Change{Old: cur.Title, New: *p.Title}
		next.Title = *p.Title
	}
	if p.Description != nil && *p.Description != cur.Description {
		changes["description"] = domain.Change{Old: cur.Description, New: *p.Description}
		next.Description = *p.Description
	}
	if p.Status != nil && *p.Status != cur.Status {
		changes["status"] = domain.Change{Old: cur.Status, New: *p.Status}
		next.Status = *p.Status
		if next.Status == domain.StatusDone {
			next.ClosedAt = &now
		} else {
			next.ClosedAt = nil
		}
		changes["closed_at"] = domain.Change{Old: timeVal(cur.ClosedAt), New: timeVal(next.ClosedAt)}
	}
	if p.AssigneeID.Set && !sameID(p.AssigneeID.Value, cur.AssigneeID) {
		changes["assignee_id"] = domain.Change{Old: idVal(cur.AssigneeID), New: idVal(p.AssigneeID.Value)}
		next.AssigneeID = p.AssigneeID.Value
	}
	if len(changes) > 0 {
		next.UpdatedAt = now
	}
	return &next, changes
}

// UpdateTask: блокирует строку (SELECT ... FOR UPDATE), проверяет права по актуальному
// состоянию, пишет задачу и историю в одной транзакции. Параллельные обновления
// сериализуются на уровне БД, поэтому изменения не теряются; опциональное поле version
// даёт клиенту оптимистичную блокировку (409 при расхождении).
func (s *Service) UpdateTask(ctx context.Context, actorID, taskID int64, p domain.TaskPatch) (*domain.Task, error) {
	if p.Title != nil {
		t := trim(*p.Title)
		if err := validateTitle(t); err != nil {
			return nil, err
		}
		p.Title = &t
	}
	if p.Status != nil && !p.Status.Valid() {
		return nil, domain.Validationf("status must be one of: todo, in_progress, done")
	}

	var result *domain.Task
	changed := false
	err := s.repo.InTx(ctx, func(tx *repository.Repo) error {
		cur, err := tx.LockTask(ctx, taskID)
		if err != nil {
			return err
		}
		role, err := tx.GetMemberRole(ctx, cur.TeamID, actorID)
		if err != nil {
			return err // не участник команды: для него задачи «не существует» (404)
		}
		if p.Version != nil && *p.Version != cur.Version {
			return domain.Conflictf("task was modified concurrently: current version is %d", cur.Version)
		}
		if err := CanEditTask(role, actorID, cur, p); err != nil {
			return err
		}
		if p.AssigneeID.Set && p.AssigneeID.Value != nil && !sameID(p.AssigneeID.Value, cur.AssigneeID) {
			if err := checkAssignee(ctx, tx, cur.TeamID, *p.AssigneeID.Value); err != nil {
				return err
			}
		}
		next, changes := applyPatch(cur, p, time.Now().UTC())
		if len(changes) == 0 {
			result = cur
			return nil
		}
		if err := tx.SaveTask(ctx, next); err != nil {
			return err
		}
		if err := tx.InsertHistory(ctx, next.ID, actorID, changes); err != nil {
			return err
		}
		result, changed = next, true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.invalidate(ctx, result.TeamID)
	}
	return result, nil
}

func filterKey(f domain.TaskFilter) string {
	var a int64
	if f.AssigneeID != nil {
		a = *f.AssigneeID
	}
	return fmt.Sprintf("s=%s:a=%d:l=%d:o=%d", f.Status, a, f.Limit, f.Offset)
}

// ListTasks: проверка участия → Redis (TTL 5 мин) → MySQL. Ключ кеша включает команду и фильтры.
func (s *Service) ListTasks(ctx context.Context, actorID int64, f domain.TaskFilter) (*domain.TaskList, error) {
	if f.TeamID <= 0 {
		return nil, domain.Validationf("team_id is required")
	}
	if f.Status != "" && !f.Status.Valid() {
		return nil, domain.Validationf("status must be one of: todo, in_progress, done")
	}
	if f.Offset < 0 {
		return nil, domain.Validationf("offset must be >= 0")
	}
	if f.Limit <= 0 {
		f.Limit = defaultLimit
	}
	if f.Limit > maxLimit {
		f.Limit = maxLimit
	}
	if _, err := s.requireMember(ctx, f.TeamID, actorID); err != nil {
		return nil, err
	}

	slot, data := s.cache.Load(ctx, f.TeamID, filterKey(f))
	if data != nil {
		var cached domain.TaskList
		if json.Unmarshal(data, &cached) == nil {
			return &cached, nil
		}
	}
	list, err := s.repo.ListTasks(ctx, f)
	if err != nil {
		return nil, err
	}
	if slot != "" {
		if b, err := json.Marshal(list); err == nil {
			s.cache.Store(ctx, slot, b)
		}
	}
	return list, nil
}

// authorizeTask загружает задачу, если пользователь состоит в её команде.
// Для чужих команд возвращается 404, чтобы не раскрывать существование задачи.
func (s *Service) authorizeTask(ctx context.Context, actorID, taskID int64) (*domain.Task, error) {
	t, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.GetMemberRole(ctx, t.TeamID, actorID); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) TaskHistory(ctx context.Context, actorID, taskID int64) ([]domain.HistoryEntry, error) {
	if _, err := s.authorizeTask(ctx, actorID, taskID); err != nil {
		return nil, err
	}
	return s.repo.ListHistory(ctx, taskID)
}

func (s *Service) AddComment(ctx context.Context, actorID, taskID int64, content string) (*domain.Comment, error) {
	content = trim(content)
	if err := validateName("content", content, 5000); err != nil {
		return nil, err
	}
	if _, err := s.authorizeTask(ctx, actorID, taskID); err != nil {
		return nil, err
	}
	return s.repo.InsertComment(ctx, taskID, actorID, content)
}

func (s *Service) ListComments(ctx context.Context, actorID, taskID int64) ([]domain.Comment, error) {
	if _, err := s.authorizeTask(ctx, actorID, taskID); err != nil {
		return nil, err
	}
	return s.repo.ListComments(ctx, taskID)
}
