package server

import (
	"context"
	"strings"

	"github.com/dvoulgaridis/bulk-mail/internal/credentials"
	"github.com/dvoulgaridis/bulk-mail/internal/store"
)

func (s *Server) encryptCredential(credentialType, value string) (store.ProfileCredential, error) {
	encrypted, err := credentials.Encrypt(value, s.credentialKey)
	if err != nil {
		return store.ProfileCredential{}, err
	}
	return store.ProfileCredential{
		CredentialType: strings.TrimSpace(credentialType),
		Scheme:         encrypted.Scheme,
		SealedValue:    encrypted.Sealed,
	}, nil
}

func (s *Server) decryptCredential(ctx context.Context, profileID int64, credentialType string) (string, error) {
	credential, err := s.repo.GetProfileCredential(ctx, profileID, credentialType)
	if err != nil {
		return "", err
	}
	return credentials.Decrypt(credentials.EncryptedValue{
		Scheme: credential.Scheme,
		Sealed: credential.SealedValue,
	}, s.credentialKey)
}
