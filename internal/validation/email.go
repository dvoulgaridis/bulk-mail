package validation

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"
)

const (
	MaxEmailLength = 254
	MaxFieldLength = 255
)

func NormalizeEmail(value string) (string, error) {
	email := strings.TrimSpace(value)
	if email == "" {
		return "", errors.New("email is required")
	}
	if len(email) > MaxEmailLength {
		return "", fmt.Errorf("email exceeds %d characters", MaxEmailLength)
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return "", fmt.Errorf("invalid email %q", email)
	}
	if addr.Address != email || strings.ContainsAny(addr.Name, "<>") {
		return "", fmt.Errorf("invalid email %q", email)
	}
	local, domain, ok := strings.Cut(addr.Address, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "..") {
		return "", fmt.Errorf("invalid email %q", email)
	}
	return strings.ToLower(addr.Address), nil
}

func TrimField(value, name string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if utf8.RuneCountInString(trimmed) > MaxFieldLength {
		return "", fmt.Errorf("%s exceeds %d characters", name, MaxFieldLength)
	}
	return trimmed, nil
}
