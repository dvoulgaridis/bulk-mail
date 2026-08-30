package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dvoulgaridis/bulk-mail/internal/credentials"
	"github.com/dvoulgaridis/bulk-mail/internal/mail"
	"github.com/dvoulgaridis/bulk-mail/internal/mail/gmail"
	mailSMTP "github.com/dvoulgaridis/bulk-mail/internal/mail/smtp"
	"github.com/dvoulgaridis/bulk-mail/internal/store"
)

type profileStore interface {
	GetSMTPProfile(context.Context, int64) (store.SMTPProfile, error)
	GetProfileCredential(context.Context, int64, string) (store.ProfileCredential, error)
}

type DeliveryService struct {
	profiles           profileStore
	credentialKey      []byte
	googleClientID     string
	sendTimeout        time.Duration
	smtpConnectTimeout time.Duration
}

func NewDeliveryService(
	profiles profileStore,
	credentialKey []byte,
	googleClientID string,
	sendTimeout time.Duration,
	smtpConnectTimeout time.Duration,
) *DeliveryService {
	return &DeliveryService{
		profiles:           profiles,
		credentialKey:      append([]byte(nil), credentialKey...),
		googleClientID:     strings.TrimSpace(googleClientID),
		sendTimeout:        sendTimeout,
		smtpConnectTimeout: smtpConnectTimeout,
	}
}

func (service *DeliveryService) Sender(
	ctx context.Context,
	profileID int64,
) (mail.Sender, store.SMTPProfile, error) {
	profile, err := service.Profile(ctx, profileID)
	if err != nil {
		return nil, store.SMTPProfile{}, err
	}
	sender, err := service.SenderForProfile(ctx, profile)
	return sender, profile, err
}

func (service *DeliveryService) Profile(ctx context.Context, profileID int64) (store.SMTPProfile, error) {
	if profileID <= 0 {
		return store.SMTPProfile{}, failure(ErrorValidation, "sender profile is required", nil)
	}
	profile, err := service.profiles.GetSMTPProfile(ctx, profileID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.SMTPProfile{}, failure(ErrorNotFound, "sender profile not found", err)
	}
	if err != nil {
		return store.SMTPProfile{}, internalFailure("load sender profile", err)
	}
	return profile, nil
}

func (service *DeliveryService) SenderForProfile(
	ctx context.Context,
	profile store.SMTPProfile,
) (mail.Sender, error) {
	identity := mail.SenderIdentity{
		Email:   profile.SenderEmail,
		Name:    profile.SenderName,
		ReplyTo: profile.ReplyTo,
	}
	if profile.ProfileType == store.ProfileTypeGmailOAuth {
		refreshToken, err := service.credential(ctx, profile.ID, store.CredentialGmailRefreshToken, true)
		if err != nil {
			return nil, err
		}
		accessToken, err := gmail.RefreshGoogleAccessToken(ctx, service.googleClientID, refreshToken)
		if err != nil {
			return nil, failure(ErrorValidation, err.Error(), err)
		}
		return &gmail.Sender{
			AccessToken:    accessToken,
			RefreshToken:   refreshToken,
			GoogleClientID: service.googleClientID,
			Identity:       identity,
		}, nil
	}
	passwordRequired := profile.ProfileType == store.ProfileTypeGmailAppPassword
	password, err := service.credential(
		ctx,
		profile.ID,
		store.CredentialSMTPPassword,
		passwordRequired,
	)
	if err != nil {
		return nil, err
	}
	return &mailSMTP.Sender{
		Config: mailSMTP.Config{
			Endpoint: mailSMTP.Endpoint{
				Host:    profile.Host,
				Port:    profile.Port,
				TLSMode: profile.TLSMode,
			},
			Username: profile.Username,
			Password: password,
			Identity: identity,
		},
		ConnectTimeout: service.smtpConnectTimeout,
	}, nil
}

func (service *DeliveryService) send(
	ctx context.Context,
	sender mail.Sender,
	message mail.Message,
) (mail.SendResult, error) {
	sendCtx, cancel := context.WithTimeout(ctx, service.sendTimeout)
	defer cancel()
	result, err := sender.Send(sendCtx, message)
	if err != nil && errors.Is(sendCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		return result, fmt.Errorf("send timed out after %s", service.sendTimeout)
	}
	return result, err
}

func (service *DeliveryService) TestProfile(ctx context.Context, profileID int64, toEmail string) error {
	sender, _, err := service.Sender(ctx, profileID)
	if err != nil {
		return err
	}
	defer closeSender(sender)

	toEmail = strings.TrimSpace(toEmail)
	if toEmail == "" {
		if tester, ok := sender.(mail.ConnectionTester); ok {
			if err := tester.TestConnection(ctx); err != nil {
				return failure(ErrorValidation, err.Error(), err)
			}
			return nil
		}
		return failure(ErrorValidation, "a test email address is required for this provider", nil)
	}
	_, err = service.send(ctx, sender, withSignature(mail.Message{
		ToEmail: toEmail,
		MessageContent: mail.MessageContent{
			Subject: "Bulk Mail delivery test",
			Body:    "Test.",
		},
	}))
	if err != nil {
		return failure(ErrorValidation, err.Error(), err)
	}
	return nil
}

func (service *DeliveryService) credential(
	ctx context.Context,
	profileID int64,
	credentialType string,
	required bool,
) (string, error) {
	credential, err := service.profiles.GetProfileCredential(ctx, profileID, credentialType)
	if errors.Is(err, sql.ErrNoRows) && !required {
		return "", nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", failure(ErrorValidation, "sender profile credentials are not configured", err)
	}
	if err != nil {
		return "", internalFailure("load sender credentials", err)
	}
	value, err := credentials.Decrypt(credentials.EncryptedValue{
		Scheme: credential.Scheme,
		Sealed: credential.SealedValue,
	}, service.credentialKey)
	if err != nil {
		return "", failure(ErrorValidation, "sender profile credentials could not be decrypted", err)
	}
	return value, nil
}
