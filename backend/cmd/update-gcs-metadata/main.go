package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/option"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID   = flag.String("project", "", "GCP Project ID")
		tenantID    = flag.String("tenant", "", "Tenant ID (optional, if not set processes all tenants)")
		credentials = flag.String("credentials", "", "Path to GCP credentials JSON file (optional)")
		dryRun      = flag.Bool("dry-run", false, "Dry run mode - only show what would be updated")
		logFilePath = flag.String("log-file", "", "Optional path to a log file (stdout is always used)")
	)
	flag.Parse()

	if *projectID == "" {
		*projectID = os.Getenv("GCP_PROJECT_ID")
		if *projectID == "" {
			*projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
		}
	}
	if *projectID == "" {
		fmt.Fprintf(os.Stderr, "ERROR: --project flag or GCP_PROJECT_ID env var is required\n")
		os.Exit(1)
	}

	// Test output
	fmt.Fprintf(os.Stdout, "=== GCS Metadata Update Script ===\n")
	fmt.Fprintf(os.Stdout, "Project: %s\n", *projectID)
	fmt.Fprintf(os.Stdout, "Dry Run: %v\n\n", *dryRun)
	os.Stdout.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	var mw io.Writer = os.Stdout
	if *logFilePath != "" {
		logFile, err := os.OpenFile(*logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open log file %q: %v\n", *logFilePath, err)
		} else {
			defer logFile.Close()
			mw = io.MultiWriter(os.Stdout, logFile)
		}
	}
	log.SetOutput(mw)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Print startup info
	fmt.Fprintf(mw, "=== GCS Metadata Update Script ===\n")
	fmt.Fprintf(mw, "Project ID: %s\n", *projectID)
	fmt.Fprintf(mw, "Dry Run: %v\n", *dryRun)
	if *tenantID != "" {
		fmt.Fprintf(mw, "Tenant ID: %s\n", *tenantID)
	}
	fmt.Fprintf(mw, "\n")

	log.Printf("Starting metadata update script...")
	log.Printf("Project ID: %s", *projectID)

	// Initialize Firestore client
	var opts []option.ClientOption
	if *credentials != "" {
		opts = append(opts, option.WithCredentialsFile(*credentials))
		log.Printf("Using credentials file: %s", *credentials)
	} else {
		log.Printf("Using Application Default Credentials")
	}

	log.Printf("Connecting to Firestore...")
	fsClient, err := firestore.NewClient(ctx, *projectID, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to create Firestore client: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: Run 'gcloud auth application-default login --project=%s'\n", *projectID)
		os.Exit(1)
	}
	defer fsClient.Close()
	log.Printf("✓ Firestore client created successfully")

	// Initialize GCS client
	log.Printf("Connecting to GCS...")
	gcsClient, err := gcp.NewGCSClient(ctx, *projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to create GCS client: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: Run 'gcloud auth application-default login --project=%s'\n", *projectID)
		os.Exit(1)
	}
	log.Printf("✓ GCS client created successfully")

	// Get bucket names
	coreDocsBucket := os.Getenv("CORE_DOCS_BUCKET")
	if coreDocsBucket == "" {
		coreDocsBucket = fmt.Sprintf("%s-core-docs", *projectID)
	}
	ragDocsBucket := os.Getenv("RAG_DOCUMENTS_BUCKET")
	if ragDocsBucket == "" {
		ragDocsBucket = fmt.Sprintf("%s-rag-documents", *projectID)
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("Project: %s", *projectID)
	log.Printf("Core Docs Bucket: %s", coreDocsBucket)
	log.Printf("RAG Documents Bucket: %s", ragDocsBucket)
	if *dryRun {
		log.Printf("DRY RUN MODE - no changes will be made")
	}

	// Get all tenants
	var tenants []string
	if *tenantID != "" {
		tenants = []string{*tenantID}
		log.Printf("Processing single tenant: %s", *tenantID)
	} else {
		log.Printf("Discovering all tenants...")
		tenants = getAllTenants(ctx, fsClient)
		log.Printf("Found %d tenants", len(tenants))
		if len(tenants) == 0 {
			log.Printf("WARNING: No tenants found. Check Firestore collections.")
			return
		}
	}

	// Initialize repositories (we'll query Firestore directly)
	docsRepo := repo.NewDocumentsRepository(fsClient)

	totalUpdated := 0
	totalSkipped := 0
	totalErrors := 0

	// Process CoreDocs
	log.Printf("\n=== Processing CoreDocs ===")
	for _, tenantID := range tenants {
		log.Printf("\nProcessing tenant: %s", tenantID)
		updated, skipped, errors := updateCoreDocsMetadata(ctx, fsClient, gcsClient, coreDocsBucket, tenantID, *dryRun)
		totalUpdated += updated
		totalSkipped += skipped
		totalErrors += errors
	}

	// Process Documents (RAG documents)
	log.Printf("\n=== Processing RAG Documents ===")
	for _, tenantID := range tenants {
		log.Printf("\nProcessing tenant: %s", tenantID)
		updated, skipped, errors := updateDocumentsMetadata(ctx, docsRepo, gcsClient, ragDocsBucket, tenantID, *dryRun)
		totalUpdated += updated
		totalSkipped += skipped
		totalErrors += errors
	}

	fmt.Fprintf(os.Stdout, "\n=== Summary ===\n")
	fmt.Fprintf(os.Stdout, "Updated: %d\n", totalUpdated)
	fmt.Fprintf(os.Stdout, "Skipped: %d\n", totalSkipped)
	fmt.Fprintf(os.Stdout, "Errors: %d\n", totalErrors)

	log.Printf("\n=== Summary ===")
	log.Printf("Updated: %d", totalUpdated)
	log.Printf("Skipped: %d", totalSkipped)
	log.Printf("Errors: %d", totalErrors)

	if totalErrors > 0 {
		os.Exit(1)
	}
}

