package document

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	defaultBatchConversionTimeout = 120 * time.Second
	defaultStatusTimeout          = 10 * time.Second
	pdfExportFilter               = `pdf:writer_pdf_Export:{"UseLosslessCompression":` +
		`{"type":"boolean","value":"true"},"ReduceImageResolution":` +
		`{"type":"boolean","value":"false"}}`
)

var pdfPageObjectPattern = regexp.MustCompile(`/Type\s*/Page(?:\s|/|>>)`)

type DOCXToPDFConverter struct {
	Executable string
	Timeout    time.Duration
	Workspace  string
}

type DOCXInput struct {
	DocumentID int
	WriteTo    func(io.Writer) error
}

type ConvertedPDF struct {
	DocumentID int
	Path       string
	Size       int64
}

type LibreOfficeStatus struct {
	Available bool
	Path      string
	Version   string
	Error     string
}

func FindLibreOfficeExecutable() (string, error) {
	for _, name := range libreOfficeExecutableNames() {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
	}
	for _, candidate := range standardLibreOfficeLocations() {
		if isRegularFile(candidate) {
			return candidate, nil
		}
	}
	return "", errors.New(
		"LibreOffice executable not found on PATH or in a standard installation location; " +
			"install LibreOffice, set integrations.libreoffice.executable in config.json, " +
			"or set BULK_MAIL_LIBREOFFICE_PATH",
	)
}

func (c DOCXToPDFConverter) Status(ctx context.Context) LibreOfficeStatus {
	executable, err := c.ResolveExecutable()
	if err != nil {
		return LibreOfficeStatus{Error: err.Error()}
	}
	status := LibreOfficeStatus{Path: executable}
	checkCtx, cancel := context.WithTimeout(ctx, defaultStatusTimeout)
	defer cancel()
	output := &boundedBuffer{maximum: 32 << 10}
	command := exec.CommandContext(checkCtx, executable, "--version")
	command.Stdout = output
	command.Stderr = output
	err = runOwnedCommand(command)
	if errors.Is(checkCtx.Err(), context.DeadlineExceeded) {
		status.Error = "LibreOffice version check timed out"
		return status
	}
	if checkCtx.Err() != nil {
		status.Error = "LibreOffice version check cancelled"
		return status
	}
	if err != nil {
		status.Error = commandError("LibreOffice version check failed", err, output.Bytes()).Error()
		return status
	}
	status.Version = firstOutputLine(output.Bytes())
	if !bytes.Contains(bytes.ToLower(output.Bytes()), []byte("libreoffice")) {
		status.Error = fmt.Sprintf("unexpected LibreOffice version output: %s", strings.TrimSpace(string(output.Bytes())))
		return status
	}
	status.Available = true
	return status
}

// ConvertBatch stages each input in its private workspace. Converted paths
// are valid only while consume is running.
func (c DOCXToPDFConverter) ConvertBatch(
	ctx context.Context,
	documents []DOCXInput,
	consume func([]ConvertedPDF) error,
) error {
	if consume == nil {
		return errors.New("converted PDF consumer is required")
	}
	if len(documents) == 0 {
		return consume(nil)
	}
	executable, err := c.ResolveExecutable()
	if err != nil {
		return err
	}
	workspaceRoot := strings.TrimSpace(c.Workspace)
	if workspaceRoot == "" {
		return errors.New("LibreOffice workspace directory is required")
	}
	workspace, err := os.MkdirTemp(workspaceRoot, "conversion-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workspace)
	inputDir := filepath.Join(workspace, "input")
	outputDir := filepath.Join(workspace, "output")
	profileDir := filepath.Join(workspace, "profile")
	for _, directory := range []string{inputDir, outputDir, profileDir} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			return err
		}
	}

	inputPaths := make([]string, 0, len(documents))
	seen := make(map[int]bool, len(documents))
	for _, document := range documents {
		if document.DocumentID < 0 || seen[document.DocumentID] {
			return fmt.Errorf("invalid document ID %d", document.DocumentID)
		}
		if document.WriteTo == nil {
			return fmt.Errorf("document %d has no staging writer", document.DocumentID)
		}
		seen[document.DocumentID] = true
		inputPath := filepath.Join(inputDir, numericDocumentName(document.DocumentID, ".docx"))
		input, err := os.OpenFile(inputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("stage DOCX document %d: %w", document.DocumentID, err)
		}
		writeErr := document.WriteTo(input)
		closeErr := input.Close()
		if writeErr != nil {
			return fmt.Errorf("stage DOCX document %d: %w", document.DocumentID, writeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("stage DOCX document %d: %w", document.DocumentID, closeErr)
		}
		inputPaths = append(inputPaths, inputPath)
	}

	convertCtx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	args := []string{
		"--headless",
		"--nologo",
		"--nodefault",
		"--nofirststartwizard",
		"--norestore",
		"-env:UserInstallation=" + fileURL(profileDir),
		"--convert-to",
		pdfExportFilter,
		"--outdir",
		outputDir,
	}
	args = append(args, inputPaths...)
	diagnostics := &boundedBuffer{maximum: 32 << 10}
	command := exec.CommandContext(convertCtx, executable, args...)
	command.WaitDelay = 5 * time.Second
	command.Stdout = diagnostics
	command.Stderr = diagnostics
	err = runOwnedCommand(command)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(convertCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("DOCX to PDF conversion timed out")
	}
	if err != nil {
		return commandError("DOCX to PDF conversion failed", err, diagnostics.Bytes())
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return err
	}
	if len(entries) != len(documents) {
		return fmt.Errorf("LibreOffice produced %d files for %d documents", len(entries), len(documents))
	}
	converted := make([]ConvertedPDF, 0, len(documents))
	for _, document := range documents {
		path := filepath.Join(outputDir, numericDocumentName(document.DocumentID, ".pdf"))
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("document %d PDF output is missing: %w", document.DocumentID, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("document %d PDF output is not a regular file", document.DocumentID)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("secure document %d PDF output: %w", document.DocumentID, err)
		}
		if err := validatePDF(path); err != nil {
			return fmt.Errorf("document %d PDF output is invalid: %w", document.DocumentID, err)
		}
		converted = append(converted, ConvertedPDF{DocumentID: document.DocumentID, Path: path, Size: info.Size()})
	}
	return consume(converted)
}

