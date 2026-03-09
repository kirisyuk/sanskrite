package main

import (
	"context"
	"flag"
	"log"
	"os"
	"reflect"
	"strings"
	"time"

	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stats struct {
	scanned          int
	sourcesUpdated   int
	runtimeCreated   int
	runtimeUpdated   int
	runtimeUnchanged int
}

func main() {
	projectID := flag.String("project", "", "GCP project id")
	apply := flag.Bool("apply", false, "Apply changes to Firestore")
	limit := flag.Int("limit", 0, "Limit number of sources to process")
	sourceID := flag.String("source-id", "", "Only process a single source id")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		*projectID = strings.TrimSpace(firstNonEmptyEnv("GCP_PROJECT_ID", "GOOGLE_CLOUD_PROJECT"))
	}
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	ctx := context.Background()
	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer fs.Close()

	dsRepo := repo.NewDataSourcesRepository(fs.Client())
	runtimeRepo := repo.NewSourceRuntimeStatesRepository(fs.Client())

	docs, err := fs.Client().Collection("core_datasources").Documents(ctx).GetAll()
	if err != nil {
		log.Fatalf("list core_datasources: %v", err)
	}

	st := stats{}
	now := time.Now().UTC()
	for _, doc := range docs {
		var ds models.DataSource
		if err := doc.DataTo(&ds); err != nil {
			log.Printf("skip malformed datasource %s: %v", doc.Ref.ID, err)
			continue
		}
		if strings.TrimSpace(ds.ID) == "" {
			ds.ID = doc.Ref.ID
		}
		if strings.TrimSpace(*sourceID) != "" && ds.ID != strings.TrimSpace(*sourceID) {
			continue
		}
		if *limit > 0 && st.scanned >= *limit {
			break
		}
		st.scanned++

		before := ds
		ds.SyncCompatForWrite()
		if !reflect.DeepEqual(before, ds) {
			if *apply {
				if err := dsRepo.Upsert(ctx, &ds); err != nil {
					log.Fatalf("upsert datasource %s: %v", ds.ID, err)
				}
			}
			st.sourcesUpdated++
		}

		existingRuntime, err := runtimeRepo.Get(ctx, ds.ID)
		if err != nil && status.Code(err) != codes.NotFound {
			log.Fatalf("get runtime state %s: %v", ds.ID, err)
		}
		if status.Code(err) == codes.NotFound {
			existingRuntime = nil
		}
		runtimeState := buildRuntimeState(existingRuntime, &ds, now)
		if existingRuntime == nil {
			if *apply {
				if err := runtimeRepo.Upsert(ctx, runtimeState); err != nil {
					log.Fatalf("upsert runtime state %s: %v", ds.ID, err)
				}
			}
			st.runtimeCreated++
			continue
		}
		if !reflect.DeepEqual(*existingRuntime, *runtimeState) {
			if *apply {
				if err := runtimeRepo.Upsert(ctx, runtimeState); err != nil {
					log.Fatalf("update runtime state %s: %v", ds.ID, err)
				}
			}
			st.runtimeUpdated++
		} else {
			st.runtimeUnchanged++
		}
	}

	mode := "dry-run"
	if *apply {
		mode = "apply"
	}
	log.Printf("[%s] scanned=%d sourcesUpdated=%d runtimeCreated=%d runtimeUpdated=%d runtimeUnchanged=%d",
		mode, st.scanned, st.sourcesUpdated, st.runtimeCreated, st.runtimeUpdated, st.runtimeUnchanged)
}

