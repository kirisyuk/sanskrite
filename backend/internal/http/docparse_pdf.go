package http

import (
	"io"

	pdf "github.com/ledongthuc/pdf"
)

func parsePDFFile(path string) (string, *int, error) {
	f, r, err := pdf.Open(path)
	if f != nil {
		defer f.Close()
	}
	if err != nil {
		return "", nil, err
	}
	var total int
	if r != nil {
		total = r.NumPage()
	}
	reader, err := r.GetPlainText()
	if err != nil {
		return "", nil, err
	}
	buf, err := io.ReadAll(reader)
	if err != nil {
		return "", nil, err
	}
	var pages *int
	if total > 0 {
		pages = &total
	}
	return string(buf), pages, nil
}
