package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/dvoulgaridis/bulk-mail/internal/validation"
)

type suppressionRequest struct {
	Emails []string `json:"emails"`
	Reason string   `json:"reason"`
}

func (s *Server) handleSuppressions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.repo.ListSuppressions(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var body suppressionRequest
		if !readJSON(w, r, &body) {
			return
		}
		normalized := make([]string, 0, len(body.Emails))
		seen := map[string]bool{}
		for _, value := range body.Emails {
			email, err := validation.NormalizeEmail(value)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if !seen[email] {
				seen[email] = true
				normalized = append(normalized, email)
			}
		}
		if len(normalized) == 0 {
			writeError(w, http.StatusBadRequest, "at least one email is required")
			return
		}
		for _, email := range normalized {
			if err := s.repo.AddSuppression(r.Context(), email, strings.TrimSpace(body.Reason)); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		items, err := s.repo.ListSuppressions(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, items)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSuppressionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, ok := pathID(w, r.URL.Path, "/api/suppressions/")
	if !ok {
		return
	}
	err := s.repo.DeleteSuppression(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "suppression not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
