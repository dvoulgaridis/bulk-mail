package document

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"slices"
	"strings"
)

var xmlAttributePattern = regexp.MustCompile(`([^\s=<>/'"]+)\s*=\s*("[^"]*"|'[^']*')`)
var decimalPercentPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)%$`)

func isDrawingMLNamespace(space string) bool {
	return space == "http://schemas.openxmlformats.org/drawingml/2006/main" ||
		space == "http://purl.oclc.org/ooxml/drawingml/main"
}

// PrepareForConversion normalizes this validated template before per-entry rendering.
// Only the private conversion copy changes; saved attachment bytes remain untouched.
func (template *CampaignTemplate) PrepareForConversion() error {
	if !template.ConvertToPDF {
		return nil
	}
	reader, err := zip.NewReader(bytes.NewReader(template.content), int64(len(template.content)))
	if err != nil {
		return err
	}
	changed := make(map[string][]byte)
	var expanded int64
	for _, file := range reader.File {
		expanded += int64(file.UncompressedSize64)
		if !strings.HasPrefix(file.Name, "word/") || !strings.HasSuffix(file.Name, ".xml") {
			continue
		}
		data, err := readZipFile(file)
		if err != nil {
			return err
		}
		normalizedData, err := normalizeXML(data)
		if err != nil {
			return fmt.Errorf("%s: %w", file.Name, err)
		}
		if !bytes.Equal(data, normalizedData) {
			changed[file.Name] = normalizedData
			expanded += int64(len(normalizedData) - len(data))
		}
	}
	if expanded > maxDOCXExpandedBytes {
		return fmt.Errorf("normalized DOCX exceeds expanded size limit")
	}
	if len(changed) == 0 {
		return nil
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	defer writer.Close()
	if err := writer.SetComment(reader.Comment); err != nil {
		return err
	}
	for _, file := range reader.File {
		data, ok := changed[file.Name]
		if !ok {
			if err := writer.Copy(file); err != nil {
				return err
			}
			continue
		}
		header := file.FileHeader
		part, err := writer.CreateHeader(&header)
		if err != nil {
			return err
		}
		if _, err := part.Write(data); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	template.content = output.Bytes()
	return nil
}

// Patch attribute values without re-encoding XML or changing namespace prefixes.
func normalizeXML(data []byte) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var output bytes.Buffer
	cursor := 0
	for {
		start := int(decoder.InputOffset())
		token, err := decoder.Token()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		element, ok := token.(xml.StartElement)
		if !ok || !isDrawingMLNamespace(element.Name.Space) {
			continue
		}
		var values map[string]string
		switch element.Name.Local {
		case "lum":
			values = normalizePercentAttributes(element.Attr, -100000, 100000, "bright", "contrast")
		case "biLevel":
			values = normalizePercentAttributes(element.Attr, 0, 100000, "thresh")
		case "alphaModFix":
			values = normalizePercentAttributes(element.Attr, 0, 1<<31-1, "amt")
		case "tile", "outerShdw":
			values = normalizePercentAttributes(element.Attr, -1<<31, 1<<31-1, "sx", "sy")
		case "scrgbClr":
			values = normalizePercentAttributes(element.Attr, -1<<31, 1<<31-1, "r", "g", "b")
		case "hslClr":
			values = normalizePercentAttributes(element.Attr, -1<<31, 1<<31-1, "sat", "lum")
		case "spcPct":
			values = normalizePercentAttributes(element.Attr, 0, 13200000, "val")
		case "buSzPct":
			values = normalizePercentAttributes(element.Attr, 25000, 400000, "val")
		}
		if len(values) == 0 {
			continue
		}
		for _, match := range xmlAttributePattern.FindAllSubmatchIndex(data[start:decoder.InputOffset()], -1) {
			if value, ok := values[string(data[start+match[2]:start+match[3]])]; ok {
				valueStart, valueEnd := start+match[4]+1, start+match[5]-1 // skip quotes
				output.Write(data[cursor:valueStart])
				output.WriteString(value)
				cursor = valueEnd
			}
		}
	}
	if cursor == 0 {
		return data, nil
	}
	output.Write(data[cursor:])
	return output.Bytes(), nil
}

// Bounds are in thousandths of a percent, matching the target OOXML integer types.
// Unsupported values remain untouched; other attributes can still be normalized.
func normalizePercentAttributes(attrs []xml.Attr, minimum, maximum int64, names ...string) map[string]string {
	values := map[string]string{}
	for _, attr := range attrs {
		if attr.Name.Space != "" || !slices.Contains(names, attr.Name.Local) {
			continue
		}
		value := strings.TrimSpace(attr.Value)
		if !strings.HasSuffix(value, "%") {
			continue
		}
		if normalized := percentageUnits(value, minimum, maximum); normalized != "" {
			values[attr.Name.Local] = normalized
		}
	}
	return values
}

// Round to 0.001%, ties away from zero; reject out-of-range input rather than clamp.
// An empty result means the original attribute should remain unchanged.
func percentageUnits(value string, minimum, maximum int64) string {
	if !decimalPercentPattern.MatchString(value) {
		return ""
	}
	number, ok := new(big.Rat).SetString(strings.TrimSuffix(value, "%"))
	if !ok || number.Cmp(big.NewRat(minimum, 1000)) < 0 || number.Cmp(big.NewRat(maximum, 1000)) > 0 {
		return ""
	}
	sign := number.Sign()
	number.Abs(number).Mul(number, big.NewRat(1000, 1)).Add(number, big.NewRat(1, 2))
	integer := new(big.Int).Quo(number.Num(), number.Denom())
	if sign < 0 {
		integer.Neg(integer)
	}
	return integer.String()
}
