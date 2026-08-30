package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	mailSMTP "github.com/dvoulgaridis/bulk-mail/internal/mail/smtp"
)

const smtpDetectionTimeout = 60 * time.Second

type smtpDetectRequest struct {
	Email string `json:"email"`
}

func (s *Server) handleSMTPDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body smtpDetectRequest
	if !readJSON(w, r, &body) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), smtpDetectionTimeout)
	defer cancel()
	result, err := mailSMTP.Detect(ctx, body.Email, s.smtpConnectTimeout)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, result)
	case errors.Is(err, mailSMTP.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "SMTP detection timed out")
	default:
		writeError(w, http.StatusInternalServerError, "SMTP detection failed")
	}
}
