package main
import (
  "bufio"
  "context"
  "fmt"
  "os"
  "regexp"
  "strings"
  "time"

  "cloud.google.com/go/firestore"
  horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
  horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func addArtifactToLensScope(ctx context.Context, lensesRepo *horizonrepo.LensesRepository, lensID string, artifact *horizonmodels.MaterializedArtifact) error {
  artifactRef := "/artifact/" + strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/")
  if _, err := lensesRepo.AddScopeCanonicalPathAutoLimited(ctx, lensID, artifactRef, horizonmodels.MaxAutoScopeAddsPerLens); err != nil {
    return err
  }
  artKey := strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/")
  ref := horizonmodels.ScopeArtifactRef{Kind: string(artifact.Kind), DisplayName: artifact.DisplayName, NodeRefs: artifact.NodeRefs, Status: artifact.PathStatus}
  if artifact.Kind == horizonmodels.MaterializedKindSupplyChainPath {
    if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
      summary := horizonmodels.SummarizeSupplyChainPathBindings(parsed)
      ref.GeoRefs = summary.GeoRefs
      ref.FlowRefs = summary.FlowRefs
      ref.ZoneRefs = summary.ZoneRefs
      ref.AffectedZoneRefs = summary.AffectedZoneRefs
      ref.SpatialSemantics = summary.SpatialSemantics
      ref.LegCount = summary.LegCount
      ref.BindingConfidence = summary.BindingConfidence
      ref.PathStressScore = summary.PathStressScore
      ref.BottleneckLegIndex = summary.BottleneckLegIndex
      ref.RecoveryLatencyHrs = summary.RecoveryLatencyHrs
    }
  }
  return lensesRepo.UpsertScopeArtifact(ctx, lensID, artKey, ref)
}

func main(){
  ctx:=context.Background()
  c,err:=firestore.NewClient(ctx,"etigram")
  if err!=nil { panic(err) }
  defer c.Close()
  logPath := "/Users/kirisyuk/Downloads/horizon-log-Pa5qjiSQDeZyKqCnrlEb-2026-03-08T14-02-27-657Z.txt"
  lensID := "Pa5qjiSQDeZyKqCnrlEb"
  re:=regexp.MustCompile(`artifact=([A-Za-z0-9]+)`) 
  f,err:=os.Open(logPath)
  if err!=nil { panic(err) }
  defer f.Close()
  ids:=map[string]struct{}{}
  s:=bufio.NewScanner(f)
  for s.Scan(){
    m:=re.FindStringSubmatch(s.Text())
    if len(m)==2 { ids[m[1]]=struct{}{} }
  }
  if err:=s.Err(); err!=nil { panic(err) }
  ar:=horizonrepo.NewMaterializedArtifactsRepository(c)
  lr:=horizonrepo.NewLensesRepository(c)
  updated:=0
  scoped:=0
  missing:=0
  for id := range ids {
    art,err:=ar.Get(ctx,id)
    if err!=nil || art==nil { missing++; continue }
    changed:=false
    if strings.TrimSpace(art.LensID)!=lensID {
      art.LensID = lensID
      art.UpdatedAt = time.Now().UTC()
      if err:=ar.Update(ctx, art); err==nil { updated++; changed=true } else { fmt.Printf("update_failed id=%s err=%v\n", id, err); continue }
    }
    if art.Kind == horizonmodels.MaterializedKindSupplyChainPath {
      if err:=addArtifactToLensScope(ctx, lr, lensID, art); err==nil { scoped++ } else { fmt.Printf("scope_failed id=%s err=%v\n", id, err) }
    }
    if changed { fmt.Printf("updated id=%s key=%s\n", id, art.CanonicalKey) }
  }
  fmt.Printf("artifact_ids=%d updated=%d scoped=%d missing=%d\n", len(ids), updated, scoped, missing)
}
