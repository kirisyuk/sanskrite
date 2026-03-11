package main

import (
  "context"
  "fmt"
  "sort"
  "strings"
  "time"

  "cloud.google.com/go/firestore"
  "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type item struct {
  Updated      time.Time
  Path         string
  Title        string
  Country      string
  GeoPrecision string
  Score        any
  Cond         string
  Sub          string
}

func qstr(f *models.HorizonFragment, key string) string {
  if f.Qualifiers == nil {
    return ""
  }
  if v, ok := f.Qualifiers[key]; ok && v != nil {
    return strings.TrimSpace(fmt.Sprint(v))
  }
  return ""
}

func main() {
  ctx := context.Background()
  db, err := firestore.NewClient(ctx, "etigram")
  if err != nil {
    panic(err)
  }
  defer db.Close()
  docs, err := db.Collection("horizon_fragments").Where("tenantId", "==", "global").Where("canonicalPath", ">=", "/zone/").Where("canonicalPath", "<", "/zone/\ufffd").Documents(ctx).GetAll()
  if err != nil {
    panic(err)
  }
  cutoff := time.Now().Add(-24 * time.Hour)
  var items []item
  for _, d := range docs {
    var f models.HorizonFragment
    if err := d.DataTo(&f); err != nil {
      continue
    }
    if f.Type != models.FragmentTypeAffectedZone || !strings.EqualFold(f.Status, "active") || f.UpdatedAt.IsZero() || f.UpdatedAt.Before(cutoff) {
      continue
    }
    score := any(nil)
    if f.Qualifiers != nil {
      score = f.Qualifiers["zoneInferenceScore"]
    }
    items = append(items, item{Updated: f.UpdatedAt, Path: f.CanonicalPath, Title: qstr(&f, "sourceTitle"), Country: qstr(&f, "countryCode"), GeoPrecision: qstr(&f, "geoPrecision"), Score: score, Cond: qstr(&f, "conditionType"), Sub: qstr(&f, "conditionSubType")})
  }
  sort.Slice(items, func(i, j int) bool { return items[i].Updated.After(items[j].Updated) })
  for _, it := range items {
    title := it.Title
    if len(title) > 160 {
      title = title[:160] + "..."
    }
    fmt.Printf("%s\t%s\tgeo=%s\tcountry=%s\tcond=%s/%s\tscore=%v\ttitle=%s\n", it.Updated.UTC().Format(time.RFC3339), it.Path, it.GeoPrecision, it.Country, it.Cond, it.Sub, it.Score, title)
  }
  fmt.Printf("total_recent=%d\n", len(items))
}
