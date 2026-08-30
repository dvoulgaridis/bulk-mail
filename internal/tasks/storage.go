package tasks

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const manifestFilename = "manifest.json"

// Stored task input types.

type StoredFile struct {
	Name string
	Data []byte
}

type Storage struct {
	root string
}

// Construction.

func OpenStorage(root string) (*Storage, error) {
	root = strings.TrimSpace(root)
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("task storage root must be an absolute path")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create task storage: %w", err)
	}
	return &Storage{root: root}, nil
}

// Input publication and access.

func (storage *Storage) Stage(manifest []byte, files []StoredFile) (string, error) {
	if storage == nil {
		return "", errors.New("task storage is unavailable")
	}
	key, err := randomStorageKey()
	if err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(storage.root, ".staging-")
	if err != nil {
		return "", fmt.Errorf("create task staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	if err := os.WriteFile(filepath.Join(staging, manifestFilename), manifest, 0o600); err != nil {
		return "", fmt.Errorf("write task manifest: %w", err)
	}
	for _, file := range files {
		if !validStoredFilename(file.Name) || file.Name == manifestFilename {
			return "", fmt.Errorf("invalid stored task filename %q", file.Name)
		}
		if err := os.WriteFile(filepath.Join(staging, file.Name), file.Data, 0o600); err != nil {
			return "", fmt.Errorf("write task attachment: %w", err)
		}
	}
	if err := os.Rename(staging, storage.path(key)); err != nil {
		return "", fmt.Errorf("publish staged task: %w", err)
	}
	return key, nil
}

func (storage *Storage) ReadManifest(key string) ([]byte, error) {
	return storage.read(key, manifestFilename)
}

func (storage *Storage) ReadFile(key, name string) ([]byte, error) {
	if !validStoredFilename(name) || name == manifestFilename {
		return nil, fmt.Errorf("invalid stored task filename %q", name)
	}
	return storage.read(key, name)
}

// Input cleanup.

func (storage *Storage) Remove(key string) error {
	if storage == nil {
		return nil
	}
	if !validStorageKey(key) {
		return fmt.Errorf("invalid task storage key %q", key)
	}
	if err := os.RemoveAll(storage.path(key)); err != nil {
		return fmt.Errorf("remove task storage: %w", err)
	}
	return nil
}

func (storage *Storage) Prune(keep map[string]struct{}) error {
	entries, err := os.ReadDir(storage.root)
	if err != nil {
		return fmt.Errorf("read task storage: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		_, retained := keep[name]
		if retained && validStorageKey(name) {
			continue
		}
		if !entry.IsDir() || (!validStorageKey(name) && !strings.HasPrefix(name, ".staging-")) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(storage.root, name)); err != nil {
			return fmt.Errorf("remove orphaned task storage %s: %w", name, err)
		}
	}
	return nil
}

// Storage internals.

func (storage *Storage) read(key, name string) ([]byte, error) {
	if storage == nil {
		return nil, errors.New("task storage is unavailable")
	}
	if !validStorageKey(key) {
		return nil, fmt.Errorf("invalid task storage key %q", key)
	}
	data, err := os.ReadFile(filepath.Join(storage.path(key), name))
	if err != nil {
		return nil, fmt.Errorf("read stored task input: %w", err)
	}
	return data, nil
}

func (storage *Storage) path(key string) string {
	return filepath.Join(storage.root, key)
}

func randomStorageKey() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("create task storage key: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func validStorageKey(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validStoredFilename(value string) bool {
	return value != "" && filepath.Base(value) == value && value != "." && value != ".."
}
