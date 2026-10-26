// Package httpapi — HTTP-слой (stdlib net/http, Go 1.22 routing): хендлеры и middleware.
package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"taskmanager/api"
	"taskmanager/internal/service"
)

type API struct {
	svc     *service.Service
	log     *slog.Logger
	limiter *RateLimiter
}

func New(svc *service.Service, log *slog.Logger, limiter *RateLimiter) *API {
	return &API{svc: svc, log: log, limiter: limiter}
}

type authedHandler func(w http.ResponseWriter, r *http.Request, userID int64)

// auth проверяет Bearer JWT и передаёт id пользователя в хендлер.
func (a *API) auth(h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			writeJSON(w, http.StatusUnauthorized, errorBody{Error: "missing bearer token", Code: "unauthorized"})
			return
		}
		uid, err := a.svc.ParseToken(token)
		if err != nil {
			a.writeError(w, r, err)
			return
		}
		h(w, r, uid)
	}
}

// Handler собирает маршруты и middleware (recover → log → rate limit).
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(api.Spec)
	})
	mux.HandleFunc("GET /swagger/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(swaggerHTML))
	})

	mux.HandleFunc("POST /api/v1/register", a.register)
	mux.HandleFunc("POST /api/v1/login", a.login)

	mux.HandleFunc("POST /api/v1/teams", a.auth(a.createTeam))
	mux.HandleFunc("GET /api/v1/teams", a.auth(a.listTeams))
	mux.HandleFunc("POST /api/v1/teams/{id}/invite", a.auth(a.invite))
	mux.HandleFunc("PUT /api/v1/teams/{id}/members/{user_id}/role", a.auth(a.changeRole))
	mux.HandleFunc("GET /api/v1/teams/{team_id}/stats", a.auth(a.teamStats))

	mux.HandleFunc("POST /api/v1/tasks", a.auth(a.createTask))
	mux.HandleFunc("GET /api/v1/tasks", a.auth(a.listTasks))
	mux.HandleFunc("PUT /api/v1/tasks/{id}", a.auth(a.updateTask))
	mux.HandleFunc("GET /api/v1/tasks/{id}/history", a.auth(a.taskHistory))
	mux.HandleFunc("POST /api/v1/tasks/{id}/comments", a.auth(a.addComment))
	mux.HandleFunc("GET /api/v1/tasks/{id}/comments", a.auth(a.listComments))

	return a.recoverer(a.logger(a.limiter.middleware(mux)))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (a *API) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		a.log.Info("http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", sw.status),
			slog.Duration("duration", time.Since(start)),
			slog.String("remote", r.RemoteAddr),
		)
	})
}

func (a *API) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				a.log.Error("panic recovered", slog.Any("panic", rec), slog.String("path", r.URL.Path))
				writeJSON(w, http.StatusInternalServerError, errorBody{Error: "internal server error", Code: "internal_error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

const swaggerHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>Task Manager API</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"></head>
<body><div id="ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>SwaggerUIBundle({url: '/openapi.yaml', dom_id: '#ui'});</script>
</body></html>`
