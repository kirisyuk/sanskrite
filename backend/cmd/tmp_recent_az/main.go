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
func qstr(m map[string]interface{}, key string) string { if m==nil {return ""}; if v,ok:=m[key]; ok && v!=nil { return strings.TrimSpace(fmt.Sprintf("%v", v))}; return "" }
func metric(state map[string]models.MetricValue, key string) float64 { if v,ok:=state[key]; ok { return v.Value }; return 0 }
func main(){
 ctx:=context.Background(); fs,err:=firestore.NewClient(ctx,"etigram"); if err!=nil { panic(err)}; defer fs.Close()
 docs,err:=fs.Collection("horizon_fragments").Where("tenantId","==","global").Where("type","==", string(models.FragmentTypeAffectedZone)).Documents(ctx).GetAll(); if err!=nil { panic(err)}
 cutoff:=time.Now().UTC().Add(-6*time.Hour)
 type row struct{Path,Title,URL,GeoPrecision,Cond,Reason,SourceCC,Updated,Status string; Hype float64}
 rows:=[]row{}
 for _,d := range docs { var f models.HorizonFragment; if err:=d.DataTo(&f); err!=nil { continue }; if f.UpdatedAt.Before(cutoff) { continue }
 rows=append(rows,row{Path:f.CanonicalPath,Title:qstr(f.Qualifiers,"sourceTitle"),URL:qstr(f.Qualifiers,"sourceUrl"),GeoPrecision:qstr(f.Qualifiers,"geoPrecision"),Cond:qstr(f.Qualifiers,"conditionType"),Reason:qstr(f.Qualifiers,"reason"),SourceCC:qstr(f.Qualifiers,"sourceCountryCode"),Updated:f.UpdatedAt.UTC().Format(time.RFC3339),Status:f.Status,Hype:metric(f.State,"zone_hype_score")})
 }
 sort.Slice(rows, func(i,j int) bool { return rows[i].Updated > rows[j].Updated })
 fmt.Printf("recent6h=%d\n", len(rows))
 for i,r := range rows { if i>=80 { break }; fmt.Printf("%s | hype=%.1f | %s | %s | cc=%s | %s | gp=%s\n  %s\n  %s\n", r.Updated,r.Hype,r.Status,r.Path,r.SourceCC,r.Cond,r.GeoPrecision,r.Title,r.URL) }
}
