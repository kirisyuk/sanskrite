package providers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/mmcdole/gofeed"
)

type RSSProvider struct {
	client *http.Client
	parser *gofeed.Parser
	policy *bluemonday.Policy
}

func NewRSSProvider() *RSSProvider {
	fp := gofeed.NewParser()
	fp.Client = &http.Client{Timeout: 20 * time.Second}
	return &RSSProvider{
		client: fp.Client,
		parser: fp,
		policy: bluemonday.StrictPolicy(), // Strips all HTML tags, keeps only text
	}
}

func (p *RSSProvider) Key() string { return "rss" }

func (p *RSSProvider) Capability() Capability {
	return Capability{SupportsPolling: true, SupportsWebhook: false, MaxItemsPerRun: 100, RateLimitPerMin: 120, PageLimit: 0}
}

func (p *RSSProvider) Ingest(ctx context.Context, tenantID string, sourceID string) ([]NormalizedDocument, error) {
	// feed URL should be provided via Source config; in this minimal adapter assume sourceID is the URL
	feedURL := sourceID
	
	// Use gofeed to parse the feed (handles RSS, Atom, JSON Feed)
	feed, err := p.parser.ParseURLWithContext(feedURL, ctx)
	if err != nil {
		return nil, err
	}

	var out []NormalizedDocument
	for _, it := range feed.Items {
		// Prioritize full content, fallback to description
		text := it.Content
		if strings.TrimSpace(text) == "" {
			text = it.Description
		}

		// Determine publish date
		var published *time.Time
		if it.PublishedParsed != nil {
			published = it.PublishedParsed
		} else if it.UpdatedParsed != nil {
			published = it.UpdatedParsed
		}

		// Extract authors
		var authors []string
		if it.Author != nil {
			authors = append(authors, it.Author.Name)
		}
		for _, a := range it.Authors {
			if a.Name != "" {
				found := false
				for _, existing := range authors {
					if existing == a.Name {
						found = true
						break
					}
				}
				if !found {
					authors = append(authors, a.Name)
				}
			}
		}

		// Construct external ID
		extID := firstNonEmpty(it.GUID, it.Link, it.Title)
		
		// Determine document path
		docPath := "rss/"
		if feed.Link != "" {
			docPath += feed.Link
		} else {
			docPath += "unknown"
		}
		docPath += "#" + extID

		nd := NormalizedDocument{
			ExternalID:        extID,
			Title:             it.Title,
			URL:               it.Link,
			DocumentPath:      docPath,
			FullText:          p.cleanHTML(text),
			Language:          feed.Language,
			ContentType:       "plainText",
			DocumentType:      "article",
			Authors:           authors,
			SourceMeta:        map[string]any{"feedTitle": feed.Title, "feedDescription": feed.Description},
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

func (p *RSSProvider) cleanHTML(s string) string {
	// Use bluemonday to strip all HTML tags and return clean text
	return strings.TrimSpace(p.policy.Sanitize(s))
}
