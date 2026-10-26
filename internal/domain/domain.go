// Package domain содержит сущности, ошибки и DTO, общие для всех слоёв.
package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Базовые категории ошибок; транспортный слой маппит их на HTTP-статусы.
var (
	ErrNotFound     = errors.New("not found")
	ErrForbidden    = errors.New("forbidden")
	ErrConflict     = errors.New("conflict")
	ErrValidation   = errors.New("validation error")
	ErrUnauthorized = errors.New("unauthorized")
)

// Error — ошибка с категорией (для errors.Is) и человекочитаемым сообщением.
type Error struct {
	kind error
	msg  string
}

func (e *Error) Error() string { return e.msg }
func (e *Error) Unwrap() error { return e.kind }

func newErr(kind error, f string, a ...any) error {
	return &Error{kind: kind, msg: fmt.Sprintf(f, a...)}
}

func Validationf(f string, a ...any) error   { return newErr(ErrValidation, f, a...) }
func Forbiddenf(f string, a ...any) error    { return newErr(ErrForbidden, f, a...) }
func Conflictf(f string, a ...any) error     { return newErr(ErrConflict, f, a...) }
func NotFoundf(f string, a ...any) error     { return newErr(ErrNotFound, f, a...) }
func Unauthorizedf(f string, a ...any) error { return newErr(ErrUnauthorized, f, a...) }

// Role — роль пользователя в команде.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

func (r Role) Valid() bool { return r == RoleOwner || r == RoleAdmin || r == RoleMember }

// Status — статус задачи.
type Status string

const (
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

func (s Status) Valid() bool {
	return s == StatusTodo || s == StatusInProgress || s == StatusDone
}

type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"created_at"`
}

type Team struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedBy int64     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	Role      Role      `json:"role,omitempty"` // роль текущего пользователя
}

type Member struct {
	TeamID int64 `json:"team_id"`
	UserID int64 `json:"user_id"`
	Role   Role  `json:"role"`
}

type Task struct {
	ID          int64      `json:"id"`
	TeamID      int64      `json:"team_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      Status     `json:"status"`
	CreatedBy   int64      `json:"created_by"`
	AssigneeID  *int64     `json:"assignee_id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	Version     int        `json:"version"`
}

// Change — старое и новое значение поля задачи.
type Change struct {
	Old any `json:"old"`
	New any `json:"new"`
}

type HistoryEntry struct {
	ID        int64             `json:"id"`
	TaskID    int64             `json:"task_id"`
	ChangedBy int64             `json:"changed_by"`
	Changes   map[string]Change `json:"changes"`
	CreatedAt time.Time         `json:"created_at"`
}

type Comment struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	UserID    int64     `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateTaskInput — тело POST /tasks.
type CreateTaskInput struct {
	TeamID      int64  `json:"team_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      Status `json:"status"`
	AssigneeID  *int64 `json:"assignee_id"`
}

// OptInt64 различает «поле не передано» и «передан null» (снять исполнителя).
type OptInt64 struct {
	Set   bool
	Value *int64
}

func (o *OptInt64) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var v int64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

// TaskPatch — тело PUT /tasks/{id}; nil-поля не меняются.
type TaskPatch struct {
	Title       *string  `json:"title"`
	Description *string  `json:"description"`
	Status      *Status  `json:"status"`
	AssigneeID  OptInt64 `json:"assignee_id"`
	// Version — необязательная оптимистичная блокировка: если указана и не совпадает
	// с текущей версией задачи, возвращается 409.
	Version *int `json:"version"`
}

type TaskFilter struct {
	TeamID     int64
	Status     Status
	AssigneeID *int64
	Limit      int
	Offset     int
}

type TaskList struct {
	Items  []Task `json:"items"`
	Total  int    `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

type AssigneeStat struct {
	UserID      int64  `json:"user_id"`
	Name        string `json:"name"`
	ClosedTasks int    `json:"closed_tasks"`
}

type TaskCommentStat struct {
	TaskID   int64  `json:"task_id"`
	Title    string `json:"title"`
	Comments int    `json:"comments"`
}

type TeamStats struct {
	TeamID          int64             `json:"team_id"`
	TasksByStatus   map[Status]int    `json:"tasks_by_status"`
	TopAssignees    []AssigneeStat    `json:"top_assignees_30d"`
	ClosedTasks     int               `json:"closed_tasks"`
	AvgCloseSeconds *float64          `json:"avg_close_time_seconds"`
	CommentsTotal   int               `json:"comments_total"`
	CommentsByTask  []TaskCommentStat `json:"comments_by_task"`
}
