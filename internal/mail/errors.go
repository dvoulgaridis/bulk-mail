package mail

import (
	"context"
	"errors"
	"net"
	"net/textproto"
	"strings"
)

type ErrorKind string

const (
	ErrorConfiguration ErrorKind = "configuration"
	ErrorPermanent     ErrorKind = "permanent"
	ErrorTransient     ErrorKind = "transient"
	ErrorCancelled     ErrorKind = "cancelled"
)

type DeliveryError struct {
	Kind ErrorKind
	Err  error
}

func (err *DeliveryError) Error() string { return err.Err.Error() }
func (err *DeliveryError) Unwrap() error { return err.Err }

func WrapError(kind ErrorKind, err error) error {
	if err == nil {
		return nil
	}
	return &DeliveryError{Kind: kind, Err: err}
}

func ClassifyError(err error) ErrorKind {
	if err == nil {
		return ""
	}
	var deliveryError *DeliveryError
	if errors.As(err, &deliveryError) {
		return deliveryError.Kind
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrorCancelled
	}
	var smtpError *textproto.Error
	if errors.As(err, &smtpError) {
		if smtpError.Code == 530 || smtpError.Code == 534 || smtpError.Code == 535 {
			return ErrorConfiguration
		}
		if smtpError.Code >= 400 && smtpError.Code < 500 {
			return ErrorTransient
		}
		if smtpError.Code >= 500 {
			return ErrorPermanent
		}
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return ErrorTransient
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "auth") ||
		strings.Contains(lower, "credential") ||
		strings.Contains(lower, "sender email") ||
		strings.Contains(lower, "smtp host") ||
		strings.Contains(lower, "smtp port") ||
		strings.Contains(lower, "starttls") {
		return ErrorConfiguration
	}
	return ErrorTransient
}
