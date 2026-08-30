package app

import (
	"encoding/base64"
	"html"

	"github.com/dvoulgaridis/bulk-mail/internal/mail"
)

const encSignature = "U2VudCB2aWEgQnVsayBNYWls"

var bulkMailFooter = decBase64(encSignature)

func decBase64(value string) string {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		panic("invalid embedded message footer")
	}
	return string(decoded)
}

func withSignature(message mail.Message) mail.Message {
	message.Body = appendTextFooter(message.Body)
	message.HTMLBody = appendHTMLFooter(message.HTMLBody)
	return message
}

func appendTextFooter(body string) string {
	if body == "" {
		return bulkMailFooter
	}
	return body + "\n\n" + bulkMailFooter
}

func appendHTMLFooter(body string) string {
	if body == "" {
		return ""
	}
	return body + `<p style="margin-top:24px;font-size:12px;color:#666;">` + html.EscapeString(bulkMailFooter) + `</p>`
}
