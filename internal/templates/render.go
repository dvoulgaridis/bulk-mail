package templates

import (
	"html"
	"regexp"
	"sort"
	"strings"

	"github.com/dvoulgaridis/bulk-mail/internal/validation"
)

var tokenPattern = regexp.MustCompile(`\{\{\s*([\p{L}\p{N}\p{M}_.-]+)\s*\}\}`)

// Keys returns the normalized placeholder keys referenced by input.
func Keys(input string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, matches := range tokenPattern.FindAllStringSubmatch(input, -1) {
		if len(matches) != 2 {
			continue
		}
		key, err := validation.NormalizePlaceholderKey(strings.TrimSpace(matches[1]))
		if err != nil || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func RenderText(input string, fields map[string]string) string {
	return render(input, fields, func(value string) string { return value })
}

func RenderHTML(input string, fields map[string]string) string {
	return render(input, fields, html.EscapeString)
}

func render(input string, fields map[string]string, transform func(string) string) string {
	return tokenPattern.ReplaceAllStringFunc(input, func(token string) string {
		matches := tokenPattern.FindStringSubmatch(token)
		if len(matches) != 2 {
			return token
		}
		key, err := validation.NormalizePlaceholderKey(strings.TrimSpace(matches[1]))
		if err != nil {
			return token
		}
		if value, ok := fields[key]; ok {
			return transform(value)
		}
		return token
	})
}
