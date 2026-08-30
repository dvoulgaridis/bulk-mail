package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	netsmtp "net/smtp"
	"strings"
	"time"

	"github.com/dvoulgaridis/bulk-mail/internal/mail"
)

const defaultConnectTimeout = 15 * time.Second

type Endpoint struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	TLSMode string `json:"tlsMode"`
}

func openClient(
	ctx context.Context,
	endpoint Endpoint,
	connectTimeout time.Duration,
) (*netsmtp.Client, net.Conn, bool, error) {
	endpoint = normalizeEndpoint(endpoint)
	if err := validateEndpoint(endpoint); err != nil {
		return nil, nil, false, mail.WrapError(mail.ErrorConfiguration, err)
	}
	if connectTimeout <= 0 {
		connectTimeout = defaultConnectTimeout
	}

	address := net.JoinHostPort(endpoint.Host, fmt.Sprintf("%d", endpoint.Port))
	dialer := &net.Dialer{Timeout: connectTimeout}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, false, mail.WrapError(
			mail.ErrorTransient,
			fmt.Errorf("smtp connect: %w", err),
		)
	}
	setupConnection := connection
	stopCancellation := context.AfterFunc(ctx, func() {
		_ = setupConnection.Close()
	})
	defer stopCancellation()
	_ = connection.SetDeadline(operationDeadline(ctx, connectTimeout))

	tlsConfig := &tls.Config{
		ServerName: endpoint.Host,
		MinVersion: tls.VersionTLS12,
	}
	secure := false
	if endpoint.TLSMode == "tls" {
		tlsConnection := tls.Client(connection, tlsConfig)
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			_ = connection.Close()
			return nil, nil, false, mail.WrapError(
				mail.ErrorConfiguration,
				fmt.Errorf("smtp tls handshake: %w", err),
			)
		}
		connection = tlsConnection
		secure = true
	}

	client, err := netsmtp.NewClient(connection, endpoint.Host)
	if err != nil {
		_ = connection.Close()
		return nil, nil, false, fmt.Errorf("smtp client: %w", err)
	}
	if endpoint.TLSMode == "starttls" {
		if available, _ := client.Extension("STARTTLS"); !available {
			_ = client.Close()
			return nil, nil, false, mail.WrapError(
				mail.ErrorConfiguration,
				errors.New("smtp server does not advertise STARTTLS"),
			)
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			_ = client.Close()
			return nil, nil, false, mail.WrapError(
				mail.ErrorConfiguration,
				fmt.Errorf("smtp STARTTLS: %w", err),
			)
		}
		secure = true
	}
	return client, connection, secure, nil
}

func normalizeEndpoint(endpoint Endpoint) Endpoint {
	endpoint.Host = strings.TrimSpace(endpoint.Host)
	endpoint.TLSMode = strings.ToLower(strings.TrimSpace(endpoint.TLSMode))
	if endpoint.Port == 465 {
		endpoint.TLSMode = "tls"
	} else if endpoint.TLSMode == "" {
		endpoint.TLSMode = "starttls"
	}
	return endpoint
}

func validateEndpoint(endpoint Endpoint) error {
	if endpoint.Host == "" {
		return errors.New("smtp host is required")
	}
	if endpoint.Port < 1 || endpoint.Port > 65535 {
		return errors.New("smtp port must be between 1 and 65535")
	}
	switch endpoint.TLSMode {
	case "none", "starttls", "tls":
		return nil
	default:
		return errors.New("smtp security must be none, STARTTLS, or TLS")
	}
}

func operationDeadline(ctx context.Context, timeout time.Duration) time.Time {
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		return contextDeadline
	}
	return deadline
}
