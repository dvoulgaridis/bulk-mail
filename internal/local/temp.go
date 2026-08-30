package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type TemporaryPaths struct {
	Root        string
	Conversions string
	Preflight   string
	Archives    string
}

type TemporarySpace struct {
	Paths TemporaryPaths
	once  sync.Once
	err   error
}

func (paths TemporaryPaths) Validate() error {
	if strings.TrimSpace(paths.Root) == "" || !filepath.IsAbs(paths.Root) {
		return errors.New("temporary root must be an absolute path")
	}
	for _, directory := range []struct{ name, path string }{
		{"conversion", paths.Conversions},
		{"preflight", paths.Preflight},
		{"archive", paths.Archives},
	} {
		name, path := directory.name, directory.path
		if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
			return fmt.Errorf("%s temporary directory must be an absolute path", name)
		}
		relative, err := filepath.Rel(paths.Root, path)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s temporary directory must be beneath the application temporary root", name)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("access %s temporary directory: %w", name, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s temporary path is not a directory", name)
		}
	}
	return nil
}

func OpenTemporarySpace(workingDirectory string) (*TemporarySpace, error) {
	if strings.TrimSpace(workingDirectory) == "" {
		return nil, errors.New("launch working directory is required")
	}
	workingDirectory, err := filepath.Abs(workingDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute launch working directory: %w", err)
	}
	sharedRoot := filepath.Join(workingDirectory, "tmp")
	if err := os.MkdirAll(sharedRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create temporary root %s: %w", sharedRoot, err)
	}
	instanceRoot, err := os.MkdirTemp(sharedRoot, "bulk-mail-")
	if err != nil {
		return nil, fmt.Errorf("create application temporary directory under %s: %w", sharedRoot, err)
	}
	paths := TemporaryPaths{
		Root:        instanceRoot,
		Conversions: filepath.Join(instanceRoot, "conversions"),
		Preflight:   filepath.Join(instanceRoot, "preflight"),
		Archives:    filepath.Join(instanceRoot, "archives"),
	}
	for _, path := range []string{paths.Conversions, paths.Preflight, paths.Archives} {
		if err := os.Mkdir(path, 0o700); err != nil {
			_ = os.RemoveAll(instanceRoot)
			return nil, fmt.Errorf("create application temporary directory %s: %w", path, err)
		}
	}
	return &TemporarySpace{Paths: paths}, nil
}

func (space *TemporarySpace) Close() error {
	if space == nil {
		return nil
	}
	space.once.Do(func() { space.err = os.RemoveAll(space.Paths.Root) })
	return space.err
}
