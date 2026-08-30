package smtp

import (
	"context"
	"errors"
	"fmt"
	"net"
	netmail "net/mail"
	netsmtp "net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/dvoulgaridis/bulk-mail/internal/mail"
)

const gracefulCloseTimeout = 5 * time.Second

type Config struct {
	Endpoint
	Username string
	Password string
	Identity mail.SenderIdentity
}

type Sender struct {
	Config         Config
	ConnectTimeout time.Duration
	mu             sync.Mutex
	client         *netsmtp.Client
	connection     net.Conn
}

func (sender *Sender) Send(ctx context.Context, message mail.Message) (mail.SendResult, error) {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if err := validateConfig(sender.Config); err != nil {
		return mail.SendResult{}, mail.WrapError(mail.ErrorConfiguration, err)
	}
	if _, err := netmail.ParseAddress(message.ToEmail); err != nil {
		return mail.SendResult{}, mail.WrapError(
			mail.ErrorPermanent,
			fmt.Errorf("recipient address: %w", err),
		)
	}
	if sender.client == nil {
		client, connection, err := sender.connect(ctx)
		if err != nil {
			return mail.SendResult{}, err
		}
		sender.client = client
		sender.connection = connection
	}
	client := sender.client
	stopCancellation := context.AfterFunc(ctx, func() { _ = client.Close() })
	result, err := sendMessage(ctx, client, sender.Config.Identity, message)
	stopCancellation()
	if err != nil {
		_ = client.Close()
		sender.client = nil
		sender.connection = nil
	}
	return result, err
}

func (sender *Sender) TestConnection(ctx context.Context) error {
	if err := validateConfig(sender.Config); err != nil {
		return mail.WrapError(mail.ErrorConfiguration, err)
	}
	client, _, err := sender.connect(ctx)
	if err != nil {
		return err
	}
	stopCancellation := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stopCancellation()
	defer client.Close()
	return nil
}

func (sender *Sender) Close() error {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if sender.client == nil {
		return nil
	}
	client := sender.client
	connection := sender.connection
	sender.client = nil
	sender.connection = nil

	if connection == nil {
		return client.Close()
	}
	if err := connection.SetDeadline(time.Now().Add(gracefulCloseTimeout)); err != nil {
		return errors.Join(err, client.Close())
	}
	if err := client.Quit(); err != nil {
		return errors.Join(err, client.Close())
	}
	return nil
}

func (sender *Sender) connect(ctx context.Context) (*netsmtp.Client, net.Conn, error) {
	client, connection, secure, err := openClient(
		ctx,
		sender.Config.Endpoint,
		sender.connectTimeout(),
	)
	if err != nil {
		return nil, nil, err
	}
	if sender.Config.Username == "" && sender.Config.Password == "" {
		_ = connection.SetDeadline(time.Time{})
		return client, connection, nil
	}
	if !secure {
		_ = client.Close()
		return nil, nil, mail.WrapError(
			mail.ErrorConfiguration,
			errors.New("smtp auth requires TLS or STARTTLS"),
		)
	}
	auth := netsmtp.PlainAuth(
		"",
		sender.Config.Username,
		sender.Config.Password,
		sender.Config.Host,
	)
	if err := client.Auth(auth); err != nil {
		_ = client.Close()
		return nil, nil, mail.WrapError(
			mail.ErrorConfiguration,
			fmt.Errorf("smtp auth: %w", err),
		)
	}
	_ = connection.SetDeadline(time.Time{})
	return client, connection, nil
}

func (sender *Sender) connectTimeout() time.Duration {
	if sender.ConnectTimeout > 0 {
		return sender.ConnectTimeout
	}
	return defaultConnectTimeout
}

func sendMessage(
	ctx context.Context,
	client *netsmtp.Client,
	identity mail.SenderIdentity,
	message mail.Message,
) (mail.SendResult, error) {
	if err := client.Mail(strings.TrimSpace(identity.Email)); err != nil {
		if ctx.Err() != nil {
			return mail.SendResult{}, ctx.Err()
		}
		return mail.SendResult{}, fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err := client.Rcpt(strings.TrimSpace(message.ToEmail)); err != nil {
		if ctx.Err() != nil {
			return mail.SendResult{}, ctx.Err()
		}
		return mail.SendResult{}, fmt.Errorf("smtp RCPT TO: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		if ctx.Err() != nil {
			return mail.SendResult{}, ctx.Err()
		}
		return mail.SendResult{}, fmt.Errorf("smtp DATA: %w", err)
	}
	if err := mail.WriteMessage(writer, identity, message); err != nil {
		_ = writer.Close()
		return mail.SendResult{}, fmt.Errorf("write message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return mail.SendResult{}, fmt.Errorf("close message: %w", err)
	}
	return mail.SendResult{}, nil
}

func validateConfig(config Config) error {
	if err := validateEndpoint(normalizeEndpoint(config.Endpoint)); err != nil {
		return err
	}
	if strings.TrimSpace(config.Identity.Email) == "" {
		return errors.New("sender email is required")
	}
	if _, err := netmail.ParseAddress(config.Identity.Email); err != nil {
		return fmt.Errorf("sender address: %w", err)
	}
	return nil
}
