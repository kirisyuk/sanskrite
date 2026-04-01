package main
import (
  "context"
  "encoding/json"
  "flag"
  "fmt"
  "log"
  gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
  "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)
func main(){
  path:=flag.String("path","","canonical path")
  flag.Parse()
  if *path=="" { log.Fatal("--path required") }
  ctx:=context.Background()
  c,err:=gcp.NewFirestoreClient(ctx,"nv-horizon"); if err!=nil { log.Fatal(err)}
  defer c.Close()
  r:=repo.NewHorizonFragmentsRepository(c.Client())
  f,err:=r.GetByCanonicalPath(ctx,*path); if err!=nil { log.Fatal(err)}
  b,_:=json.MarshalIndent(f,"","  ")
  fmt.Println(string(b))
}
