// Package repository — доступ к MySQL. Один и тот же Repo работает как поверх *sql.DB,
// так и внутри транзакции (см. InTx), поэтому сервисный слой управляет атомарностью.
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"

	"taskmanager/internal/domain"
)

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type scanner interface{ Scan(dest ...any) error }

type Repo struct {
	db *sql.DB
	q  querier
}

func New(db *sql.DB) *Repo { return &Repo{db: db, q: db} }

// Open подключается к MySQL (с ретраями на время старта контейнера).
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse MYSQL_DSN: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = true // нужно для запуска миграций
	cfg.Collation = "utf8mb4_unicode_ci"
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	for i := 0; i < 30; i++ {
		if err = db.PingContext(ctx); err == nil {
			return db, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("mysql is not reachable: %w", err)
}

// InTx выполняет fn в одной транзакции; ошибка/паника — откат, иначе коммит.
func (r *Repo) InTx(ctx context.Context, fn func(tx *Repo) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // после Commit — no-op
	if err := fn(&Repo{db: r.db, q: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case 1062: // duplicate entry
			return domain.ErrConflict
		case 1452: // FK violation
			return domain.Validationf("referenced entity does not exist")
		}
	}
	return err
}

// ---------- users ----------

func (r *Repo) CreateUser(ctx context.Context, email, passwordHash, name string) (*domain.User, error) {
	now := time.Now().UTC()
	res, err := r.q.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, name, created_at) VALUES (?, ?, ?, ?)`,
		email, passwordHash, name, now)
	if err != nil {
		return nil, mapErr(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &domain.User{ID: id, Email: email, PasswordHash: passwordHash, Name: name, CreatedAt: now}, nil
}

func (r *Repo) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	var u domain.User
	err := r.q.QueryRowContext(ctx,
		`SELECT id, email, password_hash, name, created_at FROM users WHERE email = ?`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &u, nil
}

// ---------- teams ----------

func (r *Repo) CreateTeam(ctx context.Context, name string, createdBy int64) (*domain.Team, error) {
	now := time.Now().UTC()
	res, err := r.q.ExecContext(ctx,
		`INSERT INTO teams (name, created_by, created_at) VALUES (?, ?, ?)`, name, createdBy, now)
	if err != nil {
		return nil, mapErr(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &domain.Team{ID: id, Name: name, CreatedBy: createdBy, CreatedAt: now}, nil
}

func (r *Repo) AddMember(ctx context.Context, teamID, userID int64, role domain.Role) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)`, teamID, userID, role)
	return mapErr(err)
}

func (r *Repo) SetMemberRole(ctx context.Context, teamID, userID int64, role domain.Role) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE team_members SET role = ? WHERE team_id = ? AND user_id = ?`, role, teamID, userID)
	return mapErr(err)
}

// GetMemberRole возвращает domain.ErrNotFound, если пользователь не состоит в команде.
func (r *Repo) GetMemberRole(ctx context.Context, teamID, userID int64) (domain.Role, error) {
	var role domain.Role
	err := r.q.QueryRowContext(ctx,
		`SELECT role FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, userID).Scan(&role)
	if err != nil {
		return "", mapErr(err)
	}
	return role, nil
}

func (r *Repo) ListTeams(ctx context.Context, userID int64) ([]domain.Team, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT t.id, t.name, t.created_by, t.created_at, m.role
		   FROM teams t JOIN team_members m ON m.team_id = t.id
		  WHERE m.user_id = ? ORDER BY t.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	teams := []domain.Team{}
	for rows.Next() {
		var t domain.Team
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedBy, &t.CreatedAt, &t.Role); err != nil {
			return nil, err
		}
		teams = append(teams, t)
	}
	return teams, rows.Err()
}

// ---------- tasks ----------

const taskCols = `id, team_id, title, description, status, created_by, assignee_id,
	created_at, updated_at, closed_at, version`

func scanTask(s scanner) (*domain.Task, error) {
	var t domain.Task
	var assignee sql.NullInt64
	var closed sql.NullTime
	if err := s.Scan(&t.ID, &t.TeamID, &t.Title, &t.Description, &t.Status, &t.CreatedBy,
		&assignee, &t.CreatedAt, &t.UpdatedAt, &closed, &t.Version); err != nil {
		return nil, err
	}
	if assignee.Valid {
		v := assignee.Int64
		t.AssigneeID = &v
	}
	if closed.Valid {
		v := closed.Time
		t.ClosedAt = &v
	}
	return &t, nil
}

