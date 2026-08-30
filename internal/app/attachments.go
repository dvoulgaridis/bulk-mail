package app

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/dvoulgaridis/bulk-mail/internal/document"
	"github.com/dvoulgaridis/bulk-mail/internal/mail"
)

const (
	MaxCampaignAttachmentBytes = 10 << 20 // 10 MiB
)

func validateAttachments(
	inputs []mail.Attachment,
	maximum int,
) ([]mail.Attachment, []document.CampaignTemplate, error) {
	if err := validateAttachmentCount(len(inputs), maximum); err != nil {
		return nil, nil, err
	}
	attachments := make([]mail.Attachment, 0, len(inputs))
	documents := make([]document.CampaignTemplate, 0, len(inputs))
	for _, input := range inputs {
		filename, err := validateAttachmentSource(input)
		if err != nil {
			return nil, nil, err
		}
		if !isDOCXFilename(filename) {
			attachments = append(attachments, mail.Attachment{
				Filename:    filename,
				ContentType: attachmentContentType(filename, input.Content),
				Size:        len(input.Content),
				Content:     bytes.Clone(input.Content),
			})
			continue
		}
		output := strings.TrimSpace(input.OutputFilename)
		if output == "" {
			output = strings.TrimSuffix(filename, filepath.Ext(filename)) + ".pdf"
		}
		template, err := document.NewCampaignTemplate(filename, output, input.Content)
		if err != nil {
			return nil, nil, failure(ErrorValidation, fmt.Sprintf("%s: %v", filename, err), err)
		}
		documents = append(documents, template)
		attachments = append(attachments, mail.Attachment{
			Filename:       filename,
			OutputFilename: output,
			Size:           len(input.Content),
		})
	}
	return attachments, documents, nil
}

func validateAttachmentCount(count, maximum int) error {
	if count <= maximum {
		return nil
	}
	return failure(
		ErrorValidation,
		fmt.Sprintf("a campaign can include up to %d attachments", maximum),
		nil,
	)
}

func validateAttachmentSource(input mail.Attachment) (string, error) {
	filename := safeFilename(input.Filename)
	if filename == "" {
		return "", failure(ErrorValidation, "attachment filename is required", nil)
	}
	if len(input.Content) == 0 {
		return "", failure(ErrorValidation, fmt.Sprintf("%s is empty", filename), nil)
	}
	if len(input.Content) > MaxCampaignAttachmentBytes {
		return "", failure(
			ErrorValidation,
			fmt.Sprintf("%s is larger than %d MB", filename, MaxCampaignAttachmentBytes>>20),
			nil,
		)
	}
	return filename, nil
}

func attachmentContentType(filename string, content []byte) string {
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "application/octet-stream"
	}
	return mediaType
}

func isDOCXFilename(filename string) bool {
	return strings.EqualFold(filepath.Ext(filename), ".docx")
}

func archiveStaticAttachments(attachments []mail.Attachment) []document.StaticAttachment {
	result := make([]document.StaticAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		if isDOCXFilename(attachment.Filename) {
			continue
		}
		result = append(result, document.StaticAttachment{
			Filename: attachment.Filename,
			Content:  attachment.Content,
		})
	}
	return result
}

func prepareSharedAttachments(
	ctx context.Context,
	converter document.DOCXToPDFConverter,
	attachments []mail.Attachment,
	documents []document.CampaignTemplate,
	budget *attachmentBudget,
) ([]document.GeneratedPDF, int64, error) {
	reserved := staticAttachmentBytes(attachments)
	if err := budget.acquire(ctx, reserved); err != nil {
		return nil, 0, err
	}
	staticPDFs, convertedBytes, err := prepareStaticPDFs(ctx, converter, documents, budget)
	if err != nil {
		budget.release(reserved)
		return nil, 0, err
	}
	return staticPDFs, reserved + convertedBytes, nil
}

