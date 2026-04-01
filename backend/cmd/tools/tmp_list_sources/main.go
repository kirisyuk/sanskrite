package main
import (
  "context"
  "fmt"
  "log"
  "strings"
  gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
  "github.com/igorkirisyuk/sanskrite/internal/models"
  "github.com/igorkirisyuk/sanskrite/internal/repo"
)
func main(){
  ctx:=context.Background()
  c,err:=gcp.NewFirestoreClient(ctx,"nv-horizon"); if err!=nil { log.Fatal(err)}
  defer c.Close()
  r:=repo.NewDataSourcesRepository(c.Client())
  status:="active"
  items,err:=r.ListByTenant(ctx,"global",[]string{models.SourceKindRSS},&status); if err!=nil { log.Fatal(err)}
  for _,ds:= range items {
    if ds==nil {continue}
    if ds.Details.DisableLLMProcessing == nil || !*ds.Details.DisableLLMProcessing {continue}
    host:=""
    if ds.Details.RSSURL!=nil { host=*ds.Details.RSSURL }
    mode:=""
    if ds.Details.ProcessingMode!=nil { mode=strings.TrimSpace(*ds.Details.ProcessingMode) }
    fmt.Printf("%s\t%s\t%s\t%s\n", ds.ID, ds.DisplayName, mode, host)
  }
}
