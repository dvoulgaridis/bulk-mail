package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dvoulgaridis/bulk-mail/internal/mail"
)

const SendEndpoint = "https://gmail.googleapis.com/gmail/v1/users/me/messages/send"

type Sender struct {
	HTTPClient     *http.Client
	AccessToken    string
	RefreshToken   string
	GoogleClientID string
	Identity       mail.SenderIdentity
}

func (sender *Sender) Send(ctx context.Context, message mail.Message) (mail.SendResult, error) {
	return sender.send(ctx, message, true)
}

func (sender *Sender) send(
	ctx context.Context,
	message mail.Message,
	allowRefresh bool,
) (mail.SendResult, error) {
	client := sender.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	requestBody, writeResult := requestBody(sender.Identity, message)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, SendEndpoint, requestBody)
	if err != nil {
		_ = requestBody.Close()
		<-writeResult
		return mail.SendResult{}, err
	}
	request.Header.Set("Authorization", "Bearer "+sender.AccessToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	_ = requestBody.Close()
	writeErr := <-writeResult
	if err != nil {
		if ctx.Err() != nil {
			return mail.SendResult{}, mail.WrapError(mail.ErrorCancelled, ctx.Err())
		}
		return mail.SendResult{}, mail.WrapError(mail.ErrorTransient, err)
	}
	defer response.Body.Close()
	if writeErr != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		return mail.SendResult{}, mail.WrapError(
			mail.ErrorTransient,
			fmt.Errorf("encode Gmail message: %w", writeErr),
		)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if readErr != nil {
		return mail.SendResult{}, mail.WrapError(mail.ErrorTransient, readErr)
	}
	if response.StatusCode == http.StatusUnauthorized && allowRefresh &&
		strings.TrimSpace(sender.RefreshToken) != "" {
		accessToken, refreshErr := RefreshGoogleAccessToken(
			ctx,
			sender.GoogleClientID,
			sender.RefreshToken,
		)
		if refreshErr != nil {
			return mail.SendResult{}, mail.WrapError(
				mail.ErrorConfiguration,
				fmt.Errorf("refresh Gmail access token: %w", refreshErr),
			)
		}
		sender.AccessToken = accessToken
		return sender.send(ctx, message, false)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return mail.SendResult{}, mail.WrapError(
			errorKind(response.StatusCode, body),
			fmt.Errorf("gmail send: %s", response.Status),
		)
	}
	var result struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body, &result)
	return mail.SendResult{ProviderMessageID: result.ID}, nil
}

func requestBody(identity mail.SenderIdentity, message mail.Message) (*io.PipeReader, <-chan error) {
	reader, writer := io.Pipe()
	result := make(chan error, 1)
	go func() {
		err := writeRequest(writer, identity, message)
		_ = writer.CloseWithError(err)
		result <- err
	}()
	return reader, result
}

func writeRequest(writer io.Writer, identity mail.SenderIdentity, message mail.Message) error {
	if _, err := io.WriteString(writer, `{"raw":"`); err != nil {
		return err
	}
	encoder := base64.NewEncoder(base64.RawURLEncoding, writer)
	if err := mail.WriteMessage(encoder, identity, message); err != nil {
		_ = encoder.Close()
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	_, err := io.WriteString(writer, `"}`)
	return err
}

func errorKind(status int, body []byte) mail.ErrorKind {
	lower := strings.ToLower(string(body))
	if status == http.StatusTooManyRequests || status >= 500 ||
		strings.Contains(lower, "ratelimitexceeded") ||
		strings.Contains(lower, "userratelimitexceeded") ||
		strings.Contains(lower, "quotaexceeded") {
		return mail.ErrorTransient
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return mail.ErrorConfiguration
	}
	return mail.ErrorPermanent
}

func (sender *Sender) Close() error { return nil }
