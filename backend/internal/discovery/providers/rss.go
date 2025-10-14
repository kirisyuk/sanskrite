package providers

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"time"
)

type RSSProvider struct {
	client *http.Client
}

func NewRSSProvider() *RSSProvider {
	return &RSSProvider{client: &http.Client{Timeout: 20 * time.Second}}
}

func (p *RSSProvider) Key() string { return "rss" }

func (p *RSSProvider) Capability() Capability {
	return Capability{SupportsPolling: true, SupportsWebhook: false, MaxItemsPerRun: 100, RateLimitPerMin: 120, PageLimit: 0}
}

// minimal RSS parsing
type rssFeed struct {
	Channel struct {
		Title string    `xml:"title"`
		Link  string    `xml:"link"`
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	GUID        string `xml:"guid"`
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
	Content     string `xml:"encoded"`
}

func (p *RSSProvider) Ingest(ctx context.Context, tenantID string, sourceID string) ([]NormalizedDocument, error) {
	// feed URL should be provided via Source config; in this minimal adapter assume sourceID is the URL
	feedURL := sourceID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, err
	}
	var out []NormalizedDocument
	for _, it := range feed.Channel.Items {
		text := it.Content
		if strings.TrimSpace(text) == "" {
			text = it.Description
		}
		var published *time.Time
		if t, err := time.Parse(time.RFC1123Z, it.PubDate); err == nil {
			published = &t
		} else if t2, err2 := time.Parse(time.RFC1123, it.PubDate); err2 == nil {
			published = &t2
		}
		nd := NormalizedDocument{
			ExternalID:        firstNonEmpty(it.GUID, it.Link, it.Title+"/"+it.PubDate),
			Title:             it.Title,
			URL:               it.Link,
			DocumentPath:      "rss/" + feed.Channel.Link + "#" + firstNonEmpty(it.GUID, it.Link),
			FullText:          htmlToText(text),
			Language:          "",
			ContentType:       "plainText",
			DocumentType:      "article",
			Authors:           nil,
			SourceMeta:        map[string]any{"feedTitle": feed.Channel.Title},
			OriginPublishedAt: published,
		}
		out = append(out, nd)
	}
	return out, nil
}

func (p *RSSProvider) Search(ctx context.Context, tenantID string, sourceID string, query string, opts map[string]any) ([]NormalizedDocument, error) {
	return []NormalizedDocument{}, nil
}

func (p *RSSProvider) Ground(ctx context.Context, tenantID string, sourceID string, references []string) ([]NormalizedDocument, error) {
	return []NormalizedDocument{}, nil
}

func (p *RSSProvider) Webhook(ctx context.Context, tenantID string, sourceID string, event map[string]any) error {
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func htmlToText(s string) string {
	// very naive strip; real impl may use proper HTML to text
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "<br>", "\n"), "<br/>", "\n"))
}
