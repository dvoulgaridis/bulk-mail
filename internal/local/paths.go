package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const defaultDataDirName = "data"

type Paths struct {
	WorkingDirectory  string `json:"-"`
	ProjectRoot       string `json:"projectRoot"`
	DataDir           string `json:"dataDir"`
	DatabasePath      string `json:"databasePath"`
	CredentialKeyPath string `json:"credentialKeyPath"`
	TaskQueueDir      string `json:"-"`
}

func WorkingDirectory() (string, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	workingDirectory, err = filepath.Abs(workingDirectory)
	if err != nil {
		return "", fmt.Errorf("resolve absolute working directory: %w", err)
	}
	return workingDirectory, nil
}

// ProjectRoot locates the Bulk Mail project from the executable or working
// directory. A standalone binary without repository markers uses its own
// directory as the project root.
func ProjectRoot(workingDirectory string) (string, error) {
	executable, executableErr := os.Executable()
	if executableErr == nil {
		executableDir := filepath.Dir(executable)
		if root, ok := findProjectRoot(executableDir); ok {
			return root, nil
		}
	}

	if workingDirectory != "" {
		if root, ok := findProjectRoot(workingDirectory); ok {
			return root, nil
		}
	}

	if executableErr == nil {
		return filepath.Abs(filepath.Dir(executable))
	}
	if workingDirectory != "" {
		return filepath.Abs(workingDirectory)
	}
	return "", errors.New("could not locate Bulk Mail project root")
}

func ResolvePaths(projectRoot, workingDirectory, override string) (Paths, error) {
	return resolvePaths(projectRoot, workingDirectory, override)
}

func resolvePaths(projectRoot, workingDirectory, override string) (Paths, error) {
	if projectRoot == "" {
		return Paths{}, errors.New("empty project root")
	}
	if workingDirectory == "" {
		return Paths{}, errors.New("empty working directory")
	}

	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return Paths{}, err
	}
	workingDirectory, err = filepath.Abs(workingDirectory)
	if err != nil {
		return Paths{}, err
	}

	dataDir := override
	if dataDir == "" {
		dataDir = os.Getenv("BULK_MAIL_DATA_DIR")
	}
	if dataDir == "" {
		dataDir = filepath.Join(workingDirectory, defaultDataDirName)
	} else if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(root, dataDir)
	}
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		return Paths{}, err
	}

	return Paths{
		WorkingDirectory:  workingDirectory,
		ProjectRoot:       root,
		DataDir:           dataDir,
		DatabasePath:      filepath.Join(dataDir, "bulk-mail.sqlite"),
		CredentialKeyPath: filepath.Join(dataDir, "bulk-mail.key"),
		TaskQueueDir:      filepath.Join(dataDir, "task-queue"),
	}, nil
}

func EnsureDataDir(paths Paths) error {
	for _, path := range []string{paths.DataDir, paths.TaskQueueDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func findProjectRoot(start string) (string, bool) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}

	for {
		if isFile(filepath.Join(current, "go.mod")) && isDir(filepath.Join(current, "cmd", "bulk-mail")) {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
