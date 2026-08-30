package document

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/dvoulgaridis/bulk-mail/internal/templates"
)

const maxGeneratedAttachmentBytesPerArchive int64 = 1 << 30

var archiveLimitError = fmt.Sprintf(
	"generated attachment output exceeds the %d GiB archive limit",
	maxGeneratedAttachmentBytesPerArchive>>30,
)

type CampaignAddressEntry struct {
	Email       string
	DisplayName string
	Values      map[string]string
}

type GeneratedPDF struct {
	DocumentID int
	Content    []byte
}

type StaticAttachment struct {
	Filename string
	Content  []byte
}

type GenerationResult struct {
	Email  string
	Status string
	Error  string
	Files  []string
}

func ReadConvertedPDFs(converted []ConvertedPDF) ([]GeneratedPDF, error) {
	documents := make([]GeneratedPDF, 0, len(converted))
	for _, item := range converted {
		content, err := os.ReadFile(item.Path)
		if err != nil {
			return nil, err
		}
		documents = append(documents, GeneratedPDF{DocumentID: item.DocumentID, Content: content})
	}
	return documents, nil
}

func ResolveOutputFilenames(inputs []CampaignTemplate, values map[string]string) []string {
	used := map[string]bool{}
	names := make([]string, 0, len(inputs))
	for _, input := range inputs {
		template := strings.TrimSpace(input.OutputFilename)
		if template == "" {
			template = pdfFilename(input.Filename)
		}
		name := ensurePDFExtension(SanitizeFilename(templates.RenderText(template, values)))
		if name == ".pdf" {
			name = "document.pdf"
		}
		names = append(names, uniqueFilename(name, used))
	}
	return names
}

func GenerateCampaignArchive(
	ctx context.Context,
	converter DOCXToPDFConverter,
	archivePath string,
	addressEntries []CampaignAddressEntry,
	inputs []CampaignTemplate,
	staticAttachments []StaticAttachment,
	staticPDFs []GeneratedPDF,
	onResult func(GenerationResult) error,
) (func(), error) {
	cleanup := func() { _ = os.Remove(archivePath) }
	fail := func(err error) (func(), error) {
		cleanup()
		return nil, err
	}
	archive, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fail(err)
	}
	zipWriter := zip.NewWriter(archive)
	manifest := &bytes.Buffer{}
	manifestWriter := csv.NewWriter(manifest)
	if err := manifestWriter.Write([]string{
		"entry_number",
		"email",
		"display_name",
		"status",
		"error",
		"directory",
		"files",
	}); err != nil {
		_ = zipWriter.Close()
		_ = archive.Close()
		return fail(err)
	}
	var generatedAttachmentBytes int64
	archiveLimitReached := false
	staticByID := make(map[int]GeneratedPDF, len(staticPDFs))
	for _, item := range staticPDFs {
		staticByID[item.DocumentID] = item
	}
	staticBytes := generatedPDFSize(staticPDFs) + staticAttachmentSize(staticAttachments)
	for entryIndex, entry := range addressEntries {
		if err := ctx.Err(); err != nil {
			_ = zipWriter.Close()
			_ = archive.Close()
			return fail(err)
		}
		directory := fmt.Sprintf("address-entry-%06d", entryIndex+1)
		result := GenerationResult{
			Email:  entry.Email,
			Status: "generated",
		}
		if archiveLimitReached {
			result.Status = "failed_processing"
			result.Error = archiveLimitError
		} else {
			var writeErr error
			renderErr := converter.ConvertBatch(
				ctx,
				PersonalizedDOCX(inputs, entry.Values),
				func(converted []ConvertedPDF) error {
					outputNames := ResolveOutputFilenames(inputs, entry.Values)
					convertedByID := make(map[int]ConvertedPDF, len(converted))
					for _, item := range converted {
						convertedByID[item.DocumentID] = item
					}
					entryBytes := staticBytes
					for _, item := range converted {
						entryBytes += item.Size
					}
					if generatedAttachmentBytes+entryBytes > maxGeneratedAttachmentBytesPerArchive {
						archiveLimitReached = true
						return nil
					}
					usedNames := map[string]bool{}
					for documentID := range inputs {
						filename := uniqueFilename(outputNames[documentID], usedNames)
						entryName := path.Join(directory, SanitizeFilename(filename))
						if item, ok := staticByID[documentID]; ok {
							writeErr = writeZipFile(zipWriter, entryName, item.Content)
						} else if item, ok := convertedByID[documentID]; ok {
							writeErr = writeZipPath(zipWriter, entryName, item.Path)
						} else {
							writeErr = fmt.Errorf("document %d has no converted PDF", documentID)
						}
						if writeErr != nil {
							return writeErr
						}
						result.Files = append(result.Files, filename)
					}
					for _, attachment := range staticAttachments {
						filename := uniqueFilename(SanitizeFilename(attachment.Filename), usedNames)
						entryName := path.Join(directory, filename)
						if writeErr = writeZipFile(zipWriter, entryName, attachment.Content); writeErr != nil {
							return writeErr
						}
						result.Files = append(result.Files, filename)
					}
					generatedAttachmentBytes += entryBytes
					return nil
				},
			)
			if writeErr != nil {
				_ = zipWriter.Close()
				_ = archive.Close()
				return fail(writeErr)
			}
			if renderErr != nil && ctx.Err() != nil {
				_ = zipWriter.Close()
				_ = archive.Close()
				return fail(ctx.Err())
			}
			if renderErr != nil {
				result.Status = "failed_processing"
				result.Error = compactError(renderErr.Error())
			} else if archiveLimitReached {
				result.Status = "failed_processing"
				result.Error = archiveLimitError
			}
		}
		if onResult != nil {
			if err := onResult(result); err != nil {
				_ = zipWriter.Close()
				_ = archive.Close()
				return fail(err)
			}
		}
		manifestDirectory := directory
		if result.Status != "generated" {
			manifestDirectory = ""
		}
		if err := manifestWriter.Write([]string{
			strconv.Itoa(entryIndex + 1), entry.Email, entry.DisplayName, result.Status,
			result.Error, manifestDirectory, strings.Join(result.Files, "; "),
		}); err != nil {
			_ = zipWriter.Close()
			_ = archive.Close()
			return fail(err)
		}
	}
	manifestWriter.Flush()
	if err := manifestWriter.Error(); err != nil {
		_ = zipWriter.Close()
		_ = archive.Close()
		return fail(err)
	}
	if err := writeZipFile(zipWriter, "manifest.csv", manifest.Bytes()); err != nil {
		_ = zipWriter.Close()
		_ = archive.Close()
		return fail(err)
	}
	if err := zipWriter.Close(); err != nil {
		_ = archive.Close()
		return fail(err)
	}
	if err := archive.Close(); err != nil {
		return fail(err)
	}
	return cleanup, nil
}