func (r *Repo) InsertTask(ctx context.Context, t *domain.Task) error {
	res, err := r.q.ExecContext(ctx,
		`INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id,
		                    created_at, updated_at, closed_at, version)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.TeamID, t.Title, t.Description, t.Status, t.CreatedBy, t.AssigneeID,
		t.CreatedAt, t.UpdatedAt, t.ClosedAt, t.Version)
	if err != nil {
		return mapErr(err)
	}
	t.ID, err = res.LastInsertId()
	return err
}

func (r *Repo) GetTask(ctx context.Context, id int64) (*domain.Task, error) {
	t, err := scanTask(r.q.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ?`, id))
	return t, mapErr(err)
}

// LockTask читает задачу с блокировкой строки (SELECT ... FOR UPDATE) — только внутри InTx.
func (r *Repo) LockTask(ctx context.Context, id int64) (*domain.Task, error) {
	t, err := scanTask(r.q.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ? FOR UPDATE`, id))
	return t, mapErr(err)
}

// SaveTask записывает изменения с проверкой версии (оптимистичная блокировка)
// и увеличивает t.Version. Несовпадение версии — domain.ErrConflict.
func (r *Repo) SaveTask(ctx context.Context, t *domain.Task) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE tasks SET title = ?, description = ?, status = ?, assignee_id = ?,
		                  updated_at = ?, closed_at = ?, version = version + 1
		  WHERE id = ? AND version = ?`,
		t.Title, t.Description, t.Status, t.AssigneeID, t.UpdatedAt, t.ClosedAt, t.ID, t.Version)
	if err != nil {
		return mapErr(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.Conflictf("task was modified concurrently")
	}
	t.Version++
	return nil
}

func (r *Repo) ListTasks(ctx context.Context, f domain.TaskFilter) (*domain.TaskList, error) {
	where := "team_id = ?"
	args := []any{f.TeamID}
	if f.Status != "" {
		where += " AND status = ?"
		args = append(args, f.Status)
	}
	if f.AssigneeID != nil {
		where += " AND assignee_id = ?"
		args = append(args, *f.AssigneeID)
	}
	var total int
	if err := r.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE `+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+taskCols+` FROM tasks WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := &domain.TaskList{Items: []domain.Task{}, Total: total, Limit: f.Limit, Offset: f.Offset}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		list.Items = append(list.Items, *t)
	}
	return list, rows.Err()
}

// ---------- history & comments ----------

func (r *Repo) InsertHistory(ctx context.Context, taskID, changedBy int64, changes map[string]domain.Change) error {
	b, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx,
		`INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES (?, ?, ?, ?)`,
		taskID, changedBy, string(b), time.Now().UTC())
	return mapErr(err)
}

func (r *Repo) ListHistory(ctx context.Context, taskID int64) ([]domain.HistoryEntry, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, task_id, changed_by, changes, created_at FROM task_history WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.HistoryEntry{}
	for rows.Next() {
		var h domain.HistoryEntry
		var raw []byte
		if err := rows.Scan(&h.ID, &h.TaskID, &h.ChangedBy, &raw, &h.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &h.Changes); err != nil {
			return nil, fmt.Errorf("history %d: %w", h.ID, err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *Repo) InsertComment(ctx context.Context, taskID, userID int64, content string) (*domain.Comment, error) {
	now := time.Now().UTC()
	res, err := r.q.ExecContext(ctx,
		`INSERT INTO task_comments (task_id, user_id, content, created_at) VALUES (?, ?, ?, ?)`,
		taskID, userID, content, now)
	if err != nil {
		return nil, mapErr(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &domain.Comment{ID: id, TaskID: taskID, UserID: userID, Content: content, CreatedAt: now}, nil
}

func (r *Repo) ListComments(ctx context.Context, taskID int64) ([]domain.Comment, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, task_id, user_id, content, created_at FROM task_comments WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Comment{}
	for rows.Next() {
		var c domain.Comment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.UserID, &c.Content, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
