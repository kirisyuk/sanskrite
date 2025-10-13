package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"cloud.google.com/go/firestore"
)

type rename struct{ from, to string }

func main() {
	project := flag.String("project", os.Getenv("GOOGLE_CLOUD_PROJECT"), "GCP project ID")
	commit := flag.Bool("commit", false, "Apply changes (otherwise dry-run)")
	only := flag.String("only", "", "Optional: single collection to process (e.g., core_projects)")
	flag.Parse()

	if *project == "" {
		log.Fatal("project is required (flag -project or env GOOGLE_CLOUD_PROJECT)")
	}

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, *project)
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer client.Close()

	cols := []string{
		"core_projects",
		"core_datasources",
		"core_source_profiles",
		"core_interactions",
		"core_users",
	}
	if strings.TrimSpace(*only) != "" {
		cols = []string{strings.TrimSpace(*only)}
	}

	totalDocs := 0
	changedDocs := 0
	for _, colName := range cols {
		fmt.Printf("[migrate] scanning %s\n", colName)
		docs, err := client.Collection(colName).Documents(ctx).GetAll()
		if err != nil {
			log.Printf("warn: list %s: %v", colName, err)
			continue
		}
		for _, d := range docs {
			totalDocs++
			data := d.Data()
			updates := make([]firestore.Update, 0)

			// Common fields across many collections
			updates = append(updates, detectRenames(data, nil, []rename{
				{"ID", "id"},
				{"Pk", "pk"},
				{"TenantId", "tenantId"},
				{"TenantID", "tenantId"},
				{"ProjectId", "projectId"},
				{"ProjectID", "projectId"},
				{"DisplayName", "displayName"},
				{"Description", "description"},
				{"Status", "status"},
				{"OwnerUserId", "ownerUserId"},
				{"OwnerUserID", "ownerUserId"},
				{"CreatedAt", "createdAt"},
				{"UpdatedAt", "updatedAt"},
				{"LastInteractionAt", "lastInteractionAt"},
				{"View", "view"},
				{"SchemaVersion", "schemaVersion"},
				{"CompanyRef", "companyRef"},
				{"BusinessUnitName", "businessUnitName"},
				{"OrganizationName", "organizationName"},
				{"AiGuided", "aiguided"},
			})...)

			// Collection specific tweaks
			switch colName {
			case "core_datasources":
				// Normalize details/sourceDetails
				// If Details or SourceDetails present, unify into sourceDetails
				if v, ok := data["Details"].(map[string]interface{}); ok {
					// rename nested keys
					nested := renameNested(v, []rename{
						{"URI", "uri"}, {"Uri", "uri"},
						{"RSSURL", "rssUrl"}, {"RssUrl", "rssUrl"},
						{"SearchQuery", "searchQuery"}, {"OriginalSearchQuery", "originalSearchQuery"},
						{"ChannelUsername", "channelUsername"}, {"LastProcessedMessageID", "lastProcessedMessageId"},
						{"SinceTweetID", "sinceTweetId"},
						{"OriginalFileName", "originalFileName"}, {"ContentType", "contentType"},
						{"SourceURI", "sourceUri"},
					})
					// set new field
					updates = append(updates, firestore.Update{Path: "sourceDetails", Value: nested})
					// delete old
					updates = append(updates, firestore.Update{Path: "Details", Value: firestore.Delete})
				}
				if v, ok := data["SourceDetails"].(map[string]interface{}); ok {
					nested := renameNested(v, []rename{
						{"URI", "uri"}, {"Uri", "uri"},
						{"RSSURL", "rssUrl"}, {"RssUrl", "rssUrl"},
						{"SearchQuery", "searchQuery"}, {"OriginalSearchQuery", "originalSearchQuery"},
						{"ChannelUsername", "channelUsername"}, {"LastProcessedMessageID", "lastProcessedMessageId"},
						{"SinceTweetID", "sinceTweetId"},
						{"OriginalFileName", "originalFileName"}, {"ContentType", "contentType"},
						{"SourceURI", "sourceUri"},
					})
					updates = append(updates, firestore.Update{Path: "sourceDetails", Value: nested})
					updates = append(updates, firestore.Update{Path: "SourceDetails", Value: firestore.Delete})
				}
			case "core_source_profiles":
				updates = append(updates, detectRenames(data, nil, []rename{
					{"DataSourceId", "dataSourceId"},
					{"DataSourceID", "dataSourceId"},
					{"LastAnalyzedAt", "lastAnalyzedAt"},
				})...)
				if prof, ok := data["Profile"].(map[string]interface{}); ok {
					// normalize a few known nested fields in profile
					p2 := renameNested(prof, []rename{
						{"GoalAlignmentScore", "goalAlignmentScore"},
						{"TargetAudienceCountries", "targetAudienceCountries"},
						{"CoordinatedSources", "coordinatedSources"},
						{"TrustScore", "trustScore"},
						{"TrustRationale", "trustRationale"},
						{"EditorialMethods", "editorialMethods"},
					})
					updates = append(updates, firestore.Update{Path: "profile", Value: p2})
					updates = append(updates, firestore.Update{Path: "Profile", Value: firestore.Delete})
				}
			case "core_interactions":
				updates = append(updates, detectRenames(data, nil, []rename{
					{"Kind", "kind"}, {"Content", "content"},
				})...)
			case "core_users":
				updates = append(updates, detectRenames(data, nil, []rename{
					{"Email", "email"}, {"Role", "role"}, {"Status", "status"},
					{"DisplayName", "displayName"},
					{"PreferredLanguage", "preferredLanguage"},
					{"DefaultProjectId", "defaultProjectId"}, {"DefaultProjectID", "defaultProjectId"},
					{"ProjectRoles", "projectRoles"},
					{"AssignedGoalIds", "assignedGoalIds"}, {"AssignedInitiativeIds", "assignedInitiativeIds"},
				})...)
			}

			if len(updates) == 0 {
				continue
			}

			changedDocs++
			fmt.Printf("[migrate] %s/%s -> %d updates\n", colName, d.Ref.ID, len(updates))
			if *commit {
				if _, err := d.Ref.Update(ctx, updates); err != nil {
					// If some fields don't exist for delete, fallback to Set with merge for creations, then try deletes again
					// First apply creates/sets
					creates := make(map[string]interface{})
					deletes := make([]firestore.Update, 0)
					for _, u := range updates {
						if u.Value == firestore.Delete {
							deletes = append(deletes, u)
						} else {
							creates[u.Path] = u.Value
						}
					}
					if len(creates) > 0 {
						if _, err2 := d.Ref.Set(ctx, creates, firestore.MergeAll); err2 != nil {
							log.Printf("warn: set merge %s/%s: %v", colName, d.Ref.ID, err2)
						}
					}
					if len(deletes) > 0 {
						if _, err3 := d.Ref.Update(ctx, deletes); err3 != nil {
							log.Printf("warn: delete fields %s/%s: %v", colName, d.Ref.ID, err3)
						}
					}
				}
			}
		}
	}

	fmt.Printf("[migrate] done. scanned=%d changed=%d commit=%v\n", totalDocs, changedDocs, *commit)
}

func detectRenames(data map[string]interface{}, prefix []string, pairs []rename) []firestore.Update {
	updates := make([]firestore.Update, 0)
	for _, p := range pairs {
		if _, exists := data[p.to]; exists {
			// already normalized
			if _, oldExists := data[p.from]; oldExists {
				updates = append(updates, firestore.Update{Path: joinPath(prefix, p.from), Value: firestore.Delete})
			}
			continue
		}
		if v, ok := data[p.from]; ok {
			updates = append(updates, firestore.Update{Path: joinPath(prefix, p.to), Value: v})
			updates = append(updates, firestore.Update{Path: joinPath(prefix, p.from), Value: firestore.Delete})
		}
	}
	return updates
}

func renameNested(m map[string]interface{}, pairs []rename) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	for _, p := range pairs {
		if _, exists := out[p.to]; exists {
			if _, old := out[p.from]; old {
				delete(out, p.from)
			}
			continue
		}
		if v, ok := out[p.from]; ok {
			out[p.to] = v
			delete(out, p.from)
		}
	}
	return out
}

func joinPath(prefix []string, field string) string {
	if len(prefix) == 0 {
		return field
	}
	return strings.Join(append(prefix, field), ".")
}
