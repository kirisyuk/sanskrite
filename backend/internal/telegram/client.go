package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client handles communication with the Telegram service
type Client struct {
	baseURL string
	client  *http.Client
}

// NewClient creates a new Telegram service client
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// TelegramMessage represents a Telegram message
type TelegramMessage struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
	Date string `json:"date"`
	Link string `json:"link"`
}

// FetchUpdatesRequest represents the request body for fetching updates
type FetchUpdatesRequest struct {
	Channel string `json:"channel"`
	LastID  int    `json:"last_id"`
}

// FetchUpdates fetches new messages from a Telegram channel
func (c *Client) FetchUpdates(ctx context.Context, channelUsername string, lastMessageID int) ([]TelegramMessage, error) {
	reqBody := FetchUpdatesRequest{
		Channel: channelUsername,
		LastID:  lastMessageID,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/telegram/updates", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram service returned status %d", resp.StatusCode)
	}

	var messages []TelegramMessage
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return messages, nil
}

// adapter for discovery.TelegramFetcher expected type
// Note: orchestrator uses a compatible subset (ID, Text, Date, Link)

// Ensure interface compatibility with discovery.TelegramFetcher
// This method signature already matches; the orchestrator expects a slice of structs {ID,Text,Date,Link}

// HealthCheck checks if the Telegram service is available
func (c *Client) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	return nil
}
