package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	schemeAES256GCMV1 = "aes-256-gcm-v1"
	keySize           = 32
	keyReadAttempts   = 5
	keyReadRetryDelay = 100 * time.Millisecond
)

type EncryptedValue struct {
	Scheme string
	Sealed []byte
}

func LoadOrCreateKey(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("credential key path is required")
	}
	key, err := readKey(path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return readKeyWithRetry(path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	key = make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	data := []byte(base64.StdEncoding.EncodeToString(key) + "\n")
	err = createKeyFile(path, data)
	if errors.Is(err, os.ErrExist) {
		return readKeyWithRetry(path)
	}
	if err != nil {
		return nil, err
	}
	return key, nil
}

func readKey(path string) ([]byte, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeKey(encoded)
}

func createKeyFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

func readKeyWithRetry(path string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < keyReadAttempts; attempt++ {
		key, err := readKey(path)
		if err == nil {
			return key, nil
		}
		lastErr = err
		if attempt < keyReadAttempts-1 {
			time.Sleep(keyReadRetryDelay)
		}
	}
	return nil, fmt.Errorf("load credential key: %w", lastErr)
}

func decodeKey(encoded []byte) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		return nil, err
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("credential key must be %d bytes", keySize)
	}
	return key, nil
}

func Encrypt(plaintext string, key []byte) (EncryptedValue, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return EncryptedValue{}, err
	}
	return EncryptedValue{
		Scheme: schemeAES256GCMV1,
		Sealed: aead.Seal(nil, nil, []byte(plaintext), []byte(schemeAES256GCMV1)),
	}, nil
}

func Decrypt(value EncryptedValue, key []byte) (string, error) {
	if value.Scheme != schemeAES256GCMV1 {
		return "", fmt.Errorf("unsupported credential encryption scheme %q", value.Scheme)
	}
	aead, err := newAEAD(key)
	if err != nil {
		return "", err
	}
	plain, err := aead.Open(nil, nil, value.Sealed, []byte(value.Scheme))
	if err != nil {
		return "", errors.New("credential could not be decrypted")
	}
	return string(plain), nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("credential key must be %d bytes", keySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}
