package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

func (s *Server) handleHealthz() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
	}
}

func (s *Server) handleReadyz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		checks := map[string]string{}
		status := "ok"
		code := http.StatusOK

		if err := s.db.Ping(ctx); err != nil {
			checks["postgres"] = err.Error()
			status = "degraded"
			code = http.StatusServiceUnavailable
		} else {
			checks["postgres"] = "ok"
		}

		writeJSON(w, code, healthResponse{Status: status, Checks: checks})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
