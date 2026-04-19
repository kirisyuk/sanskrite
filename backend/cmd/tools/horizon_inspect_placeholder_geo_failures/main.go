package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/geocoding"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	h "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	ah "github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

type row struct{ Type, Path, Label, Place, Query, Resolved, Formatted, Why string }

func main() {
	ctx := context.Background()
	project := flag.String("project", first(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "project")
	limit := flag.Int("limit", 30, "max rows")
	flag.Parse()
	if key := gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*project), "GOOGLE_MAPS_API_KEY", "google_maps_api_key"); key != "" && strings.TrimSpace(os.Getenv("GOOGLE_MAPS_API_KEY")) == "" {
		_ = os.Setenv("GOOGLE_MAPS_API_KEY", key)
	}
	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*project))
	if err != nil {
		panic(err)
	}
	defer fs.Close()
	cacheRepo := coreRepo.NewExternalDataCacheRepository(fs.Client())
	var geo coreontology.GeocodingClientInterface
	if gc := httphandlers.GetGoogleGeocodingClientFromEnv(); gc != nil {
		geo = geocoding.NewCachedClientWithService(geocoding.NewAdapter(gc), cacheRepo, geocoding.CacheOptionsFromEnv(), coreontology.GeocodingProviderGoogle)
	}
	iter := fs.Client().Collection("horizon_fragments").Where("tenantId", "==", "global").Documents(ctx)
	rows := make([]row, 0, *limit)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			panic(err)
		}
		var frag h.HorizonFragment
		if err := doc.DataTo(&frag); err != nil {
			continue
		}
		cp := strings.TrimSpace(frag.CanonicalPath)
		cls := canonical.ClassifyPlaceholderCanonicalPath(cp)
		if cls == "" || len(rows) >= *limit {
			if len(rows) >= *limit {
				break
			}
			continue
		}
		r := row{Type: string(frag.Class), Path: cp, Label: first(strings.TrimSpace(frag.Labels["en"]), strings.TrimSpace(frag.Identifiers["legalName"]), strings.TrimSpace(frag.Identifiers["shortName"])), Place: place(&frag), Why: string(cls)}
		switch frag.Class {
		case ah.FragmentClassSite:
			if geo != nil {
				c := clone(&frag)
				res, _ := scout.RevalidateFacilityGeo(ctx, c, geo)
				r.Query = strings.TrimSpace(res.Query)
				r.Resolved = strings.TrimSpace(res.ResolvedISO2)
				r.Formatted = strings.TrimSpace(res.FormattedAddress)
			}
		case ah.FragmentClassOrg:
			if geo != nil {
				c := clone(&frag)
				res, _ := scout.RevalidateOrgGeo(ctx, c, geo)
				r.Query = strings.TrimSpace(res.Query)
				r.Resolved = strings.TrimSpace(res.ResolvedISO2)
				r.Formatted = strings.TrimSpace(res.FormattedAddress)
			}
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
	for _, r := range rows {
		fmt.Printf("- type=%s why=%s path=%s label=%q place=%q query=%q resolved=%q formatted=%q\n", r.Type, r.Why, r.Path, r.Label, r.Place, r.Query, r.Resolved, r.Formatted)
	}
}
func first(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
func place(f *h.HorizonFragment) string {
	if f == nil || f.HomeLocation == nil {
		return ""
	}
	return strings.TrimSpace(f.HomeLocation.Name)
}
func clone(in *h.HorizonFragment) *h.HorizonFragment {
	cp := *in
	if in.HomeLocation != nil {
		l := *in.HomeLocation
		cp.HomeLocation = &l
	}
	if in.Coordinates != nil {
		c := *in.Coordinates
		cp.Coordinates = &c
	}
	if in.Labels != nil {
		cp.Labels = map[string]string{}
		for k, v := range in.Labels {
			cp.Labels[k] = v
		}
	}
	if in.Identifiers != nil {
		cp.Identifiers = map[string]string{}
		for k, v := range in.Identifiers {
			cp.Identifiers[k] = v
		}
	}
	return &cp
}
