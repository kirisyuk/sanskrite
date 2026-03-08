package main
import (
  "context"
  "fmt"
  "os"
  "sort"
  "strings"
  "github.com/igorkirisyuk/sanskrite/internal/gcp"
  h "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)
func main(){ctx:=context.Background(); project:="etigram"; if len(os.Args)>1 {project=os.Args[1]}
 fs,err:=gcp.NewFirestoreClient(ctx, project); if err!=nil {panic(err)}; defer fs.Close()
 docs,err:=fs.Client().Collection("horizon_fragments").Where("canonicalPath","==","/fac/na/general-production").Documents(ctx).GetAll(); if err!=nil {panic(err)}
 type row struct{ID,Label,Legal,Short,Home,ISO string}
 rows:=[]row{}
 for _,doc:= range docs { var frag h.HorizonFragment; if err:=doc.DataTo(&frag); err!=nil {continue}; r:=row{ID:frag.ID,Label:strings.TrimSpace(frag.Labels["en"]),Legal:strings.TrimSpace(frag.Identifiers["legalName"]),Short:strings.TrimSpace(frag.Identifiers["shortName"])}; if frag.HomeLocation!=nil {r.Home=strings.TrimSpace(frag.HomeLocation.Name); if frag.HomeLocation.ISO3166_1!=nil {r.ISO=strings.TrimSpace(*frag.HomeLocation.ISO3166_1)}}; rows=append(rows,r)}
 sort.Slice(rows, func(i,j int)bool{return rows[i].Label<rows[j].Label})
 for _,r:= range rows {fmt.Printf("%s | label=%q | legal=%q | short=%q | home=%q | iso=%q\n", r.ID, r.Label, r.Legal, r.Short, r.Home, r.ISO)}
}
