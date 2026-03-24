package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
)

var (
	projectID          = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID           = flag.String("tenant", "global", "tenant id")
	apply              = flag.Bool("apply", false, "apply writes")
	domainList         = flag.String("domains", "", "comma-separated domain filter; empty means all")
	includeDeactivates = flag.Bool("include-deactivations", true, "also deactivate known shortcut / garbage pairs")
)

type writeSummary struct {
	Synonyms    int
	Activated   int
	Deactivated int
}

func main() {
	flag.Parse()

	domains := parseDomainFilter(strings.TrimSpace(*domainList))
	for domain := range domains {
		if !isSupportedDomain(domain) {
			log.Fatalf("unsupported domain %q; supported domains: %s", domain, strings.Join(supportedDomains(), ", "))
		}
	}

	selectedPairs := make([]casebookPair, 0, len(casebookPairs))
	for _, pair := range casebookPairs {
		if domainSelected(domains, pair.Domain) {
			selectedPairs = append(selectedPairs, pair)
		}
	}
	selectedSynonyms := make([]casebookSynonym, 0, len(casebookSynonyms))
	for _, item := range casebookSynonyms {
		if domainSelected(domains, item.Domain) {
			selectedSynonyms = append(selectedSynonyms, item)
		}
	}
	selectedDeactivations := make([]casebookDeactivate, 0, len(casebookDeactivations))
	if *includeDeactivates {
		for _, item := range casebookDeactivations {
			if domainSelected(domains, item.Domain) {
				selectedDeactivations = append(selectedDeactivations, item)
			}
		}
	}

	fmt.Printf(
		"casebook seed tenant=%s apply=%t domains=%s synonyms=%d pairs=%d deactivations=%d\n",
		strings.TrimSpace(*tenantID),
		*apply,
		selectedDomainLabel(domains),
		len(selectedSynonyms),
		len(selectedPairs),
		len(selectedDeactivations),
	)
	printPlan(selectedSynonyms, selectedPairs, selectedDeactivations)
	if !*apply {
		return
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()

	db := fsClient.Client()
	dict := productdict.NewRuntime(db).WithTenantID(strings.TrimSpace(*tenantID))
	summary := map[string]*writeSummary{}

	for _, item := range selectedSynonyms {
		if err := applySynonym(ctx, db, dict, strings.TrimSpace(*tenantID), item); err != nil {
			log.Fatalf("failed to apply synonym %s => %s: %v", item.SynonymPath, item.CanonicalPath, err)
		}
		ensureSummary(summary, item.Domain).Synonyms++
	}
	for _, pair := range selectedPairs {
		if err := applyPair(ctx, db, dict, strings.TrimSpace(*tenantID), pair); err != nil {
			log.Fatalf("failed to apply pair %s -> %s: %v", pair.FromPath, pair.ToPath, err)
		}
		ensureSummary(summary, pair.Domain).Activated++
	}
	for _, item := range selectedDeactivations {
		if err := applyDeactivation(ctx, db, dict, strings.TrimSpace(*tenantID), item); err != nil {
			log.Fatalf("failed to deactivate pair %s -> %s: %v", item.FromPath, item.ToPath, err)
		}
		ensureSummary(summary, item.Domain).Deactivated++
	}

	fmt.Printf("\napply summary\n")
	for _, domain := range sortedSummaryDomains(summary) {
		stats := summary[domain]
		fmt.Printf("  - domain=%s synonyms=%d activated=%d deactivated=%d\n", domain, stats.Synonyms, stats.Activated, stats.Deactivated)
	}
}

func applyPair(ctx context.Context, db *firestore.Client, dict *productdict.Runtime, tenant string, pair casebookPair) error {
	record := map[string]interface{}{
		"tenantId":       tenant,
		"fromPath":       pair.FromPath,
		"toPath":         pair.ToPath,
		"relationKind":   strings.TrimSpace(pair.RelationKind),
		"decisionSource": "tool:horizon_seed_product_casebook",
		"reason":         strings.TrimSpace(pair.Reason),
		"casebookDomain": pair.Domain,
		"casebookChain":  pair.Chain,
		"confidence":     1.0,
		"active":         true,
		"updatedAt":      firestore.ServerTimestamp,
	}
	if _, err := db.Collection("horizon_product_transform_pairs").Doc(pairDocID(pair.FromPath, pair.ToPath)).Set(ctx, record, firestore.MergeAll); err != nil {
		return err
	}
	if err := dict.SyncTransformPairWrite(ctx, tenant, pair.FromPath, pair.ToPath, pair.RelationKind, true); err != nil {
		return err
	}
	return deactivateTransformCandidate(ctx, db, tenant, pair.FromPath, pair.ToPath, "promoted_casebook_pair")
}

func applySynonym(ctx context.Context, db *firestore.Client, dict *productdict.Runtime, tenant string, item casebookSynonym) error {
	record := map[string]interface{}{
		"tenantId":       tenant,
		"canonicalPath":  item.CanonicalPath,
		"synonymPath":    item.SynonymPath,
		"decisionSource": "tool:horizon_seed_product_casebook",
		"reason":         strings.TrimSpace(item.Reason),
		"casebookDomain": item.Domain,
		"confidence":     1.0,
		"active":         true,
		"updatedAt":      firestore.ServerTimestamp,
	}
	if _, err := db.Collection("horizon_product_synonym_pairs").Doc(synonymDocID(item.CanonicalPath, item.SynonymPath)).Set(ctx, record, firestore.MergeAll); err != nil {
		return err
	}
	return dict.SyncSynonymPairWrite(ctx, tenant, item.CanonicalPath, item.SynonymPath, true)
}

func applyDeactivation(ctx context.Context, db *firestore.Client, dict *productdict.Runtime, tenant string, item casebookDeactivate) error {
	record := map[string]interface{}{
		"tenantId":       tenant,
		"fromPath":       item.FromPath,
		"toPath":         item.ToPath,
		"decisionSource": "tool:horizon_seed_product_casebook",
		"rewriteReason":  strings.TrimSpace(item.Reason),
		"casebookDomain": item.Domain,
		"active":         false,
		"updatedAt":      firestore.ServerTimestamp,
	}
	if _, err := db.Collection("horizon_product_transform_pairs").Doc(pairDocID(item.FromPath, item.ToPath)).Set(ctx, record, firestore.MergeAll); err != nil {
		return err
	}
	if err := dict.SyncTransformPairWrite(ctx, tenant, item.FromPath, item.ToPath, "transformation", false); err != nil {
		return err
	}
	return deactivateTransformCandidate(ctx, db, tenant, item.FromPath, item.ToPath, "rejected_casebook_shortcut")
}

func deactivateTransformCandidate(ctx context.Context, db *firestore.Client, tenant string, fromPath string, toPath string, status string) error {
	doc := map[string]interface{}{
		"tenantId":  tenant,
		"fromPath":  fromPath,
		"toPath":    toPath,
		"status":    status,
		"active":    false,
		"updatedAt": firestore.ServerTimestamp,
	}
	_, err := db.Collection("horizon_product_transform_candidates").Doc(candidateDocID(tenant, fromPath, toPath)).Set(ctx, doc, firestore.MergeAll)
	return err
}

func printPlan(synonyms []casebookSynonym, pairs []casebookPair, deactivations []casebookDeactivate) {
	sort.SliceStable(synonyms, func(i, j int) bool {
		if synonyms[i].Domain != synonyms[j].Domain {
			return synonyms[i].Domain < synonyms[j].Domain
		}
		if synonyms[i].CanonicalPath != synonyms[j].CanonicalPath {
			return synonyms[i].CanonicalPath < synonyms[j].CanonicalPath
		}
		return synonyms[i].SynonymPath < synonyms[j].SynonymPath
	})
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].Domain != pairs[j].Domain {
			return pairs[i].Domain < pairs[j].Domain
		}
		if pairs[i].Chain != pairs[j].Chain {
			return pairs[i].Chain < pairs[j].Chain
		}
		if pairs[i].FromPath != pairs[j].FromPath {
			return pairs[i].FromPath < pairs[j].FromPath
		}
		return pairs[i].ToPath < pairs[j].ToPath
	})
	sort.SliceStable(deactivations, func(i, j int) bool {
		if deactivations[i].Domain != deactivations[j].Domain {
			return deactivations[i].Domain < deactivations[j].Domain
		}
		if deactivations[i].FromPath != deactivations[j].FromPath {
			return deactivations[i].FromPath < deactivations[j].FromPath
		}
		return deactivations[i].ToPath < deactivations[j].ToPath
	})

	fmt.Printf("\nsynonyms\n")
	if len(synonyms) == 0 {
		fmt.Printf("  none\n")
	} else {
		for _, item := range synonyms {
			fmt.Printf("  - domain=%s canonical=%s synonym=%s\n", item.Domain, item.CanonicalPath, item.SynonymPath)
		}
	}

	fmt.Printf("\nactivations\n")
	if len(pairs) == 0 {
		fmt.Printf("  none\n")
	} else {
		for _, pair := range pairs {
			fmt.Printf("  - domain=%s chain=%s %s -> %s kind=%s\n", pair.Domain, pair.Chain, pair.FromPath, pair.ToPath, pair.RelationKind)
		}
	}

	fmt.Printf("\ndeactivations\n")
	if len(deactivations) == 0 {
		fmt.Printf("  none\n")
		return
	}
	for _, item := range deactivations {
		fmt.Printf("  - domain=%s %s -> %s\n", item.Domain, item.FromPath, item.ToPath)
	}
}

