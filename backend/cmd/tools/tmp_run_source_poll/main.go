package main
import (
  "context"
  "flag"
  "log"
  "os"
  "time"

  "github.com/igorkirisyuk/sanskrite/internal/ai"
  "github.com/igorkirisyuk/sanskrite/internal/crawler"
  "github.com/igorkirisyuk/sanskrite/internal/discovery"
  prov "github.com/igorkirisyuk/sanskrite/internal/discovery/providers"
  gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
  "github.com/igorkirisyuk/sanskrite/internal/realtime"
  "github.com/igorkirisyuk/sanskrite/internal/repo"
  "github.com/igorkirisyuk/sanskrite/internal/taxonomy"
  "github.com/igorkirisyuk/sanskrite/internal/telegram"
)
func main(){
  sourceID:=flag.String("source","","source id")
  tenantID:=flag.String("tenant","global","tenant id")
  projectID:=flag.String("project","etigram","project id")
  flag.Parse()
  if *sourceID=="" { log.Fatal("--source is required") }
  ctx:=context.Background()
  fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
  if err != nil { log.Fatal(err) }
  defer fsClient.Close()
  taxonomy.InitRuntimeTaxonomyCacheFromFirestore(fsClient.Client(), "v1", 10*time.Minute)
  _ = taxonomy.RefreshRuntimeTaxonomy(ctx)
  taxonomy.InitMarketSegmentDictionaryCacheFromFirestore(fsClient.Client(), "v1", 20*time.Minute)
  _ = taxonomy.RefreshMarketSegmentDictionary(ctx)
  logger:=log.New(os.Stdout, "[tmp-run-source] ", log.LstdFlags|log.Lshortfile)
  sourcesRepo:=repo.NewDataSourcesRepository(fsClient.Client())
  sourceProjectsRepo:=repo.NewSourceProjectsRepository(fsClient.Client())
  docsRepo:=repo.NewDocumentsRepository(fsClient.Client())
  ingestHistoryRepo:=repo.NewSourceIngestHistoryRepository(fsClient.Client())
  ontologyFactsRepo:=repo.NewOntologyFactsRepository(fsClient.Client())
  publisher, err := realtime.NewPublisher(ctx, *projectID)
  if err != nil { log.Fatal(err) }
  exec := ai.NewAITaskExecutor(nil, nil)
  wc := crawler.NewWebCrawler()
  tgBase := os.Getenv("TELEGRAM_SERVICE_URL")
  tgClient := telegram.NewClient(tgBase)
  orchestrator := discovery.NewOrchestrator(exec, wc, tgClient, nil, sourcesRepo, publisher, logger)
  orchestrator = orchestrator.WithSourceProjects(sourceProjectsRepo)
  reg := prov.NewRegistry()
  reg.Register(prov.NewRSSProvider())
  orchestrator = orchestrator.WithDocumentIngest(reg, docsRepo)
  emb := ai.NewOpenAIEmbedding(exec, "text-embedding-3-small")
  orchestrator = orchestrator.WithEmbedding(emb)
  orchestrator = orchestrator.WithIngestHistory(ingestHistoryRepo)
  orchestrator = orchestrator.WithHorizonMonitorPipeline(
    discovery.NewHorizonMonitorPipeline(
      ontologyFactsRepo,
      exec,
      nil,
      nil,
      nil,
      nil,
      logger,
    ),
  )
  poller := discovery.NewSourcePoller(orchestrator, logger)
  if err := poller.RunSourcePoll(ctx, discovery.SourceTaskPayload{SourceID:*sourceID, TenantID:*tenantID, Kind:"rss", LeaseOwner:"manual-eval"}); err != nil { log.Fatal(err) }
}
