package app

import (
	"maps"
	"strings"
	"unicode"

	"github.com/dvoulgaridis/bulk-mail/internal/mail"
	"github.com/dvoulgaridis/bulk-mail/internal/store"
	"github.com/dvoulgaridis/bulk-mail/internal/templates"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

func validatePersonalization(options store.PersonalizationOptions) error {
	for _, group := range []store.PlaceholderOptions{options.Message, options.Attachments.PlaceholderOptions} {
		for _, value := range []string{group.FirstNameFormat, group.LastNameFormat, group.FullNameFormat} {
			switch normalizedFormat(value) {
			case "preserve", "upper", "title":
			default:
				return failure(ErrorValidation, "name format must be preserve, upper, or title", nil)
			}
		}
	}
	return nil
}

func personalizedFields(
	entry store.AddressEntry,
	options store.PlaceholderOptions,
) map[string]string {
	firstName := cleanPersonalizedValue(
		entry.Fields[string(store.AddressFieldRoleFirstName)],
		options.RemoveDiacritics,
	)
	lastName := cleanPersonalizedValue(
		entry.Fields[string(store.AddressFieldRoleLastName)],
		options.RemoveDiacritics,
	)
	fields := maps.Clone(entry.Fields)
	if fields == nil {
		fields = make(store.AddressFields)
	}
	fields[string(store.AddressFieldRoleFirstName)] = formatName(firstName, options.FirstNameFormat)
	fields[string(store.AddressFieldRoleLastName)] = formatName(lastName, options.LastNameFormat)
	fields["full_name"] = formatName(
		strings.TrimSpace(strings.Join([]string{firstName, lastName}, " ")),
		options.FullNameFormat,
	)
	return fields
}

// renderMessage is shared by preview and delivery. Attachment processing is independent.
func renderMessage(message mail.MessageContent, fields map[string]string, substitute bool) mail.MessageContent {
	if substitute {
		message.Subject = templates.RenderText(message.Subject, fields)
		message.Body = templates.RenderText(message.Body, fields)
		message.HTMLBody = templates.RenderHTML(message.HTMLBody, fields)
	}
	if strings.TrimSpace(message.HTMLBody) == "" {
		message.HTMLBody = ""
	}
	return message
}

func personalizedName(entry store.AddressEntry, fields map[string]string) string {
	name := strings.TrimSpace(fields["full_name"])
	if name != "" {
		return name
	}
	return entry.Fields["email"]
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
