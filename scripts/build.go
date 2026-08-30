package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	bundleRootName              = "bulk-mail"
	distDirectoryEnvironment    = "BULK_MAIL_DIST_DIR"
	thirdPartyNoticesFilename   = "THIRD_PARTY_NOTICES.md"
	checksumsFilename           = "checksums.txt"
	releaseStagingDirectoryName = "bulk-mail-release-*"
)

var (
	archiveTimestamp = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
	versionPattern   = regexp.MustCompile(
		`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
			`(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?` +
			`(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`,
	)
)

type archiveFormat string

const (
	archiveTarGzip archiveFormat = "tar.gz"
	archiveZIP     archiveFormat = "zip"
)

type buildTarget struct {
	goos   string
	goarch string
	format archiveFormat
}

type bundleFile struct {
	path string
	name string
	mode fs.FileMode
}

var buildTargets = []buildTarget{
	{goos: "linux", goarch: "amd64", format: archiveTarGzip},
	{goos: "darwin", goarch: "amd64", format: archiveTarGzip},
	{goos: "darwin", goarch: "arm64", format: archiveTarGzip},
	{goos: "windows", goarch: "amd64", format: archiveZIP},
}

func main() {
	version, err := releaseVersion(os.Args[1:])
	if err == nil {
		err = run(version)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "build release:", err)
		os.Exit(1)
	}
}

func releaseVersion(arguments []string) (string, error) {
	flags := flag.NewFlagSet("bulk-mail-release", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	version := flags.String("version", "", "release version, such as v0.1.0")
	if err := flags.Parse(arguments); err != nil {
		return "", err
	}
	if flags.NArg() != 0 {
		return "", fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	value := strings.TrimSpace(*version)
	if !versionPattern.MatchString(value) {
		return "", errors.New("--version must be a semantic version such as v0.1.0")
	}
	return value, nil
}

func run(version string) error {
	root, err := projectRoot()
	if err != nil {
		return err
	}
	if err := validateBundleSources(root); err != nil {
		return err
	}
	distDir, err := releaseDirectory(root)
	if err != nil {
		return err
	}

	fmt.Println("Building frontend...")
	if err := runCommand(root, nil, "pnpm", "run", "build:ui"); err != nil {
		return fmt.Errorf("build frontend: %w", err)
	}
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return fmt.Errorf("create release directory: %w", err)
	}
	stagingDir, err := os.MkdirTemp("", releaseStagingDirectoryName)
	if err != nil {
		return fmt.Errorf("create release staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	artifactNames := make([]string, 0, len(buildTargets))
	for _, target := range buildTargets {
		fmt.Printf("Building %s/%s...\n", target.goos, target.goarch)
		files, err := buildBundle(root, stagingDir, target)
		if err != nil {
			return fmt.Errorf("build %s/%s: %w", target.goos, target.goarch, err)
		}
		artifactName := target.archiveName(version)
		artifactPath := filepath.Join(stagingDir, artifactName)
		if err := createArchive(artifactPath, target.format, files); err != nil {
			return fmt.Errorf("archive %s/%s: %w", target.goos, target.goarch, err)
		}
		artifactNames = append(artifactNames, artifactName)
	}
	if err := writeChecksums(stagingDir, artifactNames); err != nil {
		return err
	}
	if err := publishArtifacts(stagingDir, distDir, artifactNames); err != nil {
		return err
	}

	fmt.Printf("Release artifacts written to %s\n", distDir)
	return nil
}

func validateBundleSources(root string) error {
	for _, name := range []string{"config.json", "LICENSE", thirdPartyNoticesFilename} {
		if !regularFile(filepath.Join(root, name)) {
			return fmt.Errorf("required bundle file %q is missing", name)
		}
	}
	return nil
}

func buildBundle(
	root string,
	stagingDir string,
	target buildTarget,
) ([]bundleFile, error) {
	targetDir := filepath.Join(stagingDir, target.goos+"-"+target.goarch)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("create bundle directory: %w", err)
	}

	executableName := target.executableName()
	executablePath := filepath.Join(targetDir, executableName)
	environment := map[string]string{
		"CGO_ENABLED": "0",
		"GOARCH":      target.goarch,
		"GOOS":        target.goos,
	}
	if err := runCommand(
		root,
		environment,
		"go",
		"build",
		"-trimpath",
		"-o",
		executablePath,
		"./cmd/bulk-mail",
	); err != nil {
		return nil, err
	}

	files := []bundleFile{
		{path: executablePath, name: executableName, mode: 0o755},
		{path: filepath.Join(root, "config.json"), name: "config.json", mode: 0o644},
		{path: filepath.Join(root, "LICENSE"), name: "LICENSE", mode: 0o644},
		{
			path: filepath.Join(root, thirdPartyNoticesFilename),
			name: thirdPartyNoticesFilename,
			mode: 0o644,
		},
	}
	return files, nil
}

func createArchive(destination string, format archiveFormat, files []bundleFile) error {
	switch format {
	case archiveTarGzip:
		return createTarGzip(destination, files)
	case archiveZIP:
		return createZIP(destination, files)
	default:
		return fmt.Errorf("unsupported archive format %q", format)
	}
}

func createTarGzip(destination string, files []bundleFile) error {
	archive, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer archive.Close()
	gzipWriter := gzip.NewWriter(archive)
	gzipWriter.Header.ModTime = archiveTimestamp
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range files {
		if err := writeTarFile(tarWriter, file); err != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		_ = gzipWriter.Close()
		return err
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	return archive.Close()
}

func writeTarFile(writer *tar.Writer, file bundleFile) error {
	info, err := os.Stat(file.path)
	if err != nil {
		return err
	}
	header := &tar.Header{
		Name:     path.Join(bundleRootName, file.name),
		Mode:     int64(file.mode.Perm()),
		Size:     info.Size(),
		ModTime:  archiveTimestamp,
		Typeflag: tar.TypeReg,
	}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	return copyIntoArchive(writer, file.path)
}

func createZIP(destination string, files []bundleFile) error {
	archive, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer archive.Close()
	zipWriter := zip.NewWriter(archive)
	for _, file := range files {
		if err := writeZIPFile(zipWriter, file); err != nil {
			_ = zipWriter.Close()
			return err
		}
	}
	if err := zipWriter.Close(); err != nil {
		return err
	}
	return archive.Close()
}

func writeZIPFile(writer *zip.Writer, file bundleFile) error {
	header := &zip.FileHeader{
		Name:   path.Join(bundleRootName, file.name),
		Method: zip.Deflate,
	}
	header.SetMode(file.mode)
	header.SetModTime(archiveTimestamp)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	return copyIntoArchive(entry, file.path)
}

func copyIntoArchive(writer io.Writer, source string) error {
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(writer, file)
	return err
}

func publishArtifacts(stagingDir, distDir string, artifactNames []string) error {
	filenames := append(append([]string{}, artifactNames...), checksumsFilename)
	for _, name := range filenames {
		if err := copyFile(
			filepath.Join(stagingDir, name),
			filepath.Join(distDir, name),
			0o644,
		); err != nil {
			return fmt.Errorf("publish %s: %w", name, err)
		}
	}
	return nil
}

func writeChecksums(directory string, artifactNames []string) error {
	var output strings.Builder
	for _, name := range artifactNames {
		checksum, err := fileChecksum(filepath.Join(directory, name))
		if err != nil {
			return fmt.Errorf("checksum %s: %w", name, err)
		}
		fmt.Fprintf(&output, "%x  %s\n", checksum, name)
	}
	checksumPath := filepath.Join(directory, checksumsFilename)
	if err := os.WriteFile(checksumPath, []byte(output.String()), 0o644); err != nil {
		return fmt.Errorf("write checksums: %w", err)
	}
	return nil
}

func (target buildTarget) executableName() string {
	if target.goos == "windows" {
		return bundleRootName + ".exe"
	}
	return bundleRootName
}

func (target buildTarget) archiveName(version string) string {
	return fmt.Sprintf(
		"%s-%s-%s-%s.%s",
		bundleRootName,
		version,
		target.goos,
		target.goarch,
		target.format,
	)
}

func projectRoot() (string, error) {
	candidates := make([]string, 0, 3)
	if workingDir, err := os.Getwd(); err == nil {
		candidates = append(candidates, workingDir)
	}
	if _, filename, _, ok := runtime.Caller(0); ok {
		candidates = append(candidates, filepath.Dir(filename))
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(executable))
	}

	for _, candidate := range candidates {
		if root, ok := findProjectRoot(candidate); ok {
			return root, nil
		}
	}
	return "", errors.New("could not locate the Bulk Mail project root")
}

func findProjectRoot(start string) (string, bool) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if regularFile(filepath.Join(current, "go.mod")) &&
			regularFile(filepath.Join(current, "package.json")) &&
			regularFile(filepath.Join(current, "cmd", "bulk-mail", "main.go")) {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

func releaseDirectory(root string) (string, error) {
	distDir := strings.TrimSpace(os.Getenv(distDirectoryEnvironment))
	if distDir == "" {
		distDir = "dist"
	}
	if !filepath.IsAbs(distDir) {
		distDir = filepath.Join(root, distDir)
	}
	return filepath.Abs(distDir)
}

func runCommand(directory string, overrides map[string]string, name string, args ...string) error {
	command := executableCommand(name, args...)
	command.Dir = directory
	command.Env = environment(overrides)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func executableCommand(name string, args ...string) *exec.Cmd {
	if runtime.GOOS == "windows" && name == "pnpm" {
		commandArguments := append([]string{"/d", "/s", "/c", name}, args...)
		return exec.Command("cmd.exe", commandArguments...)
	}
	return exec.Command(name, args...)
}

func environment(overrides map[string]string) []string {
	if len(overrides) == 0 {
		return os.Environ()
	}
	result := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !environmentKeyOverridden(key, overrides) {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

func environmentKeyOverridden(key string, overrides map[string]string) bool {
	if _, ok := overrides[key]; ok {
		return true
	}
	if runtime.GOOS == "windows" {
		for override := range overrides {
			if strings.EqualFold(key, override) {
				return true
			}
		}
	}
	return false
}

func copyFile(source, destination string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(destination, mode)
}

func fileChecksum(filename string) ([sha256.Size]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return [sha256.Size]byte{}, err
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result, nil
}

func regularFile(filename string) bool {
	info, err := os.Stat(filename)
	return err == nil && info.Mode().IsRegular()
}