func prepareStaticPDFs(
	ctx context.Context,
	converter document.DOCXToPDFConverter,
	inputs []document.CampaignTemplate,
	budget *attachmentBudget,
) ([]document.GeneratedPDF, int64, error) {
	var result []document.GeneratedPDF
	var reserved int64
	err := converter.ConvertBatch(ctx, document.StaticDOCX(inputs), func(converted []document.ConvertedPDF) error {
		for _, item := range converted {
			reserved += item.Size
		}
		if err := budget.acquire(ctx, reserved); err != nil {
			reserved = 0
			return err
		}
		var err error
		result, err = document.ReadConvertedPDFs(converted)
		return err
	})
	if err != nil {
		budget.release(reserved)
		return nil, 0, err
	}
	return result, reserved, nil
}

func prepareAddressEntryAttachments(
	ctx context.Context,
	converter document.DOCXToPDFConverter,
	attachmentInputs []mail.Attachment,
	documents []document.CampaignTemplate,
	fields map[string]string,
	staticPDFs []document.GeneratedPDF,
	budget *attachmentBudget,
) ([]mail.Attachment, int64, error) {
	var personalized []document.GeneratedPDF
	var reservedBytes int64
	err := converter.ConvertBatch(
		ctx,
		document.PersonalizedDOCX(documents, fields),
		func(converted []document.ConvertedPDF) error {
			convertedBytes := convertedPDFBytes(converted)
			if err := budget.acquire(ctx, convertedBytes); err != nil {
				return err
			}
			reservedBytes = convertedBytes
			var err error
			personalized, err = document.ReadConvertedPDFs(converted)
			return err
		},
	)
	if err != nil {
		budget.release(reservedBytes)
		return nil, 0, err
	}
	byID := make(map[int]document.GeneratedPDF, len(staticPDFs)+len(personalized))
	for _, item := range staticPDFs {
		byID[item.DocumentID] = item
	}
	for _, item := range personalized {
		byID[item.DocumentID] = item
	}
	outputNames := document.ResolveOutputFilenames(documents, fields)
	attachments := make([]mail.Attachment, 0, len(attachmentInputs))
	usedNames := map[string]bool{}
	documentID := 0
	for _, input := range attachmentInputs {
		if !isDOCXFilename(input.Filename) {
			attachments = append(attachments, mail.Attachment{
				Filename:    uniqueAttachmentFilename(input.Filename, usedNames),
				ContentType: input.ContentType,
				Size:        len(input.Content),
				Content:     input.Content,
			})
			continue
		}
		item, ok := byID[documentID]
		if !ok {
			budget.release(reservedBytes)
			return nil, 0, fmt.Errorf("document %d has no converted PDF", documentID)
		}
		attachments = append(attachments, mail.Attachment{
			Filename:    uniqueAttachmentFilename(outputNames[documentID], usedNames),
			ContentType: "application/pdf",
			Size:        len(item.Content),
			Content:     item.Content,
		})
		documentID++
	}
	return attachments, reservedBytes, nil
}

func staticAttachmentBytes(attachments []mail.Attachment) int64 {
	var total int64
	for _, attachment := range attachments {
		if !isDOCXFilename(attachment.Filename) {
			total += int64(len(attachment.Content))
		}
	}
	return total
}

func convertedPDFBytes(documents []document.ConvertedPDF) int64 {
	var total int64
	for _, item := range documents {
		total += item.Size
	}
	return total
}

func uniqueAttachmentFilename(filename string, used map[string]bool) string {
	name := safeFilename(filename)
	if name == "" {
		name = "attachment"
	}
	key := strings.ToLower(name)
	if !used[key] {
		used[key] = true
		return name
	}
	extension := filepath.Ext(name)
	base := strings.TrimSuffix(name, extension)
	for sequence := 2; ; sequence++ {
		candidate := fmt.Sprintf("%s-%d%s", base, sequence, extension)
		key = strings.ToLower(candidate)
		if !used[key] {
			used[key] = true
			return candidate
		}
	}
}

func attachmentPageCount(attachment mail.Attachment) int {
	contentType, _, _ := strings.Cut(attachment.ContentType, ";")
	if !strings.EqualFold(strings.TrimSpace(contentType), "application/pdf") {
		return 0
	}
	return document.PDFPageCount(attachment.Content)
}
