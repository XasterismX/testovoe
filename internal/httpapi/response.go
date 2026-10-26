package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"taskmanager/internal/domain"
)

type errorBody struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *API) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, msg := http.StatusInternalServerError, "internal_error", "internal server error"
	switch {
	case errors.Is(err, domain.ErrValidation):
		status, code, msg = http.StatusBadRequest, "validation_error", err.Error()
	case errors.Is(err, domain.ErrUnauthorized):
		status, code, msg = http.StatusUnauthorized, "unauthorized", err.Error()
	case errors.Is(err, domain.ErrForbidden):
		status, code, msg = http.StatusForbidden, "forbidden", err.Error()
	case errors.Is(err, domain.ErrNotFound):
		status, code, msg = http.StatusNotFound, "not_found", err.Error()
	case errors.Is(err, domain.ErrConflict):
		status, code, msg = http.StatusConflict, "conflict", err.Error()
	default:
		a.log.Error("request failed", slog.String("path", r.URL.Path), slog.Any("error", err))
	}
	writeJSON(w, status, errorBody{Error: msg, Code: code})
}

// decode читает JSON-тело (до 1 МБ), неизвестные поля запрещены.
func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return domain.Validationf("invalid JSON body: %v", err)
	}
	return nil
}

func pathID(r *http.Request, name string) (int64, error) {
	v, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || v <= 0 {
		return 0, domain.Validationf("invalid %s", name)
	}
	return v, nil
}

func queryInt(q url.Values, key string) (int64, bool, error) {
	if !q.Has(key) {
		return 0, false, nil
	}
	v, err := strconv.ParseInt(q.Get(key), 10, 64)
	if err != nil {
		return 0, false, domain.Validationf("invalid %s", key)
	}
	return v, true, nil
}
