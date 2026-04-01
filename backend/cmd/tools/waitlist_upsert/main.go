// Command waitlist_upsert writes or updates a core_waitlist document (same shape as WaitlistRepository).
//
// Example:
//
//	go run ./cmd/tools/waitlist_upsert -project nv-horizon -email user@example.com -status approved
package main

import (
	"context"
	"flag"
	"log"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	projectID := flag.String("project", "nv-horizon", "GCP project ID")
	email := flag.String("email", "", "waitlist email (document id)")
	status := flag.String("status", "approved", "pending|approved|rejected")
	note := flag.String("note", "", "optional note")
	flag.Parse()

	em := strings.ToLower(strings.TrimSpace(*email))
	if em == "" {
		log.Fatal("-email is required")
	}
	st := strings.ToLower(strings.TrimSpace(*status))
	if st != "pending" && st != "approved" && st != "rejected" {
		log.Fatalf("invalid -status %q (want pending|approved|rejected)", *status)
	}

	ctx := context.Background()
	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer fs.Close()

	wl := repo.NewWaitlistRepository(fs.Client())
	var notePtr *string
	if n := strings.TrimSpace(*note); n != "" {
		notePtr = &n
	}
	entry := &models.WaitlistEntry{Email: em, Status: st, Note: notePtr}
	if err := wl.Upsert(ctx, entry); err != nil {
		log.Fatalf("upsert: %v", err)
	}
	log.Printf("ok: core_waitlist/%s status=%s", em, st)
}
