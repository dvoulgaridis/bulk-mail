package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/dvoulgaridis/bulk-mail/internal/mail"
	mailSMTP "github.com/dvoulgaridis/bulk-mail/internal/mail/smtp"
	"github.com/dvoulgaridis/bulk-mail/internal/store"
)

const (
	maxSMTPPasswordCharLength = 1024
	maxSMTPPasswordByteLength = 4096
)

type smtpProfileRequest struct {
	Name        string            `json:"name"`
	ProfileType store.ProfileType `json:"profileType"`
	Host        string            `json:"host"`
	Port        int               `json:"port"`
	TLSMode     string            `json:"tlsMode"`
	Username    string            `json:"username"`
	SenderEmail string            `json:"senderEmail"`
	SenderName  string            `json:"senderName"`
	ReplyTo     string            `json:"replyTo"`
	NewPassword *string           `json:"newPassword,omitempty"`
}

type smtpTestRequest struct {
	ProfileID int64  `json:"profileId"`
	Email     string `json:"toEmail"`
}

type smtpTestResponse struct {
	Status string `json:"status"`
}

func (request smtpProfileRequest) profile(id int64) store.SMTPProfile {
	return store.SMTPProfile{
		ID:          id,
		Name:        request.Name,
		ProfileType: request.ProfileType,
		Host:        request.Host,
		Port:        request.Port,
		TLSMode:     request.TLSMode,
		Username:    request.Username,
		SenderEmail: request.SenderEmail,
		SenderName:  request.SenderName,
		ReplyTo:     request.ReplyTo,
	}
}

func (s *Server) handleSMTPProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		profiles, err := s.repo.ListSMTPProfiles(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, profiles)
	case http.MethodPost:
		var body smtpProfileRequest
		if !readJSON(w, r, &body) {
			return
		}
		saved, err := s.saveSMTPProfile(r.Context(), 0, body)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, saved)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSMTPProfileByID(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r.URL.Path, "/api/smtp/profiles/")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body smtpProfileRequest
		if !readJSON(w, r, &body) {
			return
		}
		saved, err := s.saveSMTPProfile(r.Context(), id, body)
		if err != nil {
			if errors.Is(err, store.ErrProfileInUse) {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
	case http.MethodDelete:
		if err := s.repo.DeleteSMTPProfile(r.Context(), id); err != nil {
			if errors.Is(err, store.ErrProfileInUse) {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) saveSMTPProfile(
	ctx context.Context,
	id int64,
	request smtpProfileRequest,
) (store.SMTPProfile, error) {
	profile, err := store.NormalizeSMTPProfile(request.profile(id))
	if err != nil {
		return store.SMTPProfile{}, err
	}
	var existing store.SMTPProfile
	if profile.ID > 0 {
		existing, err = s.repo.GetSMTPProfile(ctx, profile.ID)
		if err != nil {
			return store.SMTPProfile{}, err
		}
	}
	newPassword, hasNewPassword, err := requestedPassword(request.NewPassword)
	if err != nil {
		return store.SMTPProfile{}, err
	}
	if profile.ProfileType == store.ProfileTypeGmailOAuth {
		if hasNewPassword {
			return store.SMTPProfile{}, errors.New("Gmail OAuth profiles do not use an SMTP password")
		}
		if profile.ID == 0 {
			return store.SMTPProfile{}, errors.New("connect with Google before creating a Gmail OAuth profile")
		}
		if existing.ProfileType != store.ProfileTypeGmailOAuth || !existing.HasGoogleOAuth {
			return store.SMTPProfile{}, errors.New("Gmail OAuth profile is not connected")
		}
		profile.SenderEmail = existing.SenderEmail
	}
	effectivePassword := newPassword
	if profile.ProfileType == store.ProfileTypeGmailAppPassword {
		if !hasNewPassword && profile.ID > 0 && existing.ProfileType == profile.ProfileType {
			effectivePassword, err = s.decryptCredential(ctx, profile.ID, store.CredentialSMTPPassword)
		}
		if err != nil || strings.TrimSpace(effectivePassword) == "" {
			return store.SMTPProfile{}, errors.New("Gmail App Password is required")
		}
		tester := mailSMTP.Sender{
			Config: mailSMTP.Config{
				Endpoint: mailSMTP.Endpoint{
					Host:    profile.Host,
					Port:    profile.Port,
					TLSMode: profile.TLSMode,
				},
				Username: profile.Username,
				Password: effectivePassword,
				Identity: mail.SenderIdentity{
					Email:   profile.SenderEmail,
					Name:    profile.SenderName,
					ReplyTo: profile.ReplyTo,
				},
			},
			ConnectTimeout: s.smtpConnectTimeout,
		}
		if err := tester.TestConnection(ctx); err != nil {
			return store.SMTPProfile{}, err
		}
	}

	var changed []store.ProfileCredential
	if hasNewPassword {
		credential, err := s.encryptCredential(store.CredentialSMTPPassword, newPassword)
		if err != nil {
			return store.SMTPProfile{}, err
		}
		changed = append(changed, credential)
	}
	var deleted []string
	if profile.ProfileType == store.ProfileTypeGmailOAuth {
		deleted = append(deleted, store.CredentialSMTPPassword)
	} else {
		deleted = append(deleted, store.CredentialGmailRefreshToken)
		if profile.ID > 0 && existing.ProfileType != profile.ProfileType && !hasNewPassword {
			deleted = append(deleted, store.CredentialSMTPPassword)
		}
	}
	return s.repo.SaveSMTPProfileWithCredentialChanges(ctx, profile, changed, deleted)
}

func requestedPassword(value *string) (string, bool, error) {
	if value == nil {
		return "", false, nil
	}
	if strings.TrimSpace(*value) == "" {
		return "", false, errors.New("new password cannot be empty")
	}
	if utf8.RuneCountInString(*value) > maxSMTPPasswordCharLength {
		return "", false, fmt.Errorf(
			"new password cannot exceed %d characters",
			maxSMTPPasswordCharLength,
		)
	}
	if len(*value) > maxSMTPPasswordByteLength {
		return "", false, fmt.Errorf(
			"new password cannot exceed %d bytes",
			maxSMTPPasswordByteLength,
		)
	}
	return *value, true, nil
}

func (s *Server) handleSMTPTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body smtpTestRequest
	if !readJSON(w, r, &body) {
		return
	}
	toEmail := strings.TrimSpace(body.Email)
	if err := s.delivery.TestProfile(r.Context(), body.ProfileID, toEmail); err != nil {
		writeAppError(w, err)
		return
	}
	status := "connection ok"
	if toEmail != "" {
		status = "test email sent"
	}
	writeJSON(w, http.StatusOK, smtpTestResponse{Status: status})
}
