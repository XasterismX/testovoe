//go:build integration

package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"testing"
	"time"

	"taskmanager/internal/cache"
	"taskmanager/internal/domain"
	"taskmanager/internal/repository"
	"taskmanager/internal/service"
	"taskmanager/migrations"
)

// Запуск: TEST_MYSQL_DSN="app:app@tcp(localhost:3306)/taskmanager" go test -tags=integration ./internal/service
func TestTeamStatsIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not set")
	}
	ctx := context.Background()
	db, err := repository.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := repository.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	svc := service.New(repo, cache.Nop{}, []byte("test-secret"), time.Hour)
	_ = slog.Default()

	suffix := time.Now().UnixNano()
	mkUser := func(name string) *domain.User {
		u, err := repo.CreateUser(ctx, fmt.Sprintf("%s-%d@example.com", name, suffix), "hash", name)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	owner, admin, member, outsider := mkUser("owner"), mkUser("admin"), mkUser("member"), mkUser("outsider")

	teamA, err := svc.CreateTeam(ctx, owner.ID, "A")
	if err != nil {
		t.Fatal(err)
	}
	teamB, err := svc.CreateTeam(ctx, outsider.ID, "B")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, id := range []int64{teamA.ID, teamB.ID} {
			_, _ = db.Exec(`DELETE FROM teams WHERE id = ?`, id) // каскадом удалит задачи/комментарии/историю
		}
		for _, u := range []*domain.User{owner, admin, member, outsider} {
			_, _ = db.Exec(`DELETE FROM users WHERE id = ?`, u.ID)
		}
	})
	if _, err := svc.Invite(ctx, owner.ID, teamA.ID, admin.Email, domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invite(ctx, owner.ID, teamA.ID, member.Email, domain.RoleMember); err != nil {
		t.Fatal(err)
	}

	// Время считается внутри одного SQL-выражения (UTC_TIMESTAMP постоянен в рамках запроса),
	// поэтому длительности точные. Параметры: team, status, assignee, created offset, closed offset (секунды назад).
	const day = 86400
	insertTask := func(team int64, title, status string, assignee *int64, createdAgo int, closedAgo *int) int64 {
		var closed any
		if closedAgo != nil {
			closed = *closedAgo
		}
		res, err := db.Exec(`INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version)
			VALUES (?, ?, '', ?, ?, ?, UTC_TIMESTAMP() - INTERVAL ? SECOND, UTC_TIMESTAMP(),
			        IF(? IS NULL, NULL, UTC_TIMESTAMP() - INTERVAL ? SECOND), 1)`,
			team, title, status, owner.ID, assignee, createdAgo, closed, closed)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	ago := func(s int) *int { return &s }

	t1 := insertTask(teamA.ID, "t1", "done", &admin.ID, 3*day, ago(1*day)) // 48h, в окне 30д
	insertTask(teamA.ID, "t2", "done", &admin.ID, 10*day, ago(5*day))      // 120h, в окне
	insertTask(teamA.ID, "t3", "done", &member.ID, 41*day, ago(40*day))    // 24h, ВНЕ окна 30д
	t4 := insertTask(teamA.ID, "t4", "todo", &member.ID, 1*day, nil)
	insertTask(teamA.ID, "t5", "in_progress", nil, 1*day, nil)
	insertTask(teamA.ID, "t6", "done", &member.ID, 1*day+6*3600, ago(1*day)) // 6h, в окне
	tB := insertTask(teamB.ID, "other-team", "done", &outsider.ID, 2*day, ago(1*day))

	addComments := func(task int64, n int) {
		for i := 0; i < n; i++ {
			if _, err := db.Exec(`INSERT INTO task_comments (task_id, user_id, content, created_at) VALUES (?, ?, 'c', UTC_TIMESTAMP())`, task, owner.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	addComments(t1, 3)
	addComments(t4, 1)
	addComments(tB, 5) // комментарии чужой команды не должны попасть в отчёт

	st, err := svc.TeamStats(ctx, owner.ID, teamA.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got := st.TasksByStatus; got[domain.StatusTodo] != 1 || got[domain.StatusInProgress] != 1 || got[domain.StatusDone] != 4 {
		t.Errorf("tasks_by_status = %v", got)
	}
	if len(st.TopAssignees) != 2 ||
		st.TopAssignees[0].UserID != admin.ID || st.TopAssignees[0].ClosedTasks != 2 ||
		st.TopAssignees[1].UserID != member.ID || st.TopAssignees[1].ClosedTasks != 1 {
		t.Errorf("top_assignees_30d = %+v (задача t3 старше 30 дней учитываться не должна)", st.TopAssignees)
	}
	// (48h + 120h + 24h + 6h) / 4 = 49.5h = 178200 с
	if st.ClosedTasks != 4 || st.AvgCloseSeconds == nil || math.Abs(*st.AvgCloseSeconds-178200) > 1 {
		t.Errorf("avg close = %v closed=%d, want 178200 / 4", st.AvgCloseSeconds, st.ClosedTasks)
	}
	if st.CommentsTotal != 4 {
		t.Errorf("comments_total = %d, want 4", st.CommentsTotal)
	}
	if len(st.CommentsByTask) == 0 || st.CommentsByTask[0].TaskID != t1 || st.CommentsByTask[0].Comments != 3 {
		t.Errorf("comments_by_task = %+v", st.CommentsByTask)
	}

	// Права: admin видит отчёт, member и посторонние — нет; чужая команда не утекает.
	if _, err := svc.TeamStats(ctx, admin.ID, teamA.ID); err != nil {
		t.Errorf("admin must see stats: %v", err)
	}
	for name, uid := range map[string]int64{"member": member.ID, "outsider": outsider.ID} {
		if _, err := svc.TeamStats(ctx, uid, teamA.ID); !isForbidden(err) {
			t.Errorf("%s must get ErrForbidden, got %v", name, err)
		}
	}
	if _, err := svc.TeamStats(ctx, owner.ID, teamB.ID); !isForbidden(err) {
		t.Errorf("owner of A must not read stats of B, got %v", err)
	}
}

func isForbidden(err error) bool { return err != nil && errorsIsForbidden(err) }

var _ = sql.ErrNoRows

func errorsIsForbidden(err error) bool { return errors.Is(err, domain.ErrForbidden) }
