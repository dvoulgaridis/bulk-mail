package mail

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"path/filepath"
	"strings"
	"time"
)

func WriteMessage(writer io.Writer, identity SenderIdentity, message Message) error {
	from := mail.Address{Name: identity.Name, Address: identity.Email}
	to := mail.Address{Name: message.ToName, Address: message.ToEmail}
	bodyContentType := "text/plain; charset=utf-8"
	boundary := ""
	if len(message.Attachments) > 0 {
		boundary = mimeBoundary(identity.Email)
		bodyContentType = fmt.Sprintf("multipart/mixed; boundary=%q", boundary)
	} else if strings.TrimSpace(message.HTMLBody) != "" {
		boundary = mimeBoundary(identity.Email)
		bodyContentType = fmt.Sprintf("multipart/alternative; boundary=%q", boundary)
	}
	headers := []string{
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"Message-ID: " + messageID(identity.Email),
		"From: " + from.String(),
		"To: " + to.String(),
		"Subject: " + encodeHeader(message.Subject),
		"MIME-Version: 1.0",
		"Content-Type: " + bodyContentType,
	}
	if strings.TrimSpace(identity.ReplyTo) != "" {
		headers = append(headers, "Reply-To: "+sanitizeHeader(identity.ReplyTo))
	}
	if message.RequestDeliveryNotice {
		noticeTo := identity.Email
		if strings.TrimSpace(identity.ReplyTo) != "" {
			noticeTo = identity.ReplyTo
		}
		headers = append(
			headers,
			"Disposition-Notification-To: "+sanitizeHeader(noticeTo),
			"Return-Receipt-To: "+sanitizeHeader(noticeTo),
		)
	}
	if _, err := io.WriteString(writer, strings.Join(headers, "\r\n")+"\r\n\r\n"); err != nil {
		return err
	}
	if len(message.Attachments) == 0 && strings.TrimSpace(message.HTMLBody) == "" {
		_, err := io.WriteString(writer, crlf(message.Body))
		return err
	}
	if len(message.Attachments) == 0 {
		return writeAlternativeBody(writer, message, boundary)
	}
	return writeMultipartBody(writer, message, boundary)
}

func messageID(sender string) string {
	var buffer [12]byte
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	if _, err := rand.Read(buffer[:]); err == nil {
		suffix = hex.EncodeToString(buffer[:])
	}
	_, domain, found := strings.Cut(strings.TrimSpace(sender), "@")
	if !found || domain == "" {
		domain = "bulk-mail.local"
	}
	return fmt.Sprintf("<%s@%s>", suffix, sanitizeHeader(domain))
}

func mimeBoundary(seed string) string {
	var buffer [12]byte
	if _, err := rand.Read(buffer[:]); err == nil {
		return "bulk-mail-" + hex.EncodeToString(buffer[:])
	}
	return "bulk-mail-" + fmt.Sprintf("%d", time.Now().UnixNano()) + "-" + sanitizeBoundary(seed)
}

func writeMultipartBody(writer io.Writer, message Message, boundary string) error {
	if _, err := io.WriteString(writer, "--"+boundary+"\r\n"); err != nil {
		return err
	}
	if strings.TrimSpace(message.HTMLBody) != "" {
		alternativeBoundary := mimeBoundary(message.ToEmail)
		contentType := "Content-Type: multipart/alternative; boundary=\"" +
			alternativeBoundary + "\"\r\n\r\n"
		if _, err := io.WriteString(writer, contentType); err != nil {
			return err
		}
		if err := writeAlternativeBody(writer, message, alternativeBoundary); err != nil {
			return err
		}
	} else {
		content := "Content-Type: text/plain; charset=utf-8\r\n" +
			"Content-Transfer-Encoding: 8bit\r\n\r\n" + crlf(message.Body)
		if _, err := io.WriteString(writer, content); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(writer, "\r\n"); err != nil {
		return err
	}
	for _, attachment := range message.Attachments {
		filename := sanitizeFilename(attachment.Filename)
		contentType := strings.TrimSpace(attachment.ContentType)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		header := "--" + boundary + "\r\n" +
			"Content-Type: " + formatFilenameParameter(contentType, "name", filename) + "\r\n" +
			"Content-Transfer-Encoding: base64\r\n" +
			"Content-Disposition: " + formatFilenameParameter("attachment", "filename", filename) + "\r\n\r\n"
		if _, err := io.WriteString(writer, header); err != nil {
			return err
		}
		if err := writeBase64(writer, attachment.Content); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "--"+boundary+"--\r\n")
	return err
}

func writeAlternativeBody(writer io.Writer, message Message, boundary string) error {
	body := "--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" + crlf(message.Body) +
		"\r\n--" + boundary + "\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" + crlf(message.HTMLBody) +
		"\r\n--" + boundary + "--\r\n"
	_, err := io.WriteString(writer, body)
	return err
}

func crlf(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\n", "\r\n")
}

func writeBase64(writer io.Writer, content []byte) error {
	lines := &base64LineWriter{writer: writer}
	encoder := base64.NewEncoder(base64.StdEncoding, lines)
	if _, err := encoder.Write(content); err != nil {
		_ = encoder.Close()
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	_, err := io.WriteString(writer, "\r\n")
	return err
}

type base64LineWriter struct {
	writer io.Writer
	column int
}

func (writer *base64LineWriter) Write(content []byte) (int, error) {
	written := 0
	for len(content) > 0 {
		if writer.column == 76 {
			if _, err := io.WriteString(writer.writer, "\r\n"); err != nil {
				return written, err
			}
			writer.column = 0
		}
		length := min(76-writer.column, len(content))
		count, err := writer.writer.Write(content[:length])
		written += count
		writer.column += count
		content = content[count:]
		if err != nil {
			return written, err
		}
		if count != length {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func sanitizeFilename(value string) string {
	value = strings.TrimSpace(filepath.Base(value))
	if value == "" {
		return "attachment"
	}
	return strings.NewReplacer("\\", "_", "/", "_", "\r", "_", "\n", "_", "\"", "_").Replace(value)
}

func sanitizeBoundary(value string) string {
	return strings.NewReplacer(" ", "-", "@", "-", ".", "-", "\r", "", "\n", "").Replace(value)
}

func sanitizeHeader(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func encodeHeader(value string) string {
	value = sanitizeHeader(value)
	for _, character := range value {
		if character > 127 {
			return mime.QEncoding.Encode("utf-8", value)
		}
	}
	return value
}

func formatFilenameParameter(mediaType, parameter, filename string) string {
	for _, character := range filename {
		if character > 127 {
			return mime.FormatMediaType(mediaType, map[string]string{parameter: filename})
		}
	}
	return mediaType + "; " + parameter + "=\"" + strings.ReplaceAll(filename, "\"", "_") + "\""
}
