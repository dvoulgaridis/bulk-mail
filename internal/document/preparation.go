package document

import (
	"bytes"
	"context"
)

// PreparedDocument is either a temporary converted PDF or an in-memory DOCX.
// Path is valid only during the callback passed to PrepareBatch.
type PreparedDocument struct {
	DocumentID int
	Path       string
	Content    []byte
	Size       int64
}

func PrepareBatch(
	ctx context.Context,
	converter DOCXToPDFConverter,
	inputs []DOCXInput,
	consume func([]PreparedDocument) error,
) error {
	prepared := make([]PreparedDocument, 0, len(inputs))
	pdfInputs := make([]DOCXInput, 0, len(inputs))
	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if input.ConvertToPDF {
			pdfInputs = append(pdfInputs, input)
			continue
		}
		var content bytes.Buffer
		if err := input.WriteTo(&content); err != nil {
			return err
		}
		prepared = append(prepared, PreparedDocument{
			DocumentID: input.DocumentID, Content: content.Bytes(), Size: int64(content.Len()),
		})
	}
	if len(pdfInputs) == 0 {
		return consume(prepared)
	}
	return converter.ConvertBatch(ctx, pdfInputs, func(converted []ConvertedPDF) error {
		for _, item := range converted {
			prepared = append(prepared, PreparedDocument{
				DocumentID: item.DocumentID, Path: item.Path, Size: item.Size,
			})
		}
		return consume(prepared)
	})
}
