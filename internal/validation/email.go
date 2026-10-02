package validation

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxEmailLength = 254
	MaxFieldLength = 255
)

func NormalizeEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	if email == "" {
		return "", errors.New("email is required")
	}
	if len(email) > MaxEmailLength {
		return "", fmt.Errorf("email exceeds %d bytes", MaxEmailLength)
	}
	if strings.Count(email, "@") != 1 || strings.ContainsFunc(email, unicode.IsSpace) {
		return "", fmt.Errorf("invalid email %q", email)
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return "", fmt.Errorf("invalid email %q", email)
	}
	if addr.Address != email || addr.Name != "" {
		return "", fmt.Errorf("invalid email %q", email)
	}
	local, domain, ok := strings.Cut(addr.Address, "@")
	if !ok || local == "" || !validEmailDomain(domain) {
		return "", fmt.Errorf("invalid email %q", email)
	}
	return email, nil
}

func validEmailDomain(domain string) bool {
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, char := range label {
			if char != '-' && !unicode.IsLetter(char) && !unicode.IsDigit(char) {
				return false
			}
		}
	}
	return true
}

func TrimField(value, name string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if utf8.RuneCountInString(trimmed) > MaxFieldLength {
		return "", fmt.Errorf("%s exceeds %d characters", name, MaxFieldLength)
	}
	return trimmed, nil
}
