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
  "github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
  h "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type sample struct {Type, Path, Label string}
type kv struct {Key string; Count int}

func main() {
 ctx:=context.Background()
 project:=flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project")
 tenant:=flag.String("tenant","global","tenant")
 top:=flag.Int("top",20,"top classes")
 samplesN:=flag.Int("samples",8,"samples per class")
 flag.Parse()
 fs,err:=gcp.NewFirestoreClient(ctx, strings.TrimSpace(*project)); if err!=nil { panic(err) }
 defer fs.Close()
 iter:=fs.Client().Collection("horizon_fragments").Where("tenantId","==",strings.TrimSpace(*tenant)).Documents(ctx)
 total:=0
 classCounts:=map[string]int{}
 typeCounts:=map[string]int{}
 samples:=map[string][]sample{}
 for {
  doc,err:=iter.Next(); if err==iterator.Done { break }; if err!=nil { panic(err) }
  var frag h.HorizonFragment
  if err:=doc.DataTo(&frag); err!=nil { continue }
  cp:=strings.TrimSpace(frag.CanonicalPath)
  if cp=="" { continue }
  cls:=classify(cp)
  if cls=="" { continue }
  total++
  classCounts[cls]++
  typeCounts[string(frag.Type)+"|"+cls]++
  if len(samples[cls])<*samplesN {
    samples[cls]=append(samples[cls], sample{Type:string(frag.Type), Path:cp, Label:firstNonEmpty(strings.TrimSpace(frag.Labels["en"]), strings.TrimSpace(frag.Identifiers["legalName"]), strings.TrimSpace(frag.Identifiers["shortName"]) )})
  }
 }
 fmt.Printf("tenant=%s project=%s placeholder_path_total=%d\n", strings.TrimSpace(*tenant), strings.TrimSpace(*project), total)
 fmt.Println("classes:")
 for _,row:= range topCounts(classCounts, *top) { fmt.Printf("  %s: %d\n", row.Key, row.Count) }
 fmt.Println()
 fmt.Println("types_by_class:")
 for _,row:= range topCounts(typeCounts, *top*3) { fmt.Printf("  %s: %d\n", row.Key, row.Count) }
 fmt.Println()
 keys:=make([]string,0,len(samples)); for k:= range samples { keys=append(keys,k)}; sort.Strings(keys)
 for _,k:= range keys { fmt.Printf("samples[%s]:\n", k); for _,s:= range samples[k] { fmt.Printf("  - type=%s path=%s label=%q\n", s.Type, s.Path, s.Label) }; fmt.Println() }
}

func classify(cp string) string {
 return string(canonical.ClassifyPlaceholderCanonicalPath(strings.TrimSpace(cp)))
}
func firstNonEmpty(v ...string) string { for _,s:= range v { if strings.TrimSpace(s)!="" { return strings.TrimSpace(s) } }; return "" }
func topCounts(m map[string]int, limit int) []kv { out:=make([]kv,0,len(m)); for k,v:= range m { out=append(out, kv{k,v}) }; sort.Slice(out, func(i,j int) bool { if out[i].Count==out[j].Count { return out[i].Key<out[j].Key }; return out[i].Count>out[j].Count }); if limit>0 && len(out)>limit { out=out[:limit] }; return out }
