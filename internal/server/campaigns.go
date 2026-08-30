package server

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/dvoulgaridis/bulk-mail/internal/app"
	"github.com/dvoulgaridis/bulk-mail/internal/store"
)

// Campaign persistence.

func (s *Server) handleCampaigns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var command app.SaveCampaignCommand
	if !readJSON(w, r, &command) {
		return
	}
	saved, err := s.campaignService.SaveCampaign(
		r.Context(),
		store.NewCampaignID,
		command,
	)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) handleCampaignByID(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r.URL.Path, "/api/campaigns/")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		campaign, err := s.repo.GetCampaign(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "campaign not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load campaign failed")
			return
		}
		writeJSON(w, http.StatusOK, campaign)
	case http.MethodPut:
		var command app.SaveCampaignCommand
		if !readJSON(w, r, &command) {
			return
		}
		saved, err := s.campaignService.SaveCampaign(r.Context(), id, command)
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, saved)
	case http.MethodDelete:
		if err := s.repo.DeleteCampaign(r.Context(), id); err != nil {
			switch {
			case errors.Is(err, store.ErrCampaignInUse):
				writeError(w, http.StatusConflict, err.Error())
			case errors.Is(err, sql.ErrNoRows):
				writeError(w, http.StatusNotFound, "campaign not found")
			default:
				writeError(w, http.StatusInternalServerError, "delete campaign failed")
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPut+", "+http.MethodDelete)
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// Campaign validation and execution.

func (s *Server) handleCampaignPreflight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var command app.PreflightCampaignCommand
	if !readJSON(w, r, &command) {
		return
	}
	result, err := s.campaignService.Preflight(r.Context(), command)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCampaignSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var command app.ExecuteCampaignCommand
	if !readJSON(w, r, &command) {
		return
	}
	task, err := s.campaignService.QueueCampaign(r.Context(), command)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, task)
}

// Application error responses.

func writeAppError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch app.ErrorKindOf(err) {
	case app.ErrorValidation:
		status = http.StatusBadRequest
	case app.ErrorNotFound:
		status = http.StatusNotFound
	case app.ErrorProcessing:
		status = http.StatusUnprocessableEntity
	case app.ErrorCapacity:
		status = http.StatusServiceUnavailable
	}
	writeError(w, status, err.Error())
}
