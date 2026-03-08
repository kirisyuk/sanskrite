package main
import (
  "context"
  "fmt"
  "sort"
  "cloud.google.com/go/firestore"
  horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)
func main(){
  ctx:=context.Background()
  c,err:=firestore.NewClient(ctx,"etigram")
  if err!=nil { panic(err) }
  defer c.Close()
  lr:=horizonrepo.NewLensesRepository(c)
  ar:=horizonrepo.NewMaterializedArtifactsRepository(c)
  lens,err:=lr.Get(ctx,"Pa5qjiSQDeZyKqCnrlEb")
  if err!=nil { panic(err) }
  fmt.Printf("lens=%s scope=%d scopeArtifacts=%d\n", lens.ID,len(lens.Scope),len(lens.ScopeArtifacts))
  keys:=make([]string,0,len(lens.ScopeArtifacts))
  for k:= range lens.ScopeArtifacts { keys=append(keys,k) }
  sort.Strings(keys)
  if len(keys)>5 { keys=keys[:5] }
  for _,k:= range keys { ref:=lens.ScopeArtifacts[k]; fmt.Printf("scopeArtifact: %s -> kind=%s legs=%d stress=%.2f display=%q\n", k, ref.Kind, ref.LegCount, ref.PathStressScore, ref.DisplayName) }
  arts,err:=ar.ListByLens(ctx,lens.ID,200)
  if err!=nil { panic(err) }
  fmt.Printf("artifacts_by_lens=%d\n", len(arts))
  count:=0
  for _,a:= range arts { if string(a.Kind)=="SUPPLY_CHAIN_PATH" { count++ } }
  fmt.Printf("supply_chain_paths_by_lens=%d\n", count)
}
