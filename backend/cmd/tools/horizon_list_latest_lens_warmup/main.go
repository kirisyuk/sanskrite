package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type warmupRow struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenantId"`
	ProjectID        string    `json:"projectId"`
	Name             string    `json:"name"`
	Status           string    `json:"status"`
	UpdatedAt        time.Time `json:"updatedAt"`
	OriginalQuery    string    `json:"originalQuery,omitempty"`
	WarmStatus       string    `json:"warmStatus,omitempty"`
	WarmReason       string    `json:"warmReason,omitempty"`
	WarmCorpusID     string    `json:"warmCorpusId,omitempty"`
	WarmDocCount     int       `json:"warmDocCount,omitempty"`
	WarmQueuedAt     string    `json:"warmQueuedAt,omitempty"`
	WarmRunningAt    string    `json:"warmRunningAt,omitempty"`
	WarmCompletedAt  string    `json:"warmCompletedAt,omitempty"`
	WarmLastError    string    `json:"warmLastError,omitempty"`
	WarmAnswerStore  string    `json:"warmAnswerStore,omitempty"`
	WarmSubstrateHash string   `json:"warmSubstrateHash,omitempty"`
}

func main() {
	var (
		projectID string
		tenantID  string
		limit     int
	)
	flag.StringVar(&projectID, "project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon-test"), "GCP project id")
	flag.StringVar(&tenantID, "tenant", "", "optional tenant filter")
	flag.IntVar(&limit, "limit", 10, "number of latest active lenses")
	flag.Parse()

	ctx := context.Background()
	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fs.Close()

	q := fs.Client().Collection("horizon_lenses")
	if limit <= 0 {
		limit = 10
	}
	docs, err := q.OrderBy("updatedAt", firestore.Desc).Limit(limit * 8).Documents(ctx).GetAll()
	if err != nil {
		log.Fatalf("query latest lenses failed: %v", err)
	}

	rows := make([]warmupRow, 0, len(docs))
	for _, doc := range docs {
		var lens hmodels.Lens
		if err := doc.DataTo(&lens); err != nil {
			continue
		}
		if strings.TrimSpace(string(lens.Status)) != string(hmodels.LensStatusActive) {
			continue
		}
		if strings.TrimSpace(tenantID) != "" && strings.TrimSpace(lens.TenantID) != strings.TrimSpace(tenantID) {
			continue
		}
		state := internalKnowledgeState(lens.ViewConfig)
		rows = append(rows, warmupRow{
			ID:                strings.TrimSpace(lens.ID),
			TenantID:          strings.TrimSpace(lens.TenantID),
			ProjectID:         strings.TrimSpace(lens.ProjectID),
			Name:              strings.TrimSpace(lens.Name),
			Status:            string(lens.Status),
			UpdatedAt:         lens.UpdatedAt,
			OriginalQuery:     strings.TrimSpace(lens.OriginalQuery),
			WarmStatus:        anyString(state["status"]),
			WarmReason:        anyString(state["lastReason"]),
			WarmCorpusID:      anyString(state["corpusId"]),
			WarmDocCount:      anyInt(state["docCount"]),
			WarmQueuedAt:      anyString(state["queuedAt"]),
			WarmRunningAt:     anyString(state["runningAt"]),
			WarmCompletedAt:   anyString(state["completedAt"]),
			WarmLastError:     anyString(state["lastError"]),
			WarmAnswerStore:   anyString(state["answerStore"]),
			WarmSubstrateHash: anyString(state["substrateHash"]),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].UpdatedAt.After(rows[j].UpdatedAt)
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(rows)
}

func internalKnowledgeState(viewConfig map[string]any) map[string]any {
	if viewConfig == nil {
		return map[string]any{}
	}
	raw := viewConfig["internalKnowledge"]
	if state, ok := raw.(map[string]any); ok && state != nil {
		return state
	}
	return map[string]any{}
}

func anyString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func anyInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		var n int
		fmt.Sscanf(strings.TrimSpace(typed), "%d", &n)
		return n
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
