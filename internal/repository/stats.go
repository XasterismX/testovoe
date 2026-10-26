package repository

import (
	"context"
	"database/sql"
	"fmt"

	"taskmanager/internal/domain"
)

// statsQuery — один запрос (CTE + UNION ALL), без N+1. Все CTE фильтруются по team_id.
// Используются JOIN, GROUP BY, агрегаты, оконная функция и работа с датами (30 дней, TIMESTAMPDIFF).
// Формат строк: (metric, label, id, val, ord); сортировка по (metric, ord) делается в SQL.
const statsQuery = `
WITH
status_cnt AS (
    SELECT status, COUNT(*) AS cnt
      FROM tasks
     WHERE team_id = ?
     GROUP BY status
),
top_assignees AS (
    SELECT u.id, u.name, COUNT(*) AS closed_cnt,
           ROW_NUMBER() OVER (ORDER BY COUNT(*) DESC, u.id) AS rn
      FROM tasks t
      JOIN users u ON u.id = t.assignee_id
     WHERE t.team_id = ?
       AND t.status = 'done'
       AND t.closed_at >= UTC_TIMESTAMP() - INTERVAL 30 DAY
     GROUP BY u.id, u.name
),
close_time AS (
    SELECT AVG(TIMESTAMPDIFF(SECOND, created_at, closed_at)) AS avg_sec, COUNT(*) AS cnt
      FROM tasks
     WHERE team_id = ? AND closed_at IS NOT NULL
),
task_comment_cnt AS (
    SELECT t.id, t.title, COUNT(c.id) AS cnt,
           ROW_NUMBER() OVER (ORDER BY COUNT(c.id) DESC, t.id) AS rn
      FROM tasks t
      LEFT JOIN task_comments c ON c.task_id = t.id
     WHERE t.team_id = ?
     GROUP BY t.id, t.title
)
SELECT 'status' AS metric, CAST(status AS CHAR) AS label, CAST(NULL AS SIGNED) AS id,
       CAST(cnt AS DOUBLE) AS val, 0 AS ord
  FROM status_cnt
UNION ALL
SELECT 'top_assignee', name, id, closed_cnt, rn FROM top_assignees WHERE rn <= 3
UNION ALL
SELECT 'avg_close', NULL, cnt, avg_sec, 0 FROM close_time
UNION ALL
SELECT 'comments_total', NULL, NULL, COALESCE(SUM(cnt), 0), 0 FROM task_comment_cnt
UNION ALL
SELECT 'comments_task', title, id, cnt, rn FROM task_comment_cnt WHERE rn <= 10
ORDER BY metric, ord`

// TeamStats возвращает статистику строго по одной команде.
func (r *Repo) TeamStats(ctx context.Context, teamID int64) (*domain.TeamStats, error) {
	rows, err := r.q.QueryContext(ctx, statsQuery, teamID, teamID, teamID, teamID)
	if err != nil {
		return nil, fmt.Errorf("team stats: %w", err)
	}
	defer rows.Close()

	st := &domain.TeamStats{
		TeamID: teamID,
		TasksByStatus: map[domain.Status]int{
			domain.StatusTodo: 0, domain.StatusInProgress: 0, domain.StatusDone: 0,
		},
		TopAssignees:   []domain.AssigneeStat{},
		CommentsByTask: []domain.TaskCommentStat{},
	}
	for rows.Next() {
		var (
			metric string
			label  sql.NullString
			id     sql.NullInt64
			val    sql.NullFloat64
			ord    int
		)
		if err := rows.Scan(&metric, &label, &id, &val, &ord); err != nil {
			return nil, err
		}
		switch metric {
		case "status":
			st.TasksByStatus[domain.Status(label.String)] = int(val.Float64)
		case "top_assignee":
			st.TopAssignees = append(st.TopAssignees, domain.AssigneeStat{
				UserID: id.Int64, Name: label.String, ClosedTasks: int(val.Float64),
			})
		case "avg_close":
			st.ClosedTasks = int(id.Int64)
			if val.Valid {
				v := val.Float64
				st.AvgCloseSeconds = &v
			}
		case "comments_total":
			st.CommentsTotal = int(val.Float64)
		case "comments_task":
			st.CommentsByTask = append(st.CommentsByTask, domain.TaskCommentStat{
				TaskID: id.Int64, Title: label.String, Comments: int(val.Float64),
			})
		}
	}
	return st, rows.Err()
}
