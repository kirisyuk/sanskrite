package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// Main RSS source per category (from worldmonitor FULL_FEEDS).
// Uses raw URLs — backend fetches directly, no proxy.
var seedSources = []struct {
	Category    string
	DisplayName string
	RSSURL      string
}{
	// politics
	{"politics", "BBC World", "https://feeds.bbci.co.uk/news/world/rss.xml"},
	{"politics", "Reuters World", "https://feeds.reuters.com/reuters/worldNews"},
	// middleeast
	{"middleeast", "Al Jazeera", "https://www.aljazeera.com/xml/rss/all.xml"},
	{"middleeast", "BBC Middle East", "https://feeds.bbci.co.uk/news/world/middle_east/rss.xml"},
	// tech
	{"tech", "Hacker News", "https://hnrss.org/frontpage"},
	{"tech", "Ars Technica", "https://feeds.arstechnica.com/arstechnica/technology-lab"},
	// ai
	{"ai", "MIT Tech Review AI", "https://www.technologyreview.com/topic/artificial-intelligence/feed"},
	{"ai", "VentureBeat AI", "https://venturebeat.com/category/ai/feed/"},
	// finance
	{"finance", "CNBC", "https://www.cnbc.com/id/100003114/device/rss/rss.html"},
	{"finance", "Reuters Business", "https://feeds.reuters.com/reuters/businessNews"},
	// gov
	{"gov", "Federal Reserve", "https://www.federalreserve.gov/feeds/press_all.xml"},
	{"gov", "SEC", "https://www.sec.gov/news/pressreleases.rss"},
	{"gov", "UN News", "https://news.un.org/feed/subscribe/en/news/all/rss.xml"},
	// layoffs
	{"layoffs", "Layoffs.fyi", "https://layoffs.fyi/feed/"},
	// thinktanks
	{"thinktanks", "Foreign Policy", "https://foreignpolicy.com/feed/"},
	{"thinktanks", "Brookings", "https://www.brookings.edu/feed/"},
	// crisis
	{"crisis", "CrisisWatch", "https://www.crisisgroup.org/rss"},
	{"crisis", "IAEA", "https://www.iaea.org/feeds/topnews"},
	// regional
	{"regional", "Kyiv Independent", "https://kyivindependent.com/feed/"},
	{"regional", "Moscow Times", "https://www.themoscowtimes.com/rss/news"},
	// africa
	{"africa", "BBC Africa", "https://feeds.bbci.co.uk/news/world/africa/rss.xml"},
	// latam
	{"latam", "BBC Latin America", "https://feeds.bbci.co.uk/news/world/latin_america/rss.xml"},
	// asia
	{"asia", "BBC Asia", "https://feeds.bbci.co.uk/news/world/asia/rss.xml"},
	// energy
	{"energy", "Reuters Energy", "https://feeds.reuters.com/reuters/energyNews"},
}

func main() {
	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id")
	apply := flag.Bool("apply", false, "actually write to Firestore (default: dry-run)")
	flag.Parse()

	if *projectID == "" {
		*projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if *projectID == "" {
		log.Fatal("--project required or set GCP_PROJECT_ID")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("firestore: %v", err)
	}
	defer fsClient.Close()

	dsRepo := repo.NewDataSourcesRepository(fsClient.Client())

	created := 0
	for _, s := range seedSources {
		ds := &models.DataSource{
			TenantID:    "",
			Pk:          "global",
			DisplayName: s.DisplayName,
			Kind:        "rss",
			Status:      "paused",
			OwnerUserID: "system",
			Details: models.SourceDetails{
				RSSURL:   strPtr(s.RSSURL),
				Category: strPtr(s.Category),
			},
		}

		if *apply {
			id, err := dsRepo.Create(ctx, ds)
			if err != nil {
				log.Printf("create %s: %v", s.DisplayName, err)
				continue
			}
			created++
			fmt.Printf("created %s [%s] id=%s\n", s.DisplayName, s.Category, id)
		} else {
			fmt.Printf("[dry-run] would create %s [%s] %s\n", s.DisplayName, s.Category, s.RSSURL)
		}
	}

	if *apply {
		log.Printf("seed complete: created %d RSS sources (paused, global tenant)", created)
	} else {
		log.Printf("dry-run: would create %d sources. run with --apply to write", len(seedSources))
	}
}

func strPtr(s string) *string { return &s }
