package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	discoverynlp "github.com/igorkirisyuk/sanskrite/internal/discovery/nlp"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

type datasetRow struct {
	SourceID           string                 `json:"sourceId"`
	SourceHost         string                 `json:"sourceHost"`
	SourceType         string                 `json:"sourceType,omitempty"`
	CategoryHint       string                 `json:"categoryHint,omitempty"`
	Title              string                 `json:"title"`
	Summary            string                 `json:"summary,omitempty"`
	Body               string                 `json:"body,omitempty"`
	URL                string                 `json:"url,omitempty"`
	Language           string                 `json:"language,omitempty"`
	Label              string                 `json:"label"`
	ShouldCreateZone   bool                   `json:"shouldCreateZone"`
	ShouldWarmExisting bool                   `json:"shouldWarmExisting"`
	ShouldOnlySignal   bool                   `json:"shouldOnlySignal"`
	EventConfidence    float64                `json:"eventConfidence"`
	Reasons            []string               `json:"reasons,omitempty"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
}

func main() {
	projectID := flag.String("project", strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")), "GCP project id")
	tenantID := flag.String("tenant", "global", "Tenant to scan")
	perSource := flag.Int("per-source", 100, "Max documents per source")
	maxSources := flag.Int("max-sources", 0, "Max sources to scan (0 = no limit)")
	output := flag.String("output", filepath.Join("tmp", "event_gate_dataset.jsonl"), "Output JSONL path")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore: %v", err)
	}
	defer fsClient.Close()

	sourcesRepo := corerepo.NewDataSourcesRepository(fsClient.Client())
	docsRepo := corerepo.NewDocumentsRepository(fsClient.Client())
	status := "active"
	sources, err := sourcesRepo.ListByTenant(ctx, strings.TrimSpace(*tenantID), []string{coremodels.SourceKindRSS, coremodels.SourceKindHorizonFeed}, &status)
	if err != nil {
		log.Fatalf("list sources: %v", err)
	}
	if *maxSources > 0 && len(sources) > *maxSources {
		sources = sources[:*maxSources]
	}

	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		log.Fatalf("mkdir output dir: %v", err)
	}
	file, err := os.Create(*output)
	if err != nil {
		log.Fatalf("create output: %v", err)
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	defer writer.Flush()

	written := 0
	for _, source := range sources {
		if source == nil || !coremodels.IsHorizonMonitorFeedKind(source.Kind) && source.Kind != coremodels.SourceKindRSS {
			continue
		}
		docTenant := strings.TrimSpace(source.TenantID)
		docs, err := docsRepo.ListBySource(ctx, docTenant, strings.TrimSpace(source.ID), *perSource)
		if err != nil {
			log.Printf("skip source=%s list docs: %v", source.ID, err)
			continue
		}
		for _, doc := range docs {
			if doc == nil {
				continue
			}
			docInput := discoverynlp.DocumentInput{
				SourceID:     source.ID,
				SourceHost:   sourceHostFromURL(doc.URL),
				SourceURL:    doc.URL,
				Title:        strings.TrimSpace(doc.Title),
				Summary:      summarizeDoc(doc.FullText, 1800),
				Body:         strings.TrimSpace(doc.FullText),
				URL:          strings.TrimSpace(doc.URL),
				Language:     strings.TrimSpace(doc.Language),
				SourceType:   strings.TrimSpace(source.Kind),
				CategoryHint: firstNonEmptyString(pointerString(source.Details.HorizonMonitorCategory), pointerString(source.Details.Category), pointerString(source.PrimarySourceCategoryPath)),
			}
			label := discovery.BuildHorizonMonitorWeakLabel(docInput)
			if label == nil {
				continue
			}
			row := datasetRow{
				SourceID:           source.ID,
				SourceHost:         docInput.SourceHost,
				SourceType:         docInput.SourceType,
				CategoryHint:       docInput.CategoryHint,
				Title:              docInput.Title,
				Summary:            docInput.Summary,
				Body:               docInput.Body,
				URL:                docInput.URL,
				Language:           docInput.Language,
				Label:              label.EventClass,
				ShouldCreateZone:   label.ShouldCreateZone,
				ShouldWarmExisting: label.ShouldWarmExisting,
				ShouldOnlySignal:   label.ShouldOnlySignal,
				EventConfidence:    label.EventConfidence,
				Reasons:            label.Reasons,
				Metadata: map[string]interface{}{
					"rejectionReason": label.RejectionReason,
					"raw":             label.Raw,
				},
			}
			line, err := json.Marshal(row)
			if err != nil {
				log.Printf("skip doc=%s marshal: %v", doc.ID, err)
				continue
			}
			if _, err := writer.Write(append(line, '\n')); err != nil {
				log.Fatalf("write output: %v", err)
			}
			written++
		}
	}
	if err := writer.Flush(); err != nil {
		log.Fatalf("flush output: %v", err)
	}
	fmt.Printf("wrote=%d output=%s\n", written, *output)
}

func sourceHostFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(u.Hostname())), "www.")
}

func summarizeDoc(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max]
}

func pointerString(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
