package main
import (
  "context"
  "fmt"
  "cloud.google.com/go/firestore"
  horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
  horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)
func main(){
  ctx:=context.Background()
  c,err:=firestore.NewClient(ctx,"etigram")
  if err!=nil { panic(err) }
  defer c.Close()
  ar:=horizonrepo.NewMaterializedArtifactsRepository(c)
  arts,err:=ar.ListByTenantAndKind(ctx,"global",horizonmodels.MaterializedKindSupplyChainPath,50)
  if err!=nil { panic(err) }
  fmt.Printf("global_supply_chain_paths=%d\n", len(arts))
  for i,a:= range arts {
    if i>=10 { break }
    fmt.Printf("id=%s lens=%q key=%s display=%q\n", a.ID,a.LensID,a.CanonicalKey,a.DisplayName)
  }
}
