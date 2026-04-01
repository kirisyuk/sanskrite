package main
import (
  "context"
  "fmt"
  "log"
  "os"
  "strings"
  "github.com/igorkirisyuk/sanskrite/internal/ai"
  "github.com/igorkirisyuk/sanskrite/internal/crawler"
  "github.com/igorkirisyuk/sanskrite/internal/gcp"
  "github.com/igorkirisyuk/sanskrite/internal/horizon"
  horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
  corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
  realtime "github.com/igorkirisyuk/sanskrite/internal/realtime"
)
func main(){
  ctx:=context.Background()
  projectID:="nv-horizon-test"
  fs,err:=gcp.NewFirestoreClient(ctx, projectID); if err!=nil { panic(err) }
  defer fs.Close()
  db:=fs.Client()
  mm:=ai.NewAIModelManager()
  vertexLocation:=strings.TrimSpace(firstNonEmpty(os.Getenv("VERTEX_LOCATION"), "us-central1"))
  searchLocation:=strings.TrimSpace(firstNonEmpty(os.Getenv("VERTEX_SEARCH_LOCATION"), "global"))
  vertexDataStore:=strings.TrimSpace(firstNonEmpty(os.Getenv("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID"), os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID")))
  vertexSearch:=ai.NewVertexAISearchClient(projectID, searchLocation, vertexDataStore, "")
  vertexService,_:=ai.NewVertexAIService(projectID, vertexLocation, mm)
  deps:=horizon.HorizonServiceDependencies{AIExecutor: ai.NewAITaskExecutor([]ai.AIService{vertexService}, mm).WithCircuitBreaker(ai.NewCircuitBreaker()).WithAdmissionController(ai.NoopAdmissionController()), EmbeddingProvider: ai.NewNoOpEmbedding(), VertexSearchClient: vertexSearch, AgentJobsRepo: corerepo.NewAgentJobsRepository(db), RealtimePublisher:&realtime.Publisher{}, DocumentsRepo: corerepo.NewDocumentsRepository(db), DataSourcesRepo: corerepo.NewDataSourcesRepository(db), Crawler: crawler.NewWebCrawler(), Logger: log.New(os.Stdout, "[check] ", 0)}
  svc,err:=horizon.NewHorizonService(deps, horizonrepo.NewHorizonFragmentsRepository(db), horizonrepo.NewFramesRepository(db), horizonrepo.NewLensesRepository(db), horizonrepo.NewDependencyEdgesRepository(db), horizonrepo.NewWatchdogConfigsRepository(db), horizonrepo.NewScoutingJobsRepository(corerepo.NewAgentJobsRepository(db)), horizonrepo.NewHorizonFragmentVersionsRepository(db)); if err!=nil { panic(err) }
  for _, lim := range []int{8,20} {
    res,err:=svc.SearchFragments(ctx, "CATL", lim)
    fmt.Printf("LIMIT=%d err=%v count=%d\n", lim, err, len(res))
    for i,f := range res { if f==nil {continue}; if i>=12 {break}; fmt.Printf("%d\t%s\t%s\n", i+1, f.Type, f.CanonicalPath) }
  }
}
func firstNonEmpty(v ...string) string { for _, s := range v { if strings.TrimSpace(s)!="" { return strings.TrimSpace(s) } }; return "" }
