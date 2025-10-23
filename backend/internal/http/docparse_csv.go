package http

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// parseCSVBytes parses CSV/TSV data, detects delimiter (unless forced by isTSV),
// builds readable text (TSV-like), returns ParseMeta and a normalized UTF-8 CSV copy.
// Normalized CSV uses comma delimiter, LF newlines, and RFC4180 quoting.
func parseCSVBytes(data []byte, isTSV bool) (text string, meta map[string]any, normalized []byte, err error) {
	cleaned := stripUTF8BOM(data)

	// Detect delimiter
	delim := ','
	delimName := ","
	if isTSV {
		delim = '\t'
		delimName = "\t"
	} else {
		if d, name := sniffDelimiter(cleaned); d != 0 {
			delim, delimName = d, name
		}
	}

	r := csv.NewReader(bytes.NewReader(cleaned))
	r.Comma = delim
	r.FieldsPerRecord = -1 // allow ragged rows
	r.LazyQuotes = true
	r.TrimLeadingSpace = false

	rows, err := r.ReadAll()
	if err != nil {
		return "", nil, nil, err
	}

	// Determine columns and header
	colCount := 0
	for _, row := range rows {
		if len(row) > colCount {
			colCount = len(row)
		}
	}
	hasHeader := false
	var header []string
	if len(rows) > 0 {
		hasHeader = looksLikeHeader(rows[0], rows)
		if hasHeader {
			header = padRow(rows[0], colCount)
		} else {
			header = make([]string, colCount)
			for i := 0; i < colCount; i++ {
				header[i] = fmt.Sprintf("col_%d", i+1)
			}
		}
	}

	// Build normalized CSV (comma delimiter)
	var normBuf bytes.Buffer
	w := csv.NewWriter(&normBuf)
	w.Comma = ','
	// Write header then rows
	if colCount > 0 {
		if err := w.Write(header); err != nil {
			return "", nil, nil, err
		}
		start := 0
		if hasHeader {
			start = 1
		}
		for i := start; i < len(rows); i++ {
			if err := w.Write(padRow(rows[i], colCount)); err != nil {
				return "", nil, nil, err
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", nil, nil, err
	}

	// Build human-readable TSV text (for ExtractedText)
	var textBuf strings.Builder
	if colCount > 0 {
		textBuf.WriteString(strings.Join(escapeTabs(header), "\t"))
		textBuf.WriteByte('\n')
		start := 0
		if hasHeader {
			start = 1
		}
		for i := start; i < len(rows); i++ {
			textBuf.WriteString(strings.Join(escapeTabs(padRow(rows[i], colCount)), "\t"))
			textBuf.WriteByte('\n')
		}
	}

	// Columns metadata (names + simple type hint)
	cols := make([]map[string]any, colCount)
	for i := 0; i < colCount; i++ {
		name := header[i]
		typ := inferColumnType(rows, hasHeader, i)
		cols[i] = map[string]any{"name": name, "type": typ}
	}

	// Preview: up to 50 rows as array of maps
	preview := make([]map[string]any, 0, minInt(50, maxInt(0, len(rows)-(btoi(hasHeader)))))
	limit := 50
	start := 0
	if hasHeader {
		start = 1
	}
	for i := start; i < len(rows) && len(preview) < limit; i++ {
		row := padRow(rows[i], colCount)
		item := make(map[string]any, colCount)
		for c := 0; c < colCount; c++ {
			item[header[c]] = row[c]
		}
		preview = append(preview, item)
	}

	meta = map[string]any{
		"dataKind":  "tabular",
		"delimiter": delimName,
		"hasHeader": hasHeader,
		"rowCount":  maxInt(0, len(rows)-(btoi(hasHeader))),
		"colCount":  colCount,
		"columns":   cols,
		"charset":   "utf-8",
		"preview":   preview,
	}

	return textBuf.String(), meta, normBuf.Bytes(), nil
}

func sniffDelimiter(data []byte) (rune, string) {
	// Look at first few lines up to 64KB and count occurrences of candidate delimiters
	sample := firstNLines(data, 8, 64*1024)
	candidates := []rune{',', ';', '\t'}
	counts := make(map[rune]int)
	firstLineSep := make(map[rune]int)
	firstLine := firstLineOf(sample)
	for _, c := range candidates {
		counts[c] = strings.Count(sample, string(c))
		firstLineSep[c] = strings.Count(firstLine, string(c))
	}
	// Prefer delimiter that appears in the first line with highest distinct column count
	type cand struct {
		r     rune
		first int
		total int
	}
	arr := make([]cand, 0, len(candidates))
	for _, c := range candidates {
		arr = append(arr, cand{r: c, first: firstLineSep[c], total: counts[c]})
	}
	sort.Slice(arr, func(i, j int) bool {
		if arr[i].first == arr[j].first {
			return arr[i].total > arr[j].total
		}
		return arr[i].first > arr[j].first
	})
	if len(arr) == 0 || (arr[0].first == 0 && arr[0].total == 0) {
		return ',', ","
	}
	top := arr[0].r
	switch top {
	case ',':
		return ',', ","
	case ';':
		return ';', ";"
	case '\t':
		return '\t', "\t"
	}
	return ',', ","
}

func looksLikeHeader(first []string, rows [][]string) bool {
	// Heuristic: header cells are mostly non-numeric and unique; compare to next non-empty row
	if len(rows) < 2 {
		return true
	}
	next := rows[1]
	// expand rows to same length for comparison
	n := len(first)
	if len(next) < n {
		n = len(next)
	}
	uniq := map[string]struct{}{}
	nonNumeric := 0
	for i := 0; i < n; i++ {
		v := strings.TrimSpace(first[i])
		if v == "" {
			continue
		}
		if !isMostlyNumeric(v) {
			nonNumeric++
		}
		uniq[v] = struct{}{}
	}
	// if most cells are non-numeric and unique, treat as header
	return nonNumeric >= n/2 && len(uniq) >= n/2
}

func isMostlyNumeric(s string) bool {
	// consider numeric if digits and separators constitute majority
	digits := 0
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '+' || r == '-' || r == '.' || r == ',' || r == ' ' {
			digits++
		}
	}
	return digits*2 >= utf8.RuneCountInString(s)
}

func padRow(row []string, n int) []string {
	if len(row) >= n {
		return row
	}
	out := make([]string, n)
	copy(out, row)
	for i := len(row); i < n; i++ {
		out[i] = ""
	}
	return out
}

func escapeTabs(row []string) []string {
	out := make([]string, len(row))
	for i := range row {
		out[i] = strings.ReplaceAll(row[i], "\t", " ")
	}
	return out
}

func firstNLines(data []byte, lines int, max int) string {
	// return up to 'lines' lines as string, capped to 'max' bytes
	if max > 0 && len(data) > max {
		data = data[:max]
	}
	r := bytes.NewReader(data)
	var b strings.Builder
	readLines := 0
	buf := make([]byte, 1024)
	for {
		if readLines >= lines {
			break
		}
		n, err := r.Read(buf)
		if n > 0 {
			for i := 0; i < n; i++ {
				ch := buf[i]
				b.WriteByte(ch)
				if ch == '\n' {
					readLines++
					if readLines >= lines {
						break
					}
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
	}
	return b.String()
}

func firstLineOf(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}

func stripUTF8BOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

func inferColumnType(rows [][]string, hasHeader bool, col int) string {
	// sample up to 100 rows
	start := 0
	if hasHeader {
		start = 1
	}
	end := start + 100
	if end > len(rows) {
		end = len(rows)
	}
	numeric := 0
	nonEmpty := 0
	for i := start; i < end; i++ {
		if col >= len(rows[i]) {
			continue
		}
		v := strings.TrimSpace(rows[i][col])
		if v == "" {
			continue
		}
		nonEmpty++
		if isMostlyNumeric(v) {
			numeric++
		}
	}
	if nonEmpty == 0 {
		return "string"
	}
	if numeric*2 >= nonEmpty {
		return "number"
	}
	return "string"
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
