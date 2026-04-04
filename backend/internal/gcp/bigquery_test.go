package gcp

import (
	"errors"
	"testing"

	"google.golang.org/api/googleapi"
)

func TestIsBigQueryNotFoundError(t *testing.T) {
	t.Run("google api 404", func(t *testing.T) {
		err := &googleapi.Error{Code: 404, Message: "Not found"}
		if !isBigQueryNotFoundError(err) {
			t.Fatalf("expected 404 googleapi error to be treated as not found")
		}
	})

	t.Run("wrapped text fallback", func(t *testing.T) {
		err := errors.New("BigQuery error: Not found: Dataset x:y")
		if !isBigQueryNotFoundError(err) {
			t.Fatalf("expected wrapped not found message to be treated as not found")
		}
	})

	t.Run("other error", func(t *testing.T) {
		err := &googleapi.Error{Code: 500, Message: "boom"}
		if isBigQueryNotFoundError(err) {
			t.Fatalf("did not expect 500 error to be treated as not found")
		}
	})
}