func getAllTenants(ctx context.Context, fsClient *firestore.Client) []string {
	tenantMap := make(map[string]bool)

	// Get tenants from projects collection
	log.Printf("Querying projects collection...")
	projectsCol := fsClient.Collection("projects")
	projectDocs, err := projectsCol.Documents(ctx).GetAll()
	if err != nil {
		log.Printf("Warning: failed to query projects collection: %v", err)
	} else {
		log.Printf("Found %d projects", len(projectDocs))
		for _, doc := range projectDocs {
			var p struct {
				TenantID string `firestore:"tenantId"`
			}
			if err := doc.DataTo(&p); err == nil && p.TenantID != "" {
				tenantMap[p.TenantID] = true
			}
		}
	}

	// Get tenants from core_docs collection
	log.Printf("Querying core_docs collection...")
	coreDocsCol := fsClient.Collection("core_docs")
	coreDocs, err := coreDocsCol.Documents(ctx).GetAll()
	if err != nil {
		log.Printf("Warning: failed to query core_docs collection: %v", err)
	} else {
		log.Printf("Found %d core_docs", len(coreDocs))
		for _, doc := range coreDocs {
			var cd struct {
				TenantID string `firestore:"tenantId"`
			}
			if err := doc.DataTo(&cd); err == nil && cd.TenantID != "" {
				tenantMap[cd.TenantID] = true
			}
		}
	}

	// Get tenants from rag_documents collection
	log.Printf("Querying rag_documents collection...")
	ragDocsCol := fsClient.Collection("rag_documents")
	ragDocs, err := ragDocsCol.Documents(ctx).GetAll()
	if err != nil {
		log.Printf("Warning: failed to query rag_documents collection: %v", err)
	} else {
		log.Printf("Found %d rag_documents", len(ragDocs))
		for _, doc := range ragDocs {
			var d struct {
				TenantID string `firestore:"tenantId"`
			}
			if err := doc.DataTo(&d); err == nil && d.TenantID != "" {
				tenantMap[d.TenantID] = true
			}
		}
	}

	tenants := make([]string, 0, len(tenantMap))
	for tid := range tenantMap {
		tenants = append(tenants, tid)
	}
	log.Printf("Total unique tenants found: %d", len(tenants))
	return tenants
}