func numericDocumentName(id int, extension string) string {
	return fmt.Sprintf("document-%03d%s", id, extension)
}

type boundedBuffer struct {
	buffer  bytes.Buffer
	maximum int
}

func (buffer *boundedBuffer) Write(content []byte) (int, error) {
	remaining := buffer.maximum - buffer.buffer.Len()
	if remaining > 0 {
		_, _ = buffer.buffer.Write(content[:min(len(content), remaining)])
	}
	return len(content), nil
}

func (buffer *boundedBuffer) Bytes() []byte { return buffer.buffer.Bytes() }

// ResolveExecutable locates the configured LibreOffice executable without starting it.
func (c DOCXToPDFConverter) ResolveExecutable() (string, error) {
	if configured := strings.TrimSpace(c.Executable); configured != "" {
		path, err := configuredExecutable(configured)
		if err != nil {
			return "", fmt.Errorf("configured LibreOffice executable: %w", err)
		}
		return path, nil
	}
	return FindLibreOfficeExecutable()
}

func (c DOCXToPDFConverter) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return defaultBatchConversionTimeout
}

func fileURL(path string) string {
	absolute, err := filepath.Abs(path)
	if err == nil {
		path = absolute
	}
	slashPath := filepath.ToSlash(path)
	if len(slashPath) >= 2 && slashPath[1] == ':' {
		slashPath = "/" + slashPath
	}
	return (&url.URL{Scheme: "file", Path: slashPath}).String()
}

func configuredExecutable(value string) (string, error) {
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, strings.TrimLeft(value[1:], `/\`))
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	if !isRegularFile(absolute) {
		return "", fmt.Errorf("%s is not a file", absolute)
	}
	return absolute, nil
}

func libreOfficeExecutableNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"soffice.com", "soffice", "libreoffice"}
	}
	return []string{"soffice", "libreoffice"}
}

func standardLibreOfficeLocations() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/Applications/LibreOffice.app/Contents/MacOS/soffice"}
	case "windows":
		locations := make([]string, 0, 2)
		seen := map[string]bool{}
		for _, name := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			root := strings.TrimSpace(os.Getenv(name))
			if root == "" {
				continue
			}
			candidate := filepath.Join(root, "LibreOffice", "program", "soffice.com")
			key := strings.ToLower(candidate)
			if !seen[key] {
				locations = append(locations, candidate)
				seen[key] = true
			}
		}
		return locations
	default:
		return nil
	}
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func firstOutputLine(output []byte) string {
	for _, line := range strings.Split(string(output), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func commandError(message string, err error, output []byte) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %w", message, err)
	}
	return fmt.Errorf("%s: %w: %s", message, err, detail)
}

func validatePDF(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	header := make([]byte, 5)
	if _, err := io.ReadFull(file, header); err != nil {
		return fmt.Errorf("generated PDF is not readable: %w", err)
	}
	if string(header) != "%PDF-" {
		return fmt.Errorf("generated file is not a PDF")
	}
	return nil
}

// PDFPageCount returns the number of explicit /Type/Page objects in a
// LibreOffice-generated PDF without adding a second PDF dependency.
func PDFPageCount(content []byte) int {
	return len(pdfPageObjectPattern.FindAll(content, -1))
}
