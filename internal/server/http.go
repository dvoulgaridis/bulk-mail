package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	localTokenCookieName = "bulk_mail_token"
	maxJSONRequestBytes  = 80 << 20
)

func (s *Server) localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			http.Error(w, "loopback only", http.StatusForbidden)
			return
		}
		if !allowedHost(r.Host) {
			http.Error(w, "local host required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleSessionBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !allowedOrigin(r) {
		writeError(w, http.StatusForbidden, "same origin required")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     localTokenCookieName,
		Value:    s.token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) requireLocalToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if isMutatingMethod(r.Method) && !allowedOrigin(r) {
			writeError(w, http.StatusForbidden, "same origin required")
			return
		}
		if s.requestToken(r) != s.token {
			writeError(w, http.StatusForbidden, "local token required")
			return
		}
		next(w, r)
	}
}

func (s *Server) requestToken(r *http.Request) string {
	if token := r.Header.Get("X-Bulk-Mail-Token"); token != "" {
		return token
	}
	cookie, err := r.Cookie(localTokenCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func allowedHost(hostport string) bool {
	host := hostport
	if parsedHost, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsedHost
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	}
}

func allowedOrigin(r *http.Request) bool {
	origins := r.Header.Values("Origin")
	if len(origins) != 1 {
		return false
	}
	origin := strings.TrimSpace(origins[0])
	if origin == "" || origin == "null" {
		return false
	}

	source, err := url.Parse(origin)
	if err != nil || !validOriginURL(source) {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if !strings.EqualFold(source.Scheme, scheme) {
		return false
	}

	target, err := url.Parse("//" + r.Host)
	if err != nil || target.Host == "" || target.User != nil || target.Path != "" {
		return false
	}
	return strings.EqualFold(source.Hostname(), target.Hostname()) &&
		effectivePort(source, scheme) == effectivePort(target, scheme)
}

func validOriginURL(value *url.URL) bool {
	return value.Scheme != "" &&
		value.Host != "" &&
		value.User == nil &&
		value.Path == "" &&
		value.RawQuery == "" &&
		!value.ForceQuery &&
		value.Fragment == ""
}

func effectivePort(value *url.URL, scheme string) string {
	if port := value.Port(); port != "" {
		return port
	}
	if scheme == "https" {
		return "443"
	}
	return "80"
}

func readJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if r.Body == nil {
		writeError(w, http.StatusBadRequest, "request body is required")
		return false
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		if err == io.EOF {
			writeError(w, http.StatusBadRequest, "request body is required")
			return false
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			writeError(w, http.StatusBadRequest, "request body must contain exactly one JSON value")
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func pathID(w http.ResponseWriter, path, prefix string) (int64, bool) {
	raw := strings.TrimPrefix(path, prefix)
	id, err := strconv.ParseInt(strings.Trim(raw, "/"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func randomToken() (string, error) {
	var buf [24]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
