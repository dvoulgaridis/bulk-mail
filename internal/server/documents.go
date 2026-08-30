package server

import (
	"net/http"

	"github.com/dvoulgaridis/bulk-mail/internal/app"
)

// Document-generation execution.

func (s *Server) handleCampaignGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var command app.ExecuteCampaignCommand
	if !readJSON(w, r, &command) {
		return
	}
	task, err := s.campaignService.QueueDocumentGeneration(r.Context(), command)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, task)
}
