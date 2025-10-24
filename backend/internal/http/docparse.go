package http

import (
	"os"
	"strings"
)

// parseByContentType extracts text and best-effort page count from raw bytes using content type.
// For pdf/docx we use external libraries via temp files to avoid complex reader plumbing.
func parseByContentType(contentType string, data []byte) (text string, pages *int, err error) {
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "application/pdf"):
		t, p, e := parsePDFBytes(data)
		return t, p, e
	case strings.Contains(ct, "application/vnd.openxmlformats-officedocument.wordprocessingml.document") || strings.Contains(ct, "application/msword"):
		t, p, e := parseDOCXBytes(data)
		return t, p, e
	case strings.Contains(ct, "text/markdown"), strings.Contains(ct, "text/plain"), strings.HasPrefix(ct, "text/"):
		s := string(data)
		return s, nil, nil
	default:
		s := string(data)
		return s, nil, nil
	}
}

// parsePDFBytes uses ledongthuc/pdf to get plain text.
func parsePDFBytes(data []byte) (string, *int, error) {
	tmp, err := os.CreateTemp("", "sx-pdf-*.pdf")
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", nil, err
	}
	_ = tmp.Close()

	// ledongthuc/pdf API via Open(path)
	// import at compile time; noop here
	// actual implementation in separate helper to avoid unused import when tests stub
	return parsePDFFile(tmp.Name())
}

// parseDOCXBytes uses baliance/gooxml to extract text from docx.
func parseDOCXBytes(data []byte) (string, *int, error) {
	tmp, err := os.CreateTemp("", "sx-docx-*.docx")
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", nil, err
	}
	_ = tmp.Close()
	return parseDOCXFile(tmp.Name())
}
