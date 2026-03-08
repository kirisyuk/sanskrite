package main
import (
  "context"
  "fmt"
  "github.com/igorkirisyuk/sanskrite/internal/gcp"
)
func main(){
  ctx:=context.Background()
  fs,err:=gcp.NewFirestoreClient(ctx,"etigram"); if err!=nil { panic(err) }
  defer fs.Close(); db:=fs.Client()
  q:=db.Collection("horizon_materialized_artifacts").Where("tenantId","==","global").Where("kind","==","SUPPLY_CHAIN_PATH").Where("lensId","==","Pa5qjiSQDeZyKqCnrlEb")
  docs,err:=q.Documents(ctx).GetAll(); if err!=nil { panic(err) }
  fmt.Printf("count=%d\n", len(docs))
  for i,doc:= range docs {
    if i>=12 { break }
    data:=doc.Data()
    fmt.Printf("id=%s key=%v display=%v pathStatus=%v\n", doc.Ref.ID, data["canonicalKey"], data["displayName"], data["pathStatus"])
    if payload,ok:=data["payload"].(map[string]interface{}); ok {
      if legs,ok:=payload["legs"].([]interface{}); ok { fmt.Printf("  legs=%d\n", len(legs)) }
      if stress,ok:=payload["stress"].(map[string]interface{}); ok { fmt.Printf("  stress=%v bottleneck=%v\n", stress["pathScore"], stress["bottleneckLegIndex"]) }
    }
  }
}
