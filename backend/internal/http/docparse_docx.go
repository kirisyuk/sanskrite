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
	for _, p := range doc.Paragraphs() {
		for _, r := range p.Runs() {
			b.WriteString(r.Text())
		}
		b.WriteString("\n")
	}
	return b.String(), nil, nil
}
