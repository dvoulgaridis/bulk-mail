package gmail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	netmail "net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const GoogleSendScope = "https://www.googleapis.com/auth/gmail.send"

type GoogleToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

type GoogleIdentity struct {
	Subject string
	Email   string
	Name    string
}

func ExchangeGoogleCode(
	ctx context.Context,
	clientID string,
	code string,
	verifier string,
	redirectURI string,
) (GoogleToken, error) {
	values := url.Values{
		"client_id":     {strings.TrimSpace(clientID)},
		"code":          {code},
		"code_verifier": {verifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	}
	return googleTokenRequest(ctx, values)
}

func RefreshGoogleAccessToken(ctx context.Context, clientID, refreshToken string) (string, error) {
	values := url.Values{
		"client_id":     {strings.TrimSpace(clientID)},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	token, err := googleTokenRequest(ctx, values)
	if err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		return "", errors.New("Google did not return an access token")
	}
	return token.AccessToken, nil
}

func ValidateGoogleIdentity(ctx context.Context, clientID, idToken string) (GoogleIdentity, error) {
	clientID = strings.TrimSpace(clientID)
	idToken = strings.TrimSpace(idToken)
	if clientID == "" || idToken == "" {
		return GoogleIdentity{}, errors.New("Google identity token is missing")
	}
	endpoint := "https://oauth2.googleapis.com/tokeninfo?" +
		url.Values{"id_token": {idToken}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return GoogleIdentity{}, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return GoogleIdentity{}, err
	}
	defer response.Body.Close()
	var info struct {
		Audience      string          `json:"aud"`
		Issuer        string          `json:"iss"`
		Subject       string          `json:"sub"`
		Email         string          `json:"email"`
		EmailVerified json.RawMessage `json:"email_verified"`
		Name          string          `json:"name"`
		ExpiresAt     string          `json:"exp"`
		Error         string          `json:"error_description"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&info); err != nil {
		return GoogleIdentity{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if info.Error != "" {
			return GoogleIdentity{}, errors.New(info.Error)
		}
		return GoogleIdentity{}, fmt.Errorf("validate Google identity: %s", response.Status)
	}
	if info.Audience != clientID {
		return GoogleIdentity{}, errors.New(
			"Google identity token audience does not match the configured client ID",
		)
	}
	if info.Issuer != "accounts.google.com" && info.Issuer != "https://accounts.google.com" {
		return GoogleIdentity{}, errors.New("Google identity token issuer is invalid")
	}
	if strings.TrimSpace(info.Subject) == "" {
		return GoogleIdentity{}, errors.New("Google identity does not contain a stable subject")
	}
	verified, err := googleBoolean(info.EmailVerified)
	if err != nil || !verified {
		return GoogleIdentity{}, errors.New("Google account email is not verified")
	}
	address, err := netmail.ParseAddress(strings.TrimSpace(info.Email))
	if err != nil || !strings.EqualFold(address.Address, strings.TrimSpace(info.Email)) {
		return GoogleIdentity{}, errors.New("Google identity email is invalid")
	}
	if info.ExpiresAt != "" {
		expiresAt, err := strconv.ParseInt(info.ExpiresAt, 10, 64)
		if err != nil || time.Now().Unix() >= expiresAt {
			return GoogleIdentity{}, errors.New("Google identity token has expired")
		}
	}
	return GoogleIdentity{
		Subject: strings.TrimSpace(info.Subject),
		Email:   strings.ToLower(strings.TrimSpace(info.Email)),
		Name:    strings.TrimSpace(info.Name),
	}, nil
}

func HasGoogleScope(scopes, required string) bool {
	for _, scope := range strings.Fields(scopes) {
		if scope == required {
			return true
		}
	}
	return false
}

func googleBoolean(value json.RawMessage) (bool, error) {
	var boolean bool
	if err := json.Unmarshal(value, &boolean); err == nil {
		return boolean, nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return false, err
	}
	return strconv.ParseBool(text)
}

func googleTokenRequest(ctx context.Context, values url.Values) (GoogleToken, error) {
	if values.Get("client_id") == "" {
		return GoogleToken{}, errors.New("Google OAuth client ID is not configured")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://oauth2.googleapis.com/token",
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return GoogleToken{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return GoogleToken{}, err
	}
	defer response.Body.Close()
	var token GoogleToken
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&token); err != nil {
		return GoogleToken{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if token.Description != "" {
			return GoogleToken{}, errors.New(token.Description)
		}
		if token.Error != "" {
			return GoogleToken{}, errors.New(token.Error)
		}
		return GoogleToken{}, fmt.Errorf("Google token exchange: %s", response.Status)
	}
	return token, nil
}
