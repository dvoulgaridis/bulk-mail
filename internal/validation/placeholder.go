package validation

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const MaxPlaceholderKeyLength = 64

func NormalizePlaceholderKey(value string) (string, error) {
	key := norm.NFC.String(strings.ToLower(norm.NFC.String(strings.TrimSpace(value))))
	if key == "" {
		return "", errors.New("placeholder key is required")
	}
	if utf8.RuneCountInString(key) > MaxPlaceholderKeyLength {
		return "", fmt.Errorf("placeholder key exceeds %d characters", MaxPlaceholderKeyLength)
	}
	for _, character := range key {
		if unicode.IsLetter(character) || unicode.IsNumber(character) || unicode.IsMark(character) {
			continue
		}
		switch character {
		case '_', '.', '-':
			continue
		default:
			return "", fmt.Errorf("placeholder key %q contains unsupported characters", value)
		}
	}
	return key, nil
}
