package app

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/dvoulgaridis/bulk-mail/internal/mail"
	"github.com/dvoulgaridis/bulk-mail/internal/templates"
	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const encSignature = "Cjxicj48cCBzdHlsZT0ibWFyZ2luOjA7Zm9udC1zaXplOjEycHg7Y29sb3I6IzY2NjsiPlNlbnQgdmlhIDxhIGhy" +
	"ZWY9Imh0dHBzOi8vZ2l0aHViLmNvbS9kdm91bGdhcmlkaXMvYnVsay1tYWlsIj5CdWxrIE1haWw8L2E+PC9wPg=="

var bulkMailFooter = decBase64(encSignature)

func validateMessage(message mail.MessageContent, substitute bool) error {
	if message.BodyFormat != "text" && message.BodyFormat != "html" {
		return failure(ErrorValidation, "message format must be text or html", nil)
	}
	if message.BodyFormat == "html" && substitute {
		if err := templates.ValidateHTMLPlaceholders(message.Body); err != nil {
			return failure(ErrorValidation, err.Error(), err)
		}
	}
	return nil
}

func decBase64(value string) string {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		panic("invalid embedded message footer")
	}
	return string(decoded)
}

func withSignature(message mail.Message) (mail.Message, error) {
	body, err := appendHTMLFooter(message.Body)
	if err != nil {
		return mail.Message{}, fmt.Errorf("append message footer: %w", err)
	}
	message.Body = body
	return message, nil
}

func appendHTMLFooter(body string) (string, error) {
	document, err := nethtml.Parse(strings.NewReader(body))
	if err != nil {
		return "", err
	}
	var bodyNode *nethtml.Node
	for node := range document.Descendants() {
		if node.Type == nethtml.ElementNode && node.DataAtom == atom.Body {
			bodyNode = node
			break
		}
	}
	if bodyNode == nil {
		return "", fmt.Errorf("HTML message has no body element")
	}
	footer, err := nethtml.ParseFragment(strings.NewReader(bulkMailFooter), bodyNode)
	if err != nil {
		return "", err
	}
	for _, node := range footer {
		bodyNode.AppendChild(node)
	}
	var output strings.Builder
	if err := nethtml.Render(&output, document); err != nil {
		return "", err
	}
	return output.String(), nil
}