func ensureSummary(summary map[string]*writeSummary, domain string) *writeSummary {
	if summary[domain] == nil {
		summary[domain] = &writeSummary{}
	}
	return summary[domain]
}

func sortedSummaryDomains(summary map[string]*writeSummary) []string {
	out := make([]string, 0, len(summary))
	for domain := range summary {
		out = append(out, domain)
	}
	sort.Strings(out)
	return out
}

func selectedDomainLabel(filter map[string]struct{}) string {
	if len(filter) == 0 {
		return "all"
	}
	out := make([]string, 0, len(filter))
	for domain := range filter {
		out = append(out, domain)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func pairDocID(fromPath string, toPath string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(fromPath) + "\n" + strings.TrimSpace(toPath)))
	return hex.EncodeToString(sum[:])
}

func synonymDocID(canonicalPath string, synonymPath string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(canonicalPath) + "\n" + strings.TrimSpace(synonymPath)))
	return hex.EncodeToString(sum[:])
}

func candidateDocID(tenant string, fromPath string, toPath string) string {
	sum := sha1.Sum([]byte(strings.Join([]string{strings.TrimSpace(tenant), strings.TrimSpace(fromPath), strings.TrimSpace(toPath)}, "|")))
	return "transform_candidate_" + hex.EncodeToString(sum[:10])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
