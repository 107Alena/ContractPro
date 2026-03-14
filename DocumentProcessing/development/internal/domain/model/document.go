package model

// OCRStatus represents whether OCR was applied to the document.
type OCRStatus string

const (
	OCRStatusApplicable    OCRStatus = "applicable"
	OCRStatusNotApplicable OCRStatus = "not_applicable"
)

// InputDocumentReference holds metadata about the source document file.
type InputDocumentReference struct {
	DocumentID string `json:"document_id"`
	FileName   string `json:"file_name"`
	FileSize   int64  `json:"file_size"`
	MimeType   string `json:"mime_type"`
	Checksum   string `json:"checksum,omitempty"`
}

// PageText represents the extracted text of a single page.
type PageText struct {
	PageNumber int    `json:"page_number"`
	Text       string `json:"text"`
}

// ExtractedText holds the per-page extracted text of a document.
type ExtractedText struct {
	DocumentID string     `json:"document_id"`
	Pages      []PageText `json:"pages"`
}

// FullText returns the concatenated text of all pages.
func (e *ExtractedText) FullText() string {
	if len(e.Pages) == 0 {
		return ""
	}
	if len(e.Pages) == 1 {
		return e.Pages[0].Text
	}
	var size int
	for _, p := range e.Pages {
		size += len(p.Text)
	}
	size += len(e.Pages) - 1 // newlines between pages
	buf := make([]byte, 0, size)
	for i, p := range e.Pages {
		if i > 0 {
			buf = append(buf, '\n')
		}
		buf = append(buf, p.Text...)
	}
	return string(buf)
}

// OCRRawArtifact holds the raw OCR result or a not_applicable marker.
type OCRRawArtifact struct {
	RawText string    `json:"raw_text,omitempty"`
	Status  OCRStatus `json:"status"`
}