func generatedPDFSize(documents []GeneratedPDF) int64 {
	var total int64
	for _, document := range documents {
		total += int64(len(document.Content))
	}
	return total
}

func staticAttachmentSize(attachments []StaticAttachment) int64 {
	var total int64
	for _, attachment := range attachments {
		total += int64(len(attachment.Content))
	}
	return total
}

func uniqueFilename(name string, used map[string]bool) string {
	key := strings.ToLower(name)
	if !used[key] {
		used[key] = true
		return name
	}
	extension := filepath.Ext(name)
	base := strings.TrimSuffix(name, extension)
	for sequence := 2; ; sequence++ {
		candidate := truncateFilename(fmt.Sprintf("%s-%d%s", base, sequence, extension), 200)
		key = strings.ToLower(candidate)
		if !used[key] {
			used[key] = true
			return candidate
		}
	}
}

func writeZipFile(writer *zip.Writer, name string, content []byte) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0o600)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = entry.Write(content)
	return err
}

func writeZipPath(writer *zip.Writer, name, sourcePath string) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0o600)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	_, err = io.Copy(entry, source)
	return err
}

func SanitizeFilename(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	for _, character := range value {
		if character < 32 || strings.ContainsRune(`<>:"/\\|?*`, character) || unicode.IsControl(character) {
			builder.WriteRune('_')
		} else {
			builder.WriteRune(character)
		}
	}
	name := strings.TrimRight(strings.TrimSpace(builder.String()), ". ")
	if name == "" || name == "." || name == ".." {
		return "document"
	}
	extension := filepath.Ext(name)
	base := strings.TrimSuffix(name, extension)
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		name = "_" + name
	}
	return truncateFilename(name, 200)
}

func truncateFilename(name string, maximumBytes int) string {
	if len(name) <= maximumBytes {
		return name
	}
	extension := filepath.Ext(name)
	base := []rune(strings.TrimSuffix(name, extension))
	for len(base) > 0 && len(string(base)+extension) > maximumBytes {
		base = base[:len(base)-1]
	}
	if len(base) == 0 {
		return "document" + extension
	}
	return string(base) + extension
}

func ensurePDFExtension(value string) string {
	extension := filepath.Ext(value)
	base := strings.TrimSpace(strings.TrimSuffix(value, extension))
	if base == "" {
		base = "document"
	}
	return base + ".pdf"
}

func pdfFilename(value string) string {
	return ensurePDFExtension(SanitizeFilename(value))
}

func compactError(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	characters := []rune(value)
	if len(characters) > 500 {
		value = string(characters[:500])
	}
	return value
}
