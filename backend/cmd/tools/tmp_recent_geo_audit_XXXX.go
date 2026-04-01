package main

import (
  "context"
  "fmt"
  "os"
  "sort"
  "strings"

  "cloud.google.com/go/firestore"
  "github.com/igorkirisyuk/sanskrite/internal/gcp"
  "github.com/igorkirisyuk/sanskrite/internal/geocoding"
  "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
  "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
  httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
  coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
  corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

type finding struct {
  UpdatedAt string
  Path string
  Type string
  Label string
  Query string
  Provider string
  ResolvedISO2 string
  ChangedISO2 bool
  ChangedPath bool
  ChangedCoordinates bool
  Reason string
}

func main() {
  ctx := context.Background()
  projectID := strings.TrimSpace(firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"))
  tenantID := "global"
  limit := 250

  if key := gcp.GetEnvOrSecret(ctx, projectID, "GOOGLE_MAPS_API_KEY", "google_maps_api_key"); key != "" && strings.TrimSpace(os.Getenv("GOOGLE_MAPS_API_KEY")) == "" {
    _ = os.Setenv("GOOGLE_MAPS_API_KEY", key)
  }

  fs, err := gcp.NewFirestoreClient(ctx, projectID)
  if err != nil { panic(err) }
  defer fs.Close()
  db := fs.Client()

  extRepo := corerepo.NewExternalDataCacheRepository(db)
  geocoder := combinedGeoClient(extRepo)
  if geocoder == nil { panic("geocoder not configured") }

  docs, err := db.Collection("horizon_fragments").
    Where("tenantId", "==", tenantID).
    OrderBy("updatedAt", firestore.Desc).
    Limit(limit).
    Documents(ctx).GetAll()
  if err != nil { panic(err) }

  findings := make([]finding, 0)
  scanned := 0
  for _, doc := range docs {
    var frag models.HorizonFragment
    if err := doc.DataTo(&frag); err != nil { continue }
    if strings.TrimSpace(frag.ID) == "" { frag.ID = doc.Ref.ID }
    path := strings.TrimSpace(frag.CanonicalPath)
    if frag.Type != models.FragmentTypeFacility && frag.Type != models.FragmentTypeOrganization { continue }
    if frag.Type == models.FragmentTypeOrganization && !strings.HasPrefix(path, "/org/") && !strings.HasPrefix(path, "/gov/") { continue }
    if frag.Type == models.FragmentTypeFacility && !strings.HasPrefix(path, "/fac/") { continue }
    scanned++

    clone := cloneFragment(&frag)
    if clone == nil { continue }
    if frag.Type == models.FragmentTypeFacility {
      res, err := scout.RevalidateFacilityGeo(ctx, clone, geocoder)
      if err != nil || !res.Applied { continue }
      if !(res.ChangedCoordinates || res.ChangedISO2 || res.ChangedPath) { continue }
      findings = append(findings, finding{UpdatedAt: frag.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"), Path: path, Type: string(frag.Type), Label: labelOf(&frag), Query: res.Query, Provider: res.Provider, ResolvedISO2: res.ResolvedISO2, ChangedISO2: res.ChangedISO2, ChangedPath: res.ChangedPath, ChangedCoordinates: res.ChangedCoordinates, Reason: res.Reason})
      continue
    }
    res, err := scout.RevalidateOrgGeo(ctx, clone, geocoder)
    if err != nil || !res.Applied { continue }
    if !(res.ChangedCoordinates || res.ChangedISO2 || res.ChangedPath) { continue }
    findings = append(findings, finding{UpdatedAt: frag.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"), Path: path, Type: string(frag.Type), Label: labelOf(&frag), Query: res.Query, Provider: res.Provider, ResolvedISO2: res.ResolvedISO2, ChangedISO2: res.ChangedISO2, ChangedPath: res.ChangedPath, ChangedCoordinates: res.ChangedCoordinates, Reason: res.Reason})
  }

  sort.Slice(findings, func(i, j int) bool {
    if findings[i].UpdatedAt == findings[j].UpdatedAt { return findings[i].Path < findings[j].Path }
    return findings[i].UpdatedAt > findings[j].UpdatedAt
  })

  fmt.Printf("recent_scanned=%d findings=%d limit=%d\n", scanned, len(findings), limit)
  for _, f := range findings {
    fmt.Printf("- updatedAt=%s type=%s path=%s resolved=%s coords=%t iso=%t pathfix=%t provider=%s label=%q query=%q reason=%q\n", f.UpdatedAt, f.Type, f.Path, f.ResolvedISO2, f.ChangedCoordinates, f.ChangedISO2, f.ChangedPath, f.Provider, f.Label, f.Query, f.Reason)
  }
}

func combinedGeoClient(cacheRepo *corerepo.ExternalDataCacheRepository) coreontology.GeocodingClientInterface {
  var googleClient coreontology.GeocodingClientInterface
  if gc := httphandlers.GetGoogleGeocodingClientFromEnv(); gc != nil {
    googleClient = geocoding.NewCachedClientWithService(geocoding.NewAdapter(gc), cacheRepo, geocoding.CacheOptionsFromEnv(), coreontology.GeocodingProviderGoogle)
  }
  nominatimClient := geocoding.NewCachedClientWithService(geocoding.NominatimOptionsFromEnv(), cacheRepo, geocoding.CacheOptionsFromEnv(), coreontology.GeocodingProviderNominatim)
  return geocoding.NewFallbackClient(googleClient, nominatimClient, nil)
}

func cloneFragment(in *models.HorizonFragment) *models.HorizonFragment {
  if in == nil { return nil }
  cp := *in
  if in.HomeLocation != nil { loc := *in.HomeLocation; cp.HomeLocation = &loc }
  if in.Coordinates != nil { coords := *in.Coordinates; cp.Coordinates = &coords }
  if in.Labels != nil { cp.Labels = map[string]string{}; for k, v := range in.Labels { cp.Labels[k] = v } }
  if in.Identifiers != nil { cp.Identifiers = map[string]string{}; for k, v := range in.Identifiers { cp.Identifiers[k] = v } }
  if in.Qualifiers != nil { cp.Qualifiers = map[string]interface{}{}; for k, v := range in.Qualifiers { cp.Qualifiers[k] = v } }
  return &cp
}

func labelOf(fragment *models.HorizonFragment) string {
  if fragment == nil { return "" }
  return firstNonEmpty(fragment.Labels["en"], fragment.Labels["legalName"], fragment.Labels["shortName"], fragment.Description)
}

func firstNonEmpty(values ...string) string {
  for _, v := range values { if strings.TrimSpace(v) != "" { return strings.TrimSpace(v) } }
  return ""
}
