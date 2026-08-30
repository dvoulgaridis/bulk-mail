package document

import (
	"bytes"
	"fmt"
	"io"
)

// CampaignTemplate contains DOCX content that passed complete package validation.
// Its private content keeps unvalidated bytes outside the rendering boundary.
type CampaignTemplate struct {
	Filename       string
	OutputFilename string
	content        []byte
	placeholders   []string
}

func NewCampaignTemplate(filename, outputFilename string, content []byte) (CampaignTemplate, error) {
	prepared, err := prepareDOCX(filename, content)
	if err != nil {
		return CampaignTemplate{}, err
	}
	return CampaignTemplate{
		Filename: filename, OutputFilename: outputFilename,
		content: bytes.Clone(prepared.content), placeholders: prepared.placeholders,
	}, nil
}

func (template CampaignTemplate) Placeholders() []string {
	return append([]string(nil), template.placeholders...)
}

func StaticDOCX(inputs []CampaignTemplate) []DOCXInput {
	documents := make([]DOCXInput, 0, len(inputs))
	for index, input := range inputs {
		if len(input.placeholders) == 0 {
			template := input
			documents = append(documents, DOCXInput{DocumentID: index, WriteTo: func(writer io.Writer) error {
				_, err := io.Copy(writer, bytes.NewReader(template.content))
				return err
			}})
		}
	}
	return documents
}

func PersonalizedDOCX(inputs []CampaignTemplate, values map[string]string) []DOCXInput {
	documents := make([]DOCXInput, 0, len(inputs))
	for index, input := range inputs {
		if len(input.placeholders) == 0 {
			continue
		}
		template := input
		documents = append(documents, DOCXInput{DocumentID: index, WriteTo: func(writer io.Writer) error {
			if err := renderTrustedDOCXTo(writer, template.content, values); err != nil {
				return fmt.Errorf("%s render failed: %w", template.Filename, err)
			}
			return nil
		}})
	}
	return documents
}