func updateCoreDocsMetadata(ctx context.Context, fsClient *firestore.Client, gcsClient *gcp.GCSClient, bucket, tenantID string, dryRun bool) (updated, skipped, errors int) {
	// Get all CoreDocs for tenant - query directly from Firestore collection
	log.Printf("Querying CoreDocs for tenant %s...", tenantID)
	col := fsClient.Collection("core_docs")
	docs, err := col.Where("tenantId", "==", tenantID).Documents(ctx).GetAll()
	if err != nil {
		log.Printf("Error querying CoreDocs for tenant %s: %v", tenantID, err)
		return 0, 0, 1
	}

	log.Printf("Found %d CoreDocs for tenant %s", len(docs), tenantID)

	if len(docs) == 0 {
		log.Printf("No CoreDocs found for tenant %s, skipping", tenantID)
		return 0, 0, 0
	}

	for _, docSnap := range docs {
		var cd models.CoreDoc
		if err := docSnap.DataTo(&cd); err != nil {
			log.Printf("Error parsing CoreDoc %s: %v", docSnap.Ref.ID, err)
			errors++
			continue
		}

		if cd.StoragePath == nil || *cd.StoragePath == "" {
			skipped++
			continue
		}

		// Extract bucket and object from gs:// path
		gsPath := *cd.StoragePath
		if !strings.HasPrefix(gsPath, "gs://") {
			skipped++
			continue
		}

		pathParts := strings.TrimPrefix(gsPath, "gs://")
		parts := strings.SplitN(pathParts, "/", 2)
		if len(parts) != 2 {
			skipped++
			continue
		}

		objectBucket := parts[0]
		objectPath := parts[1]

		// Only process if it's in the core-docs bucket
		if objectBucket != bucket {
			skipped++
			continue
		}

		// Get current metadata (idempotent: safe to retry if object doesn't exist)
		currentMeta, err := gcsClient.GetMetadata(ctx, bucket, objectPath)
		if err != nil {
			// Object might not exist in GCS (deleted or never uploaded)
			log.Printf("Warning: could not get metadata for %s (object may not exist): %v", objectPath, err)
			skipped++
			continue
		}

		// Prepare new metadata (copy existing if present) - idempotent operation
		newMeta := make(map[string]string)
		for k, v := range currentMeta {
			newMeta[k] = v
		}

		needsUpdate := false

		// Add/update tenantId (idempotent: safe to update multiple times)
		if newMeta["tenantId"] != cd.TenantID {
			newMeta["tenantId"] = cd.TenantID
			needsUpdate = true
		}

		// Add/update projectId (idempotent: safe to update multiple times)
		if cd.ProjectID != "" {
			if newMeta["projectId"] != cd.ProjectID {
				newMeta["projectId"] = cd.ProjectID
				needsUpdate = true
			}
		}

		// Add filename if missing (idempotent: only adds if missing)
		if cd.Filename != "" && newMeta["filename"] == "" {
			newMeta["filename"] = cd.Filename
			needsUpdate = true
		}

		if needsUpdate {
			if dryRun {
				log.Printf("  [DRY RUN] Would update %s: tenantId=%s, projectId=%s", objectPath, cd.TenantID, cd.ProjectID)
				updated++
			} else {
				// Idempotent: update metadata (safe to run multiple times)
				if err := gcsClient.UpdateMetadata(ctx, bucket, objectPath, newMeta); err != nil {
					log.Printf("Error updating metadata for %s: %v", objectPath, err)
					errors++
				} else {
					log.Printf("  Updated %s: tenantId=%s, projectId=%s", objectPath, cd.TenantID, cd.ProjectID)
					updated++
				}
			}
		} else {
			// Already has correct metadata - skip (idempotent)
			skipped++
		}

		// Small delay to avoid rate limits
		time.Sleep(50 * time.Millisecond)
	}

	return updated, skipped, errors
}

