package templates

import (
	"fmt"
	"html"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/dvoulgaridis/bulk-mail/internal/validation"
	nethtml "golang.org/x/net/html"
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

// HTML escaping is safe for text content, not attributes, tag names or raw-text elements.
func ValidateHTMLPlaceholders(input string) error {
	tokens := nethtml.NewTokenizer(strings.NewReader(input))
	rawText := false
	for {
		kind := tokens.Next()
		if kind == nethtml.ErrorToken {
			if tokens.Err() == io.EOF {
				return nil
			}
			return tokens.Err()
		}
		if (kind != nethtml.TextToken || rawText) && len(Keys(string(tokens.Raw()))) > 0 {
			return fmt.Errorf("HTML placeholders must appear in text content, not tags, attributes or raw-text elements")
		}
		if kind == nethtml.StartTagToken {
			name, _ := tokens.TagName()
			switch string(name) {
			case "script", "style", "xmp", "iframe", "noembed", "noframes", "plaintext":
				rawText = true
			}
		} else if kind == nethtml.EndTagToken {
			rawText = false
		}
	}
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
