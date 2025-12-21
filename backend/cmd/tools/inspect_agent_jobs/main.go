package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
)

type jobLite struct {
	ID        string                 `firestore:"id"`
	AgentID   string                 `firestore:"agentId"`
	ProjectID string                 `firestore:"projectId"`
	Status    string                 `firestore:"status"`
	State     map[string]interface{} `firestore:"state"`
	CreatedAt time.Time              `firestore:"createdAt"`
	UpdatedAt time.Time              `firestore:"updatedAt"`
}

type bucketKey struct {
	Agent  string
	Status string
}

func main() {
	ctx := context.Background()

	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id (defaults to $GCP_PROJECT_ID)")
	status := flag.String("status", "pending", "status filter: pending|running|completed|failed|partial|all")
	agent := flag.String("agent", "scout", "agentId filter (use 'all' for no filter)")
	limit := flag.Int("limit", 500, "max documents to fetch (default 500)")
	freshness := flag.Duration("freshness", 30*24*time.Hour, "only include jobs created within this window (default 30d)")
	flag.Parse()

	if *projectID == "" {
		fmt.Fprintln(os.Stderr, "--project is required (or set GCP_PROJECT_ID)")
		os.Exit(2)
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "firestore init failed: %v\n", err)
		os.Exit(1)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	q := db.Collection("agent_jobs").Query
	if strings.ToLower(strings.TrimSpace(*agent)) != "all" && strings.TrimSpace(*agent) != "" {
		q = q.Where("agentId", "==", strings.TrimSpace(*agent))
	}
	switch strings.ToLower(strings.TrimSpace(*status)) {
	case "all":
		// no status filter
	default:
		q = q.Where("status", "==", strings.TrimSpace(*status))
	}
	if *freshness > 0 {
		cutoff := time.Now().UTC().Add(-*freshness)
		q = q.Where("createdAt", ">=", cutoff)
	}
	// Ordering:
	// - When agentId is specified (not "all"), we can safely OrderBy(createdAt ASC) using the existing composite index:
	//   (agentId ASC, status ASC, createdAt ASC).
	// - When agentId is "all", ordering + filters tends to require extra composite indexes; keep it simple and unordered.
	if strings.ToLower(strings.TrimSpace(*agent)) != "all" && strings.TrimSpace(*agent) != "" {
		q = q.OrderBy("createdAt", firestore.Asc)
	}
	if *limit > 0 {
		q = q.Limit(*limit)
	}

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "query failed: %v\n", err)
		os.Exit(1)
	}

	now := time.Now().UTC()
	total := 0
	counts := map[bucketKey]int{}
	targetCounts := map[string]int{}
	var ages []time.Duration
	var oldest, newest *jobLite

	for _, d := range docs {
		var j jobLite
		if err := d.DataTo(&j); err != nil {
			continue
		}
		j.ID = d.Ref.ID
		total++

		k := bucketKey{Agent: strings.TrimSpace(j.AgentID), Status: strings.TrimSpace(strings.ToLower(j.Status))}
		counts[k]++

		age := now.Sub(j.CreatedAt)
		ages = append(ages, age)

		if oldest == nil || j.CreatedAt.Before(oldest.CreatedAt) {
			cp := extractTargetCanonicalPath(j.State)
			tmp := j
			if tmp.State == nil {
				tmp.State = map[string]interface{}{"targetCanonicalPath": cp}
			} else {
				tmp.State["targetCanonicalPath"] = cp
			}
			oldest = &tmp
		}
		if newest == nil || j.CreatedAt.After(newest.CreatedAt) {
			cp := extractTargetCanonicalPath(j.State)
			tmp := j
			if tmp.State == nil {
				tmp.State = map[string]interface{}{"targetCanonicalPath": cp}
			} else {
				tmp.State["targetCanonicalPath"] = cp
			}
			newest = &tmp
		}

		if cp := extractTargetCanonicalPath(j.State); cp != "" {
			targetCounts[cp]++
		}
	}

	fmt.Printf("agent_jobs sample: fetched=%d filter(agent=%q status=%q freshness=%s)\n", total, *agent, *status, freshness.String())
	if total == 0 {
		return
	}

	// Age percentiles (on sample)
	sort.Slice(ages, func(i, j int) bool { return ages[i] < ages[j] })
	fmt.Printf("age(sample): min=%s p50=%s p90=%s p99=%s max=%s\n",
		ages[0].Truncate(time.Second),
		pctl(ages, 0.50).Truncate(time.Second),
		pctl(ages, 0.90).Truncate(time.Second),
		pctl(ages, 0.99).Truncate(time.Second),
		ages[len(ages)-1].Truncate(time.Second),
	)

	// Counts by (agent,status)
	type kv struct {
		K bucketKey
		V int
	}
	var buckets []kv
	for k, v := range counts {
		buckets = append(buckets, kv{K: k, V: v})
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].V > buckets[j].V })
	fmt.Println("counts(sample) by (agent,status):")
	for _, b := range buckets {
		fmt.Printf("  - %s/%s: %d\n", b.K.Agent, b.K.Status, b.V)
	}

	// Top targets
	type tk struct {
		Target string
		N      int
	}
	var tops []tk
	for t, n := range targetCounts {
		tops = append(tops, tk{Target: t, N: n})
	}
	sort.Slice(tops, func(i, j int) bool { return tops[i].N > tops[j].N })
	fmt.Println("top targets(sample):")
	for i := 0; i < len(tops) && i < 15; i++ {
		fmt.Printf("  - %s: %d\n", tops[i].Target, tops[i].N)
	}

	if oldest != nil {
		fmt.Printf("oldest(sample): id=%s agent=%s status=%s createdAt=%s target=%s\n",
			oldest.ID, oldest.AgentID, oldest.Status, oldest.CreatedAt.Format(time.RFC3339), extractTargetCanonicalPath(oldest.State))
	}
	if newest != nil {
		fmt.Printf("newest(sample): id=%s agent=%s status=%s createdAt=%s target=%s\n",
			newest.ID, newest.AgentID, newest.Status, newest.CreatedAt.Format(time.RFC3339), extractTargetCanonicalPath(newest.State))
	}
}

func extractTargetCanonicalPath(state map[string]interface{}) string {
	if state == nil {
		return ""
	}
	if v, ok := state["targetCanonicalPath"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func pctl(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
