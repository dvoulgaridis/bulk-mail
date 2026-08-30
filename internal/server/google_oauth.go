package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dvoulgaridis/bulk-mail/internal/mail/gmail"
	"github.com/dvoulgaridis/bulk-mail/internal/store"
)

const googleOAuthAttemptLifetime = 5 * time.Minute

type googleOAuthStartRequest struct {
	ProfileID   int64  `json:"profileId"`
	ProfileName string `json:"profileName"`
}

type googleOAuthStartResponse struct {
	AuthURL string `json:"authUrl"`
}

type googleOAuthCompletion struct {
	Type      string `json:"type"`
	ProfileID int64  `json:"profileId"`
	Message   string `json:"message"`
}

func (s *Server) handleGoogleOAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.googleClientID == "" {
		writeError(
			w,
			http.StatusBadRequest,
			"Google OAuth client ID is not configured; set integrations.google.client_id "+
				"in config.json or BULK_MAIL_GOOGLE_CLIENT_ID",
		)
		return
	}
	var body googleOAuthStartRequest
	if !readJSON(w, r, &body) {
		return
	}
	if body.ProfileID > 0 {
		_, err := s.repo.GetSMTPProfile(r.Context(), body.ProfileID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "sender profile not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.repo.CheckSMTPProfileMutable(r.Context(), body.ProfileID); err != nil {
			if errors.Is(err, store.ErrProfileInUse) {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	state, verifier, err := newOAuthSecrets()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "secure token generation failed")
		return
	}
	redirectURI := "http://" + r.Host + "/api/oauth/google/callback"
	now := time.Now()
	s.oauthMu.Lock()
	for key, attempt := range s.oauth {
		if now.After(attempt.ExpiresAt) || attempt.ProfileID == body.ProfileID {
			delete(s.oauth, key)
		}
	}
	s.oauth[state] = oauthAttempt{
		ProfileID:   body.ProfileID,
		ProfileName: strings.TrimSpace(body.ProfileName),
		Verifier:    verifier,
		RedirectURI: redirectURI,
		ExpiresAt:   now.Add(googleOAuthAttemptLifetime),
	}
	s.oauthMu.Unlock()
	authURL := "https://accounts.google.com/o/oauth2/v2/auth?" + url.Values{
		"client_id":             {s.googleClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {"openid email profile " + gmail.GoogleSendScope},
		"state":                 {state},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}.Encode()
	writeJSON(w, http.StatusOK, googleOAuthStartResponse{AuthURL: authURL})
}

func (s *Server) handleGoogleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		writeOAuthCompletion(w, http.StatusBadRequest, 0, "missing OAuth state")
		return
	}
	s.oauthMu.Lock()
	attempt, ok := s.oauth[state]
	if ok {
		delete(s.oauth, state)
	}
	s.oauthMu.Unlock()
	if !ok || time.Now().After(attempt.ExpiresAt) {
		writeOAuthCompletion(w, http.StatusBadRequest, 0, "OAuth attempt expired")
		return
	}
	if oauthError := strings.TrimSpace(r.URL.Query().Get("error")); oauthError != "" {
		writeOAuthCompletion(
			w,
			http.StatusBadRequest,
			attempt.ProfileID,
			"Google authorization was not completed: "+oauthError,
		)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeOAuthCompletion(w, http.StatusBadRequest, attempt.ProfileID, "missing OAuth authorization code")
		return
	}
	token, err := gmail.ExchangeGoogleCode(
		r.Context(),
		s.googleClientID,
		code,
		attempt.Verifier,
		attempt.RedirectURI,
	)
	if err != nil {
		writeOAuthCompletion(w, http.StatusBadRequest, attempt.ProfileID, err.Error())
		return
	}
	if token.RefreshToken == "" {
		writeOAuthCompletion(w, http.StatusBadRequest, attempt.ProfileID, "Google did not return a refresh token")
		return
	}
	if !gmail.HasGoogleScope(token.Scope, gmail.GoogleSendScope) {
		writeOAuthCompletion(w, http.StatusBadRequest, attempt.ProfileID, "Google did not grant Gmail sending permission")
		return
	}
	identity, err := gmail.ValidateGoogleIdentity(r.Context(), s.googleClientID, token.IDToken)
	if err != nil {
		writeOAuthCompletion(w, http.StatusBadRequest, attempt.ProfileID, err.Error())
		return
	}
	profile, err := s.googleOAuthProfile(r.Context(), attempt.ProfileID, attempt.ProfileName, identity)
	if err != nil {
		writeOAuthCompletion(w, http.StatusBadRequest, attempt.ProfileID, err.Error())
		return
	}
	refreshToken, err := s.encryptCredential(store.CredentialGmailRefreshToken, token.RefreshToken)
	if err != nil {
		writeOAuthCompletion(w, http.StatusInternalServerError, attempt.ProfileID, "Google credential could not be encrypted")
		return
	}
	saved, err := s.repo.SaveSMTPProfileWithCredentialChanges(
		r.Context(),
		profile,
		[]store.ProfileCredential{refreshToken},
		[]string{store.CredentialSMTPPassword},
	)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrProfileInUse) {
			status = http.StatusConflict
		}
		writeOAuthCompletion(w, status, attempt.ProfileID, err.Error())
		return
	}
	writeOAuthCompletion(w, http.StatusOK, saved.ID, "Gmail connected successfully")
}

func (s *Server) googleOAuthProfile(
	ctx context.Context,
	profileID int64,
	profileName string,
	identity gmail.GoogleIdentity,
) (store.SMTPProfile, error) {
	if profileID == 0 {
		if profileName == "" {
			profileName = "Gmail — " + identity.Email
		}
		return store.SMTPProfile{
			Name:        profileName,
			ProfileType: store.ProfileTypeGmailOAuth,
			SenderEmail: identity.Email,
			SenderName:  identity.Name,
		}, nil
	}
	profile, err := s.repo.GetSMTPProfile(ctx, profileID)
	if err != nil {
		return store.SMTPProfile{}, err
	}
	profile.ProfileType = store.ProfileTypeGmailOAuth
	profile.SenderEmail = identity.Email
	if strings.TrimSpace(profile.SenderName) == "" {
		profile.SenderName = identity.Name
	}
	return profile, nil
}

func newOAuthSecrets() (string, string, error) {
	state, err := randomToken()
	if err != nil {
		return "", "", err
	}
	verifierA, err := randomToken()
	if err != nil {
		return "", "", err
	}
	verifierB, err := randomToken()
	if err != nil {
		return "", "", err
	}
	return state, verifierA + verifierB, nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func writeOAuthCompletion(w http.ResponseWriter, status int, profileID int64, message string) {
	eventType := "google-oauth-complete"
	if status < 200 || status >= 300 {
		eventType = "google-oauth-error"
	}
	payload, _ := json.Marshal(googleOAuthCompletion{
		Type:      eventType,
		ProfileID: profileID,
		Message:   message,
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set(
		"Content-Security-Policy",
		"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'",
	)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	const completionPage = `<!doctype html><html><head><meta charset="utf-8">` +
		`<title>Bulk Mail</title></head><body><p>%s</p><script>` +
		`const channel=new BroadcastChannel("bulk-mail-google-oauth");` +
		`channel.postMessage(%s);setTimeout(()=>{channel.close();` +
		`if(window.opener){window.opener=null}window.close()},100);` +
		`</script></body></html>`
	_, _ = fmt.Fprintf(w, completionPage, html.EscapeString(message), payload)
}
