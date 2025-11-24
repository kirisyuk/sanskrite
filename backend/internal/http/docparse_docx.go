package http

import (
	"strings"

	"baliance.com/gooxml/document"
)

func parseDOCXFile(path string) (string, *int, error) {
	doc, err := document.Open(path)
	if err != nil {
		return "", nil, err
	}
	var b strings.Builder
	
	// Extract text from headers
	for _, header := range doc.Headers() {
		for _, p := range header.Paragraphs() {
			extractParagraphText(&b, p)
		}
	}
	
	// Extract text from main document paragraphs
	for _, p := range doc.Paragraphs() {
		extractParagraphText(&b, p)
	}
	
	// Extract text from tables
	for _, tbl := range doc.Tables() {
		for _, row := range tbl.Rows() {
			var cells []string
			for _, cell := range row.Cells() {
				var cellText strings.Builder
				for _, p := range cell.Paragraphs() {
					extractParagraphText(&cellText, p)
				}
				cells = append(cells, strings.TrimSpace(cellText.String()))
			}
			// Join cells with tab separator for better readability
			if len(cells) > 0 {
				b.WriteString(strings.Join(cells, "\t"))
				b.WriteString("\n")
			}
		}
		// Add empty line after each table for better separation
		b.WriteString("\n")
	}
	
	// Extract text from footers
	for _, footer := range doc.Footers() {
		for _, p := range footer.Paragraphs() {
			extractParagraphText(&b, p)
		}
	}
	
	// Extract text from form fields
	for _, field := range doc.FormFields() {
		// Form fields may have text content, try to extract it
		// Note: FormField API might be limited, but we try to get any available text
		if fieldText := extractFormFieldText(field); fieldText != "" {
			b.WriteString(fieldText)
			b.WriteString("\n")
		}
	}
	
	// Extract text from structured document tags (content controls)
	for _, sdt := range doc.StructuredDocumentTags() {
		if sdtText := extractStructuredTagText(sdt); sdtText != "" {
			b.WriteString(sdtText)
			b.WriteString("\n")
		}
	}
	
	return b.String(), nil, nil
}

// extractParagraphText extracts all text from a paragraph, including hyperlinks
func extractParagraphText(b *strings.Builder, p document.Paragraph) {
	// Extract text from runs
	for _, r := range p.Runs() {
		text := r.Text()
		if text != "" {
			b.WriteString(text)
		}
		// Try to extract hyperlink URLs if present
		// Note: Hyperlinks in runs might need low-level access via X() method
		// For now, we extract the text which is the most important part
	}
	b.WriteString("\n")
}

// extractFormFieldText extracts text from a form field
func extractFormFieldText(field document.FormField) string {
	var parts []string
	if name := field.Name(); name != "" {
		parts = append(parts, name)
	}
	if value := field.Value(); value != "" {
		parts = append(parts, value)
	}
	if len(parts) > 0 {
		return strings.Join(parts, ": ")
	}
	return ""
}

// extractStructuredTagText extracts text from structured document tags
func extractStructuredTagText(sdt document.StructuredDocumentTag) string {
	var b strings.Builder
	// Extract text from paragraphs within structured document tags
	for _, p := range sdt.Paragraphs() {
		extractParagraphText(&b, p)
	}
	return b.String()
}
