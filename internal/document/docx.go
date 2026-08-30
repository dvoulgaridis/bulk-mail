package document

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dvoulgaridis/bulk-mail/internal/validation"
)

const maxDOCXExpandedBytes = 100 << 20
const maxContentTypesBytes = 1 << 20

var placeholderPattern = regexp.MustCompile(`\{\{\s*([\p{L}\p{N}\p{M}_.-]+)\s*\}\}`)

type textNode struct {
	Start int
	End   int
	Text  string
	Raw   []byte
}

type replacement struct {
	Start int
	End   int
	Text  string
}

type preparedDOCX struct {
	content      []byte
	placeholders []string
}

func prepareDOCX(filename string, data []byte) (preparedDOCX, error) {
	if strings.ToLower(filepath.Ext(filename)) != ".docx" {
		return preparedDOCX{}, fmt.Errorf("only .docx files can be attached")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return preparedDOCX{}, fmt.Errorf("invalid DOCX file: %w", err)
	}
	placeholders, err := validateAndDiscoverDOCX(reader.File)
	if err != nil {
		return preparedDOCX{}, err
	}
	return preparedDOCX{content: data, placeholders: placeholders}, nil
}

func validateAndDiscoverDOCX(files []*zip.File) ([]string, error) {
	var hasDocument bool
	var expanded uint64
	partNames := make(map[string]bool, len(files))
	for _, file := range files {
		partName := strings.TrimSuffix(file.Name, "/")
		if partName == "" || !fs.ValidPath(partName) || strings.ContainsRune(partName, '\\') {
			return nil, fmt.Errorf("DOCX file contains an invalid part path")
		}
		name := strings.ToLower(partName)
		if partNames[name] {
			return nil, fmt.Errorf("DOCX file contains duplicate part %s", file.Name)
		}
		partNames[name] = true
		if file.UncompressedSize64 > maxDOCXExpandedBytes || expanded > maxDOCXExpandedBytes-file.UncompressedSize64 {
			return nil, fmt.Errorf("DOCX file expands beyond %d MB", maxDOCXExpandedBytes>>20)
		}
		expanded += file.UncompressedSize64
		if name == "word/document.xml" {
			hasDocument = true
		}
		if strings.HasPrefix(name, "word/vba") || strings.HasSuffix(name, ".bin") || strings.Contains(name, "vbaproject") {
			return nil, fmt.Errorf("macro-enabled DOCX files are not supported")
		}
	}
	if !hasDocument {
		return nil, fmt.Errorf("DOCX file is missing word/document.xml")
	}

	seen := map[string]bool{}
	for _, file := range files {
		name := strings.ToLower(file.Name)
		switch {
		case name == "[content_types].xml":
			data, err := readZipFileMaximum(file, maxContentTypesBytes)
			if err != nil {
				return nil, err
			}
			if contentTypesReferenceMacros(data) {
				return nil, fmt.Errorf("macro-enabled DOCX files are not supported")
			}
		case isWordXML(name):
			data, err := readZipFile(file)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", file.Name, err)
			}
			if err := discoverXMLPlaceholders(data, seen); err != nil {
				return nil, fmt.Errorf("read %s: %w", file.Name, err)
			}
		default:
			if err := validateZipFile(file); err != nil {
				return nil, fmt.Errorf("read %s: %w", file.Name, err)
			}
		}
	}
	placeholders := make([]string, 0, len(seen))
	for key := range seen {
		placeholders = append(placeholders, key)
	}
	sort.Strings(placeholders)
	return placeholders, nil
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func readZipFileMaximum(file *zip.File, maximum int64) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("%s exceeds %d MiB", file.Name, maximum>>20)
	}
	return data, nil
}

func validateZipFile(file *zip.File) error {
	reader, err := file.Open()
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(io.Discard, reader)
	closeErr := reader.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func discoverXMLPlaceholders(data []byte, seen map[string]bool) error {
	nodes, err := xmlTextNodes(data)
	if err != nil {
		return err
	}
	var text strings.Builder
	for _, node := range nodes {
		text.WriteString(node.Text)
	}
	for _, match := range placeholderPattern.FindAllStringSubmatch(text.String(), -1) {
		if len(match) != 2 {
			continue
		}
		key, err := validation.NormalizePlaceholderKey(match[1])
		if err == nil {
			seen[key] = true
		}
	}
	return nil
}

func renderTrustedDOCXTo(output io.Writer, content []byte, values map[string]string) error {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return err
	}
	writer := zip.NewWriter(output)
	if err := writer.SetComment(reader.Comment); err != nil {
		_ = writer.Close()
		return err
	}
	for _, file := range reader.File {
		if !isWordXML(file.Name) {
			if err := writer.Copy(file); err != nil {
				_ = writer.Close()
				return err
			}
			continue
		}
		data, err := readZipFile(file)
		if err != nil {
			_ = writer.Close()
			return err
		}
		data, err = replaceXMLTextPlaceholders(data, values)
		if err != nil {
			_ = writer.Close()
			return err
		}
		header := file.FileHeader
		header.Method = zip.Deflate
		part, err := writer.CreateHeader(&header)
		if err != nil {
			_ = writer.Close()
			return err
		}
		if _, err := part.Write(data); err != nil {
			_ = writer.Close()
			return err
		}
	}
	return writer.Close()
}

