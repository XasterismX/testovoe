package httpapi

import (
	"net/http"

	"taskmanager/internal/domain"
)

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := decode(w, r, &req); err != nil {
		a.writeError(w, r, err)
		return
	}
	u, err := a.svc.Register(r.Context(), req.Email, req.Name, req.Password)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(w, r, &req); err != nil {
		a.writeError(w, r, err)
		return
	}
	token, exp, err := a.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_at": exp})
}

func (a *API) createTeam(w http.ResponseWriter, r *http.Request, uid int64) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(w, r, &req); err != nil {
		a.writeError(w, r, err)
		return
	}
	t, err := a.svc.CreateTeam(r.Context(), uid, req.Name)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (a *API) listTeams(w http.ResponseWriter, r *http.Request, uid int64) {
	teams, err := a.svc.ListTeams(r.Context(), uid)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, teams)
}

func (a *API) invite(w http.ResponseWriter, r *http.Request, uid int64) {
	teamID, err := pathID(r, "id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	var req struct {
		Email string      `json:"email"`
		Role  domain.Role `json:"role"`
	}
	if err := decode(w, r, &req); err != nil {
		a.writeError(w, r, err)
		return
	}
	m, err := a.svc.Invite(r.Context(), uid, teamID, req.Email, req.Role)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (a *API) changeRole(w http.ResponseWriter, r *http.Request, uid int64) {
	teamID, err := pathID(r, "id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	targetID, err := pathID(r, "user_id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	var req struct {
		Role domain.Role `json:"role"`
	}
	if err := decode(w, r, &req); err != nil {
		a.writeError(w, r, err)
		return
	}
	m, err := a.svc.ChangeRole(r.Context(), uid, teamID, targetID, req.Role)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (a *API) teamStats(w http.ResponseWriter, r *http.Request, uid int64) {
	teamID, err := pathID(r, "team_id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	st, err := a.svc.TeamStats(r.Context(), uid, teamID)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *API) createTask(w http.ResponseWriter, r *http.Request, uid int64) {
	var in domain.CreateTaskInput
	if err := decode(w, r, &in); err != nil {
		a.writeError(w, r, err)
		return
	}
	t, err := a.svc.CreateTask(r.Context(), uid, in)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (a *API) listTasks(w http.ResponseWriter, r *http.Request, uid int64) {
	q := r.URL.Query()
	f := domain.TaskFilter{Status: domain.Status(q.Get("status"))}
	var err error
	if f.TeamID, _, err = queryInt(q, "team_id"); err != nil {
		a.writeError(w, r, err)
		return
	}
	if v, ok, err := queryInt(q, "assignee_id"); err != nil {
		a.writeError(w, r, err)
		return
	} else if ok {
		f.AssigneeID = &v
	}
	if v, _, err := queryInt(q, "limit"); err != nil {
		a.writeError(w, r, err)
		return
	} else {
		f.Limit = int(v)
	}
	if v, _, err := queryInt(q, "offset"); err != nil {
		a.writeError(w, r, err)
		return
	} else {
		f.Offset = int(v)
	}
	list, err := a.svc.ListTasks(r.Context(), uid, f)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) updateTask(w http.ResponseWriter, r *http.Request, uid int64) {
	id, err := pathID(r, "id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	var p domain.TaskPatch
	if err := decode(w, r, &p); err != nil {
		a.writeError(w, r, err)
		return
	}
	t, err := a.svc.UpdateTask(r.Context(), uid, id, p)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (a *API) taskHistory(w http.ResponseWriter, r *http.Request, uid int64) {
	id, err := pathID(r, "id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	h, err := a.svc.TaskHistory(r.Context(), uid, id)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (a *API) addComment(w http.ResponseWriter, r *http.Request, uid int64) {
	id, err := pathID(r, "id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := decode(w, r, &req); err != nil {
		a.writeError(w, r, err)
		return
	}
	c, err := a.svc.AddComment(r.Context(), uid, id, req.Content)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) listComments(w http.ResponseWriter, r *http.Request, uid int64) {
	id, err := pathID(r, "id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	cs, err := a.svc.ListComments(r.Context(), uid, id)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cs)
}
