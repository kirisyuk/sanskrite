package crawler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// WebCrawler handles web content fetching and text extraction
type WebCrawler struct {
	client    *http.Client
	userAgent string
}

// NewWebCrawler creates a new web crawler with default settings
func NewWebCrawler() *WebCrawler {
	return &WebCrawler{
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Allow up to 10 redirects
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				return nil
			},
		},
		userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
	}
}

// FetchResult contains the fetched content and metadata
type FetchResult struct {
	URL         string
	ContentType string
	Text        string
	ByteSize    int
	FetchedAt   time.Time
}

// FetchContent fetches a URL and extracts readable text
func (c *WebCrawler) FetchContent(ctx context.Context, url string) (*FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers to appear as a real browser
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,ru;q=0.8")
	req.Header.Set("Accept-Encoding", "identity") // Don't request compression to simplify
	req.Header.Set("DNT", "1")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Read body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")

	// Extract text based on content type
	var text string
	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml") {
		text = extractHTMLText(bodyBytes)
	} else if strings.Contains(contentType, "text/plain") {
		text = string(bodyBytes)
	} else {
		// For other types, try to extract as text
		text = string(bodyBytes)
	}

	return &FetchResult{
		URL:         url,
		ContentType: contentType,
		Text:        text,
		ByteSize:    len(bodyBytes),
		FetchedAt:   time.Now().UTC(),
	}, nil
}

// extractHTMLText extracts plain text from HTML content
func extractHTMLText(htmlBytes []byte) string {
	// Remove <script> and <style> tags (Go regexp doesn't support backreferences, use two patterns)
	reScript := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	cleaned := reScript.ReplaceAll(htmlBytes, []byte{})
	reStyle := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	cleaned = reStyle.ReplaceAll(cleaned, []byte{})

	// Parse HTML
	doc, err := html.Parse(bytes.NewReader(cleaned))
	if err != nil {
		// Fallback: strip tags with regex
		return extractHTMLTextRegex(htmlBytes)
	}

	var text strings.Builder
	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
			text.WriteString(" ")
		}
		// Add newlines for block elements
		if n.Type == html.ElementNode {
			switch n.Data {
			case "br", "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6":
				text.WriteString("\n")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}
	traverse(doc)

	result := text.String()

	// Decode HTML entities
	result = htmlUnescapeString(result)

	// Normalize whitespace
	result = normalizeWhitespace(result)

	return strings.TrimSpace(result)
}

// extractHTMLTextRegex is a fallback HTML parser using regex
func extractHTMLTextRegex(htmlBytes []byte) string {
	html := string(htmlBytes)

	// Remove <script> and <style> blocks (Go regexp doesn't support backreferences, use two patterns)
	reScript := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	html = reScript.ReplaceAllString(html, "")
	reStyle := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	html = reStyle.ReplaceAllString(html, "")

	// Replace <br> and </p> with newlines
	reBr := regexp.MustCompile(`(?i)<br\s*/?>`)
	html = reBr.ReplaceAllString(html, "\n")
	reP := regexp.MustCompile(`(?i)</p>`)
	html = reP.ReplaceAllString(html, "\n")

	// Remove all HTML tags
	reTags := regexp.MustCompile(`<[^>]*>`)
	text := reTags.ReplaceAllString(html, "")

	// Decode HTML entities
	text = htmlUnescapeString(text)

	// Normalize whitespace
	text = normalizeWhitespace(text)

	return strings.TrimSpace(text)
}

// normalizeWhitespace normalizes whitespace in text
func normalizeWhitespace(text string) string {
	// Replace tabs with spaces
	text = strings.ReplaceAll(text, "\t", " ")

	// Replace multiple spaces with single space
	reSpaces := regexp.MustCompile(`[ ]+`)
	text = reSpaces.ReplaceAllString(text, " ")

	// Replace spaces before newlines
	reSpaceNewline := regexp.MustCompile(`[ ]+\n`)
	text = reSpaceNewline.ReplaceAllString(text, "\n")

	// Replace multiple newlines with double newline
	reNewlines := regexp.MustCompile(`\n{3,}`)
	text = reNewlines.ReplaceAllString(text, "\n\n")

	// Normalize line endings
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	return text
}

// htmlUnescapeString decodes a subset of HTML entities
func htmlUnescapeString(s string) string {
	// Use standard library minimal decoding via strings.Replacer for common entities
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
	)
	return replacer.Replace(s)
}

// CanonicalizeURL normalizes a URL for deduplication
func CanonicalizeURL(rawURL string) string {
	// Force HTTPS
	if strings.HasPrefix(rawURL, "http://") {
		rawURL = "https://" + strings.TrimPrefix(rawURL, "http://")
	}

	// Remove query parameters and hash
	if idx := strings.Index(rawURL, "?"); idx != -1 {
		rawURL = rawURL[:idx]
	}
	if idx := strings.Index(rawURL, "#"); idx != -1 {
		rawURL = rawURL[:idx]
	}

	// Remove trailing slash
	rawURL = strings.TrimSuffix(rawURL, "/")

	// Convert to lowercase
	rawURL = strings.ToLower(rawURL)

	return rawURL
}