func isWordXML(name string) bool {
	name = strings.ToLower(name)
	return name == "word/document.xml" ||
		strings.HasPrefix(name, "word/header") ||
		strings.HasPrefix(name, "word/footer")
}

func replaceXMLTextPlaceholders(data []byte, values map[string]string) ([]byte, error) {
	nodes, err := xmlTextNodes(data)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return data, nil
	}
	normalized := normalizePlaceholderValues(values)
	matches := placeholderMatches(nodes, normalized)
	if len(matches) == 0 {
		return data, nil
	}

	var out bytes.Buffer
	cursor := 0
	for _, match := range matches {
		if match.Start < cursor {
			continue
		}
		out.Write(data[cursor:match.Start])
		out.WriteString(escapeXMLText(match.Text))
		cursor = match.End
	}
	out.Write(data[cursor:])
	return out.Bytes(), nil
}

func xmlTextNodes(data []byte) ([]textNode, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var nodes []textNode
	textDepth := 0
	for {
		start := int(decoder.InputOffset())
		token, err := decoder.Token()
		if err == io.EOF {
			return nodes, nil
		}
		if err != nil {
			return nil, err
		}
		end := int(decoder.InputOffset())
		switch value := token.(type) {
		case xml.StartElement:
			if isWordTextElement(value.Name) {
				textDepth++
			}
		case xml.EndElement:
			if isWordTextElement(value.Name) {
				textDepth--
			}
		case xml.CharData:
			if textDepth > 0 {
				nodes = append(nodes, textNode{Start: start, End: end, Text: string(value), Raw: data[start:end]})
			}
		}
	}
}

func isWordTextElement(name xml.Name) bool {
	if name.Local != "t" {
		return false
	}
	switch name.Space {
	case "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
		"http://purl.oclc.org/ooxml/wordprocessingml/main":
		return true
	default:
		return false
	}
}

func placeholderMatches(nodes []textNode, values map[string]string) []replacement {
	var combined strings.Builder
	offsets := make([]int, 0, len(nodes))
	for _, node := range nodes {
		offsets = append(offsets, combined.Len())
		combined.WriteString(node.Text)
	}

	var replacements []replacement
	for _, match := range placeholderPattern.FindAllStringSubmatchIndex(combined.String(), -1) {
		if len(match) < 4 {
			continue
		}
		key, err := validation.NormalizePlaceholderKey(combined.String()[match[2]:match[3]])
		if err != nil {
			continue
		}
		value, ok := values[key]
		if !ok {
			continue
		}
		start, end, ok := xmlRangeForTextRange(nodes, offsets, match[0], match[1])
		if ok {
			replacements = append(replacements, replacement{Start: start, End: end, Text: value})
		}
	}
	return replacements
}

func xmlRangeForTextRange(nodes []textNode, offsets []int, textStart, textEnd int) (int, int, bool) {
	start := -1
	end := -1
	for i, node := range nodes {
		nodeStart := offsets[i]
		nodeEnd := nodeStart + len(node.Text)
		if textEnd <= nodeStart || textStart >= nodeEnd {
			continue
		}
		localStart := maxInt(0, textStart-nodeStart)
		localEnd := minInt(len(node.Text), textEnd-nodeStart)
		if start == -1 {
			start = node.Start + rawTextOffset(node.Raw, localStart)
		}
		end = node.Start + rawTextOffset(node.Raw, localEnd)
	}
	return start, end, start >= 0 && end >= start
}

func rawTextOffset(raw []byte, decodedOffset int) int {
	if decodedOffset <= 0 {
		return 0
	}
	rawOffset := 0
	decoded := 0
	for rawOffset < len(raw) {
		if decodedOffset == decoded {
			return rawOffset
		}
		if raw[rawOffset] == '&' {
			if relativeEnd := bytes.IndexByte(raw[rawOffset:], ';'); relativeEnd >= 0 {
				entityEnd := rawOffset + relativeEnd + 1
				entity := raw[rawOffset:entityEnd]
				unescaped := html.UnescapeString(string(entity))
				if unescaped != string(entity) {
					decodedEnd := decoded + len(unescaped)
					if decodedOffset <= decodedEnd {
						return entityEnd
					}
					decoded = decodedEnd
					rawOffset = entityEnd
					continue
				}
			}
		}
		decoded++
		rawOffset++
		if decodedOffset == decoded {
			return rawOffset
		}
	}
	return len(raw)
}

func normalizePlaceholderValues(values map[string]string) map[string]string {
	normalized := make(map[string]string, len(values))
	for key, value := range values {
		key, err := validation.NormalizePlaceholderKey(key)
		if err == nil {
			normalized[key] = value
		}
	}
	return normalized
}

func escapeXMLText(value string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(value))
	return out.String()
}

func contentTypesReferenceMacros(data []byte) bool {
	lower := bytes.ToLower(data)
	return bytes.Contains(lower, []byte("vnd.ms-word.document.macroenabled")) ||
		bytes.Contains(lower, []byte("vnd.ms-word.template.macroenabled")) ||
		bytes.Contains(lower, []byte("vbaproject"))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
