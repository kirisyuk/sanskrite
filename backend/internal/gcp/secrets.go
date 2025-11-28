package gcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

// LoadSecret loads a secret from GCP Secret Manager
// Returns empty string and nil error if secret is not found (non-critical)
func LoadSecret(ctx context.Context, projectID, secretName string) (string, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create secret manager client: %w", err)
	}
	defer client.Close()

	// Try different secret name variations (GCP uses lowercase)
	secretNames := []string{
		secretName,                  // As is
		strings.ToLower(secretName), // lowercase
		strings.ToLower(strings.ReplaceAll(secretName, "_", "-")), // lowercase with dashes
	}

	for _, name := range secretNames {
		req := &secretmanagerpb.AccessSecretVersionRequest{
			Name: fmt.Sprintf("projects/%s/secrets/%s/versions/latest", projectID, name),
		}
		result, err := client.AccessSecretVersion(ctx, req)
		if err == nil && result != nil && result.Payload != nil {
			return strings.TrimSpace(string(result.Payload.Data)), nil
		}
	}

	return "", fmt.Errorf("secret %s not found in project %s", secretName, projectID)
}

// GetEnvOrSecret gets value from environment variable or loads from GCP Secret Manager
func GetEnvOrSecret(ctx context.Context, projectID, envVar, secretName string) string {
	// First check environment variable
	if val := os.Getenv(envVar); val != "" {
		return val
	}

	// Try to load from GCP Secret Manager
	if projectID != "" {
		val, err := LoadSecret(ctx, projectID, secretName)
		if err == nil && val != "" {
			return val
		}
	}

	return ""
}
