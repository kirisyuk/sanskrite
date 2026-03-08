package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	var (
		apiURL     = flag.String("url", "", "API base URL (e.g., https://api.example.com)")
		adminToken = flag.String("token", "", "Admin job token (X-Job-Token header)")
	)
	flag.Parse()

	if *apiURL == "" {
		*apiURL = os.Getenv("API_URL")
		if *apiURL == "" {
			log.Fatal("API URL is required (--url flag or API_URL env var)")
		}
	}

	if *adminToken == "" {
		*adminToken = os.Getenv("ADMIN_JOB_TOKEN")
		if *adminToken == "" {
			log.Fatal("Admin token is required (--token flag or ADMIN_JOB_TOKEN env var)")
		}
	}

	// Remove trailing slash
	if (*apiURL)[len(*apiURL)-1] == '/' {
		*apiURL = (*apiURL)[:len(*apiURL)-1]
	}

	url := *apiURL + "/api/v1/admin/sources/coordinate"

	fmt.Printf("Testing poller endpoint: %s\n", url)
	fmt.Printf("Time: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("X-Job-Token", *adminToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	fmt.Printf("Response Status: %d %s\n", resp.StatusCode, resp.Status)
	fmt.Printf("Response Body:\n")

	// Pretty print JSON if possible
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, body, "", "  "); err == nil {
		fmt.Println(prettyJSON.String())
	} else {
		fmt.Println(string(body))
	}

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Request failed with status %d", resp.StatusCode)
	}

	fmt.Printf("\n✅ Poller test completed successfully!\n")
}
