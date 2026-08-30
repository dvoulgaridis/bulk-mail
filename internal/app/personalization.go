package app

import (
	"maps"
	"strings"
	"unicode"

	"github.com/dvoulgaridis/bulk-mail/internal/store"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

type PersonalizationOptions = store.PersonalizationOptions

func validatePersonalization(options PersonalizationOptions) error {
	for _, value := range []string{options.FirstNameFormat, options.LastNameFormat, options.FullNameFormat} {
		switch normalizedFormat(value) {
		case "preserve", "upper", "title":
		default:
			return failure(ErrorValidation, "name format must be preserve, upper, or title", nil)
		}
	}
	return nil
}

func personalizedFields(
	entry store.AddressEntry,
	options PersonalizationOptions,
) map[string]string {
	firstName := formatName(
		cleanPersonalizedValue(
			entry.Fields[string(store.AddressFieldRoleFirstName)],
			options.RemoveDiacritics,
		),
		options.FirstNameFormat,
	)
	lastName := formatName(
		cleanPersonalizedValue(
			entry.Fields[string(store.AddressFieldRoleLastName)],
			options.RemoveDiacritics,
		),
		options.LastNameFormat,
	)
	fields := maps.Clone(entry.Fields)
	if fields == nil {
		fields = make(store.AddressFields)
	}
	fields[string(store.AddressFieldRoleEmail)] = strings.TrimSpace(entry.Email)
	fields[string(store.AddressFieldRoleFirstName)] = firstName
	fields[string(store.AddressFieldRoleLastName)] = lastName
	fields["full_name"] = formatName(
		strings.TrimSpace(strings.Join([]string{firstName, lastName}, " ")),
		options.FullNameFormat,
	)
	return fields
}

func personalizedName(entry store.AddressEntry, fields map[string]string) string {
	name := strings.TrimSpace(fields["full_name"])
	if name != "" {
		return name
	}
	return entry.Email
}

func formatName(value, format string) string {
	switch normalizedFormat(format) {
	case "upper":
		return strings.ToUpper(value)
	case "title":
		return cases.Title(language.Und).String(strings.ToLower(value))
	default:
		return value
	}
}

func normalizedFormat(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "preserve"
	}
	return value
}

func cleanPersonalizedValue(value string, removeDiacritics bool) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if !removeDiacritics {
		return norm.NFC.String(value)
	}
	decomposed := norm.NFD.String(value)
	return norm.NFC.String(strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, decomposed))
}