func buildRuntimeState(existing *models.SourceRuntimeState, ds *models.DataSource, now time.Time) *models.SourceRuntimeState {
	state := &models.SourceRuntimeState{
		ID:                ds.ID,
		SourceID:          ds.ID,
		TenantID:          ds.TenantID,
		Kind:              ds.Kind,
		Scope:             models.SourceScopeValue(ds),
		ProcessingMode:    models.SourceProcessingModeValue(ds),
		OutputMode:        models.SourceOutputModeValue(ds),
		Status:            ds.Status,
		LastPolledAt:      cloneTimePtr(ds.LastPolledAt),
		LastFoundAt:       cloneTimePtr(ds.LastFoundAt),
		NextPollAt:        cloneTimePtr(ds.NextPollAt),
		PauseReason:       cloneStringPtr(ds.PauseReason),
		PausedAt:          cloneTimePtr(ds.PausedAt),
		PausedUntil:       cloneTimePtr(ds.PausedUntil),
		PollBackoffSec:    cloneIntPtr(ds.PollBackoffSec),
		ConsecutiveErrors: ds.ErrorCount,
		LastStatus:        stringPtr(strings.TrimSpace(ds.Status)),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if ds.ErrorCount == 0 {
		state.LastSuccessAt = cloneTimePtr(ds.LastPolledAt)
	} else {
		state.LastErrorAt = cloneTimePtr(ds.LastPolledAt)
	}
	if state.NextPollAt == nil {
		next := now
		state.NextPollAt = &next
	}

	if existing != nil {
		state.CreatedAt = existing.CreatedAt
		if state.CreatedAt.IsZero() {
			state.CreatedAt = now
		}
		state.UpdatedAt = existing.UpdatedAt
		if state.UpdatedAt.IsZero() {
			state.UpdatedAt = now
		}
		state.LastPolledAt = coalesceTimePtr(existing.LastPolledAt, state.LastPolledAt)
		state.LastFoundAt = coalesceTimePtr(existing.LastFoundAt, state.LastFoundAt)
		state.LastSuccessAt = coalesceTimePtr(existing.LastSuccessAt, state.LastSuccessAt)
		state.LastErrorAt = coalesceTimePtr(existing.LastErrorAt, state.LastErrorAt)
		state.LastCoordinatedAt = cloneTimePtr(existing.LastCoordinatedAt)
		state.LastEnqueuedAt = cloneTimePtr(existing.LastEnqueuedAt)
		state.NextPollAt = coalesceTimePtr(existing.NextPollAt, state.NextPollAt)
		state.LeaseOwner = cloneStringPtr(existing.LeaseOwner)
		state.LeaseUntil = cloneTimePtr(existing.LeaseUntil)
		state.Status = coalesceString(existing.Status, state.Status)
		state.PauseReason = coalesceStringPtr(existing.PauseReason, state.PauseReason)
		state.PausedAt = coalesceTimePtr(existing.PausedAt, state.PausedAt)
		state.PausedUntil = coalesceTimePtr(existing.PausedUntil, state.PausedUntil)
		state.ConsecutiveEmptyPolls = existing.ConsecutiveEmptyPolls
		if state.ConsecutiveErrors == 0 {
			state.ConsecutiveErrors = existing.ConsecutiveErrors
		}
		state.LastStatus = coalesceStringPtr(existing.LastStatus, state.LastStatus)
		state.LastError = cloneStringPtr(existing.LastError)
		state.LastYield = cloneFloatPtr(existing.LastYield)
		state.AvgYield = cloneFloatPtr(existing.AvgYield)
		state.PollBackoffSec = coalesceIntPtr(existing.PollBackoffSec, state.PollBackoffSec)
		state.TotalRuns = existing.TotalRuns
		state.TotalDocumentsProcessed = existing.TotalDocumentsProcessed
		state.TotalDocumentsCreated = existing.TotalDocumentsCreated
	}
	return state
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func cloneTimePtr(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}
	v := in.UTC()
	return &v
}

func cloneIntPtr(in *int) *int {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneStringPtr(in *string) *string {
	if in == nil {
		return nil
	}
	v := strings.TrimSpace(*in)
	if v == "" {
		return nil
	}
	return &v
}

func cloneFloatPtr(in *float64) *float64 {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func coalesceTimePtr(primary, fallback *time.Time) *time.Time {
	if primary != nil {
		return cloneTimePtr(primary)
	}
	return cloneTimePtr(fallback)
}

func coalesceStringPtr(primary, fallback *string) *string {
	if primary != nil && strings.TrimSpace(*primary) != "" {
		return cloneStringPtr(primary)
	}
	return cloneStringPtr(fallback)
}

func coalesceIntPtr(primary, fallback *int) *int {
	if primary != nil {
		return cloneIntPtr(primary)
	}
	return cloneIntPtr(fallback)
}

func stringPtr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func coalesceString(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}
