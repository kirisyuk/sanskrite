package http

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
	"github.com/igorkirisyuk/sanskrite/internal/telegram"
)

// PreviewTelegramChannel handles GET /api/v1/sources/preview/telegram
// Query parameter: channel (required) - Telegram channel username (e.g., @channelname or channelname)
// Returns up to 20 latest messages from the channel
func (h *DiscoveryHandler) PreviewTelegramChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Get channel parameter
	channel := r.URL.Query().Get("channel")
	if channel == "" {
		WriteBadRequest(w, "VALIDATION_ERROR", "channel parameter is required")
		return
	}

	// Normalize channel username (remove @ if present, ensure lowercase)
	channel = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(channel), "@"))
	if channel == "" {
		WriteBadRequest(w, "VALIDATION_ERROR", "invalid channel name")
		return
	}

	// Get Telegram service URL from environment
	telegramServiceURL := getTelegramServiceURL()
	if telegramServiceURL == "" {
		WriteInternalError(w, "Telegram service not configured")
		return
	}

	tgClient := telegram.NewClient(telegramServiceURL)

	// Fetch updates (last 20 messages)
	// Increased timeout to 45 seconds to account for cold starts and Telegram API delays
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	messages, err := tgClient.FetchUpdates(ctx, channel, 0)
	if err != nil {
		h.logger.Printf("Failed to fetch Telegram updates: %v", err)
		WriteError(w, http.StatusBadGateway, "TELEGRAM_ERROR", fmt.Sprintf("Failed to fetch channel updates: %v", err), nil)
		return
	}

	// Limit to 20 most recent messages
	// Telegram service (Telethon iter_messages) returns messages in reverse chronological order (newest first),
	// so we take the first 20 messages (most recent)
	if len(messages) > 20 {
		messages = messages[:20]
	}

	// Format response
	response := map[string]interface{}{
		"channel":  channel,
		"count":    len(messages),
		"messages": messages,
	}

	WriteJSON(w, http.StatusOK, response)
}

// PreviewRSSFeed handles GET /api/v1/sources/preview/rss
// Query parameter: url (required) - RSS feed URL
// Returns up to 20 latest items from the feed
func (h *DiscoveryHandler) PreviewRSSFeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Get URL parameter
	feedURL := r.URL.Query().Get("url")
	if feedURL == "" {
		WriteBadRequest(w, "VALIDATION_ERROR", "url parameter is required")
		return
	}

	feedURL = strings.TrimSpace(feedURL)
	if feedURL == "" {
		WriteBadRequest(w, "VALIDATION_ERROR", "invalid URL")
		return
	}

	// Validate URL format
	parsedURL, err := url.Parse(feedURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		WriteBadRequest(w, "VALIDATION_ERROR", "invalid URL format")
		return
	}

	// Only allow http and https schemes
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		WriteBadRequest(w, "VALIDATION_ERROR", "URL must use http or https scheme")
		return
	}

	// Create RSS provider
	rssProvider := providers.NewRSSProvider()

	// Fetch feed
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	docs, err := rssProvider.Ingest(ctx, authInfo.TenantID, feedURL)
	if err != nil {
		h.logger.Printf("Failed to fetch RSS feed: %v", err)
		WriteError(w, http.StatusBadGateway, "RSS_ERROR", fmt.Sprintf("Failed to fetch RSS feed: %v", err), nil)
		return
	}

	// Limit to 20 most recent items
	// RSS feeds typically return items in reverse chronological order (newest first),
	// but we ensure we take the first 20 which should be the most recent
	// If feed returns oldest first, we'd need to reverse, but standard RSS feeds are newest first
	if len(docs) > 20 {
		docs = docs[:20]
	}

	// Format response - convert NormalizedDocument to preview format
	items := make([]map[string]interface{}, 0, len(docs))
	for _, doc := range docs {
		item := map[string]interface{}{
			"id":          doc.ExternalID,
			"title":      doc.Title,
			"url":        doc.URL,
			"text":       doc.FullText,
			"publishedAt": nil,
		}
		if doc.OriginPublishedAt != nil {
			item["publishedAt"] = doc.OriginPublishedAt.Format(time.RFC3339)
		}
		if doc.SourceMeta != nil {
			if feedTitle, ok := doc.SourceMeta["feedTitle"].(string); ok {
				item["feedTitle"] = feedTitle
			}
		}
		items = append(items, item)
	}

	response := map[string]interface{}{
		"url":   feedURL,
		"count": len(items),
		"items": items,
	}

	WriteJSON(w, http.StatusOK, response)
}

// getTelegramServiceURL retrieves Telegram service URL from environment variable
func getTelegramServiceURL() string {
	// Get from environment variable TELEGRAM_SERVICE_URL
	// This is set during deployment
	return strings.TrimSpace(os.Getenv("TELEGRAM_SERVICE_URL"))
}

