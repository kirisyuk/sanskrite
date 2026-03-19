package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var (
	backendURL    = os.Getenv("BACKEND_URL")
	functionsKey  = os.Getenv("FUNCTIONS_KEY")
	allowedOrigins = parseAllowedOrigins()
)

// parseAllowedOrigins parses ALLOWED_ORIGIN or ALLOWED_ORIGINS env var (comma or pipe separated)
// Falls back to defaults if not set
func parseAllowedOrigins() []string {
	// Try ALLOWED_ORIGINS first (plural, comma or pipe separated)
	if env := os.Getenv("ALLOWED_ORIGINS"); env != "" {
		splitFn := func(r rune) bool { return r == ',' || r == '|' }
		var origins []string
		for _, o := range strings.FieldsFunc(env, splitFn) {
			o = strings.TrimSpace(o)
			if o != "" {
				origins = append(origins, o)
			}
		}
		if len(origins) > 0 {
			return origins
		}
	}
	// Fallback to ALLOWED_ORIGIN (singular, backward compatibility)
	if env := os.Getenv("ALLOWED_ORIGIN"); env != "" {
		return []string{env}
	}
	// Default: allow localhost and production
	return []string{
		"http://localhost:5173",
		"http://localhost:3000",
		"https://etigram.pro",
		"https://www.etigram.pro",
		"https://*.etigram.pro",
		"https://etigram.one",
		"https://www.etigram.one",
		"https://*.etigram.one",
	}
}

type VerifiedRequest struct {
	URL string `json:"url"`
}

type VerifiedResponse struct {
	Code   string `json:"code"`
	URL    string `json:"url"`
	Status string `json:"status"`
}

func VerifiedProxy(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	allowOrigin := ""
	
	if origin != "" {
		// Exact match against allowed origins list
		for _, ao := range allowedOrigins {
			if origin == ao {
				allowOrigin = origin
				break
			}
		}
		// Global wildcard support
		if allowOrigin == "" {
			for _, ao := range allowedOrigins {
				if ao == "*" {
					allowOrigin = origin
					break
				}
			}
		}
		// Subdomain wildcard entries like "*.etigram.one"
		if allowOrigin == "" {
			u, err := url.Parse(origin)
			if err == nil {
				host := u.Hostname()
				scheme := u.Scheme
				for _, ao := range allowedOrigins {
					if strings.HasPrefix(ao, "http://") || strings.HasPrefix(ao, "https://") {
						if au, err2 := url.Parse(ao); err2 == nil {
							ahost := au.Hostname()
							if strings.HasPrefix(ahost, "*.") {
								if strings.HasSuffix(host, strings.TrimPrefix(ahost, "*")) && au.Scheme == scheme {
									allowOrigin = origin
									break
								}
							}
						}
					} else if strings.HasPrefix(ao, "*.") {
						if strings.HasSuffix(host, strings.TrimPrefix(ao, "*")) {
							allowOrigin = origin
							break
						}
					}
				}
			}
		}
	}

	if allowOrigin != "" {
		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		w.Header().Add("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	
	// Always set these headers (even if origin not matched, for preflight)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Max-Age", "3600")

	// Handle preflight
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Only allow POST
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate environment variables
	if backendURL == "" {
		log.Printf("ERROR: BACKEND_URL not set")
		http.Error(w, "Server configuration error", http.StatusInternalServerError)
		return
	}
	if functionsKey == "" {
		log.Printf("ERROR: FUNCTIONS_KEY not set")
		http.Error(w, "Server configuration error", http.StatusInternalServerError)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("ERROR: Failed to read request body: %v", err)
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Validate JSON
	var req VerifiedRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("ERROR: Invalid JSON: %v", err)
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	// Create request to backend
	backendReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, backendURL+"/api/v1/verified", bytes.NewReader(body))
	if err != nil {
		log.Printf("ERROR: Failed to create backend request: %v", err)
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// Add system authentication header
	backendReq.Header.Set("x-functions-key", functionsKey)
	backendReq.Header.Set("Content-Type", "application/json")

	// Forward other headers (for logging/tracing)
	if userAgent := r.Header.Get("User-Agent"); userAgent != "" {
		backendReq.Header.Set("User-Agent", userAgent)
	}
	if forwardedFor := r.Header.Get("X-Forwarded-For"); forwardedFor != "" {
		backendReq.Header.Set("X-Forwarded-For", forwardedFor)
	}

	// Execute request
	client := &http.Client{
		Timeout: 60 * time.Second, // Default timeout
	}

	resp, err := client.Do(backendReq)
	if err != nil {
		log.Printf("ERROR: Backend request failed: %v", err)
		http.Error(w, "Backend service unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("ERROR: Failed to read backend response: %v", err)
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	// Forward status code and headers
	w.Header().Set("Content-Type", "application/json")
	for key, values := range resp.Header {
		// Skip headers that shouldn't be forwarded
		if key == "Content-Length" || key == "Connection" || key == "Transfer-Encoding" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

func main() {
	// Health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Verified proxy endpoint
	http.HandleFunc("/verified", VerifiedProxy)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting verified-proxy on port %s", port)
	log.Printf("Backend URL: %s", backendURL)
	log.Printf("Functions key configured: %v", functionsKey != "")
	log.Printf("Allowed origins: %v", allowedOrigins)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
