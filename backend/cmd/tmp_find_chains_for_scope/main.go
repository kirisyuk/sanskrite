package main
import (
  "context"
  "fmt"
  "sort"
  "cloud.google.com/go/firestore"
  horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
  horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)
func main(){
 ctx:=context.Background(); c,err:=firestore.NewClient(ctx,"etigram"); if err!=nil { panic(err) }; defer c.Close()
 ar:=horizonrepo.NewMaterializedArtifactsRepository(c)
 paths:=[]string{"/fac/be/zeebrugge-lng-terminal","/fac/gb/south-hook-lng-terminal","/loc/qa","/market/global/energy/lng","/org/qa/qatarenergy","/product/raw/hydrocarbon/liquefaction/lng"}
 seen:=map[string]bool{}
 keys:=[]string{}
 for _,p:= range paths {
   arts,err:=ar.FindByNodeRef(ctx,"global",p,horizonmodels.MaterializedKindSupplyChainPath); if err!=nil { panic(err) }
   fmt.Printf("path=%s chains=%d\n", p, len(arts))
   for _,a:= range arts {
     if !seen[a.ID] { seen[a.ID]=true; keys=append(keys,a.ID+"|"+a.CanonicalKey+"|"+a.DisplayName) }
   }
 }
 sort.Strings(keys)
 fmt.Printf("unique=%d\n", len(keys))
 for i,k:= range keys { if i>=20 { break }; fmt.Println(k) }
}
