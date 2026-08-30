package server

import (
	"context"
	"net/http"

	"github.com/dvoulgaridis/bulk-mail/internal/app"
	"github.com/dvoulgaridis/bulk-mail/internal/mail/gmail"
	"github.com/dvoulgaridis/bulk-mail/internal/store"
)

type dependencyStatusResponse struct {
	Available bool   `json:"available"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

type settingsDependenciesResponse struct {
	LibreOffice dependencyStatusResponse `json:"libreOffice"`
}

type appLimitsResponse struct {
	MaxCampaignAttachmentBytes int `json:"maxCampaignAttachmentBytes"`
	MaxAddressListFields       int `json:"maxAddressListFields"`
}

type appIntegrationsResponse struct {
	Google googleIntegrationResponse `json:"google"`
}

type googleIntegrationResponse struct {
	OAuthConfigured bool   `json:"oauthConfigured"`
	SendEndpoint    string `json:"sendEndpoint"`
}

type stateResponse struct {
	store.AppState
	Limits       appLimitsResponse       `json:"limits"`
	Integrations appIntegrationsResponse `json:"integrations"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	state, err := s.repo.State(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stateResponse{
		AppState: state,
		Limits: appLimitsResponse{
			MaxCampaignAttachmentBytes: app.MaxCampaignAttachmentBytes,
			MaxAddressListFields:       store.MaxAddressListFields,
		},
		Integrations: appIntegrationsResponse{Google: googleIntegrationResponse{
			OAuthConfigured: s.googleClientID != "",
			SendEndpoint:    gmail.SendEndpoint,
		}},
	})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := s.repo.GetAppSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, settings)
	case http.MethodPut:
		var body store.AppSettings
		if !readJSON(w, r, &body) {
			return
		}
		if err := s.repo.SaveAppSettings(r.Context(), body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, body)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSettingsDependencies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	status := s.documentConverter.Status(r.Context())
	writeJSON(w, http.StatusOK, settingsDependenciesResponse{
		LibreOffice: dependencyStatusResponse{
			Available: status.Available,
			Path:      status.Path,
			Version:   status.Version,
			Error:     status.Error,
		},
	})
}

func (s *Server) handleQuit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		_ = s.Shutdown(ctx)
	}()
}
