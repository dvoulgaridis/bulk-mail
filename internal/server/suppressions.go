package server

import "net/http"

type suppressionRequest struct {
	Emails []string `json:"emails"`
	IDs    []int64  `json:"ids"`
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
	case http.MethodPost, http.MethodDelete:
		var body suppressionRequest
		if !readJSON(w, r, &body) {
			return
		}
		remove := r.Method == http.MethodDelete
		if (remove && (len(body.IDs) == 0 || len(body.Emails) > 0)) ||
			(!remove && (len(body.Emails) == 0 || len(body.IDs) > 0)) {
			writeError(w, http.StatusBadRequest, "provide a nonempty array of emails or deletion IDs")
			return
		}
		writeJSON(w, http.StatusOK, s.repo.WriteSuppressions(r.Context(), body.Emails, body.IDs, body.Reason, remove))
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