func updateDocumentsMetadata(ctx context.Context, docsRepo *repo.DocumentsRepository, gcsClient *gcp.GCSClient, bucket, tenantID string, dryRun bool) (updated, skipped, errors int) {
	// Get all Documents for tenant
	log.Printf("Querying Documents for tenant %s...", tenantID)
	col := docsRepo.Collection()
	docs, err := col.Where("tenantId", "==", tenantID).Documents(ctx).GetAll()
	if err != nil {
		log.Printf("Error querying Documents for tenant %s: %v", tenantID, err)
		return 0, 0, 1
	}

	log.Printf("Found %d Documents for tenant %s", len(docs), tenantID)

	if len(docs) == 0 {
		log.Printf("No Documents found for tenant %s, skipping", tenantID)
		return 0, 0, 0
	}

	for _, docSnap := range docs {
		var d models.Document
		if err := docSnap.DataTo(&d); err != nil {
			log.Printf("Error parsing Document %s: %v", docSnap.Ref.ID, err)
			errors++
			continue
		}

		if d.StoragePath == nil || *d.StoragePath == "" {
			skipped++
			continue
		}

		// Extract bucket and object from gs:// path
		gsPath := *d.StoragePath
		if !strings.HasPrefix(gsPath, "gs://") {
			skipped++
			continue
		}

		pathParts := strings.TrimPrefix(gsPath, "gs://")
		parts := strings.SplitN(pathParts, "/", 2)
		if len(parts) != 2 {
			skipped++
			continue
		}

		objectBucket := parts[0]
		objectPath := parts[1]

		// Only process if it's in the rag-documents bucket
		if objectBucket != bucket {
			skipped++
			continue
		}

		// Get current metadata (idempotent: safe to retry if object doesn't exist)
		currentMeta, err := gcsClient.GetMetadata(ctx, bucket, objectPath)
		if err != nil {
			// Object might not exist in GCS (deleted or never uploaded)
			log.Printf("Warning: could not get metadata for %s (object may not exist): %v", objectPath, err)
			skipped++
			continue
		}

		// Prepare new metadata (copy existing if present) - idempotent operation
		newMeta := make(map[string]string)
		for k, v := range currentMeta {
			newMeta[k] = v
		}

		needsUpdate := false

		// Add/update tenantId (idempotent: safe to update multiple times)
		if newMeta["tenantId"] != d.TenantID {
			newMeta["tenantId"] = d.TenantID
			needsUpdate = true
		}

		// Add/update projectId from SourceID or Attrs (idempotent)
		projectID := ""
		if d.SourceID != "" && strings.HasPrefix(d.SourceID, "project:") {
			projectID = strings.TrimPrefix(d.SourceID, "project:")
		} else if d.Attrs != nil {
			if pid, ok := d.Attrs["projectId"].(string); ok && pid != "" {
				projectID = pid
			}
		}

		if projectID != "" {
			if newMeta["projectId"] != projectID {
				newMeta["projectId"] = projectID
				needsUpdate = true
			}
		}

		// Add sourceId if missing (idempotent: only adds if missing)
		if d.SourceID != "" && newMeta["sourceId"] == "" {
			newMeta["sourceId"] = d.SourceID
			needsUpdate = true
		}

		// Add documentId if missing (idempotent: only adds if missing)
		if d.ID != "" && newMeta["documentId"] == "" {
			newMeta["documentId"] = d.ID
			needsUpdate = true
		}

		// Add title if missing (idempotent: only adds if missing)
		if d.Title != "" && newMeta["title"] == "" {
			newMeta["title"] = d.Title
			needsUpdate = true
		}

		if needsUpdate {
			if dryRun {
				log.Printf("  [DRY RUN] Would update %s: tenantId=%s, projectId=%s", objectPath, d.TenantID, projectID)
				updated++
			} else {
				// Idempotent: update metadata (safe to run multiple times)
				if err := gcsClient.UpdateMetadata(ctx, bucket, objectPath, newMeta); err != nil {
					log.Printf("Error updating metadata for %s: %v", objectPath, err)
					errors++
				} else {
					log.Printf("  Updated %s: tenantId=%s, projectId=%s", objectPath, d.TenantID, projectID)
					updated++
				}
			}
		} else {
			// Already has correct metadata - skip (idempotent)
			skipped++
		}

		// Small delay to avoid rate limits
		time.Sleep(50 * time.Millisecond)
	}

	return updated, skipped, errors
}




