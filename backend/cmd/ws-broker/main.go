package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/pubsub"
	firebase "firebase.google.com/go/v4"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"google.golang.org/api/option"
)

func main() {
	addr := getEnv("PORT", "8080")
	ctx := context.Background()

	projectID := getEnv("GCP_PROJECT_ID", getEnv("GOOGLE_CLOUD_PROJECT", ""))
	if projectID == "" {
		log.Fatal("GCP_PROJECT_ID/GOOGLE_CLOUD_PROJECT is required")
	}

	var opts []option.ClientOption
	if creds := getEnv("GOOGLE_APPLICATION_CREDENTIALS", ""); creds != "" {
		opts = append(opts, option.WithCredentialsFile(creds))
	}

	fbApp, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID}, opts...)
	if err != nil {
		log.Fatalf("firebase init: %v", err)
	}

	fs, err := gcp.NewFirestoreClient(ctx, projectID)
	if err != nil {
		log.Fatalf("firestore init: %v", err)
	}

	logger := log.New(os.Stdout, "[ws-broker] ", log.LstdFlags|log.Lshortfile)
	users := repo.NewUsersRepository(fs.Client())

	// Hub
	hub := realtime.NewHub(logger)

	// WS endpoint: /ws
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK); w.Write([]byte("ok")) })
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		// Authorization: Bearer <FirebaseIDToken> or ?token=
		idToken := ""
		if ah := r.Header.Get("Authorization"); len(ah) > 7 && ah[:7] == "Bearer " {
			idToken = ah[7:]
		} else {
			idToken = r.URL.Query().Get("token")
		}
		if idToken == "" {
			http.Error(w, "missing or invalid Authorization", http.StatusUnauthorized)
			return
		}
		client, err := fbApp.Auth(r.Context())
		if err != nil {
			http.Error(w, "auth init error", http.StatusInternalServerError)
			return
		}
		tok, err := client.VerifyIDToken(r.Context(), idToken)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		uid := tok.UID
		tenant := ""
		if u, err := users.GetByUID(r.Context(), uid); err == nil && u != nil {
			tenant = u.TenantID
		}
		if tenant == "" {
			http.Error(w, "tenant not resolved", http.StatusForbidden)
			return
		}
		hub.ServeWS(w, r, tenant)
	})

	// Pub/Sub subscriber: subscribe to realtime topics and route to hub
	psClient, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("pubsub init: %v", err)
	}
	sub := realtime.NewSubscriber(psClient, hub, logger)
	_ = sub.Start(ctx,
		string(realtime.EventDiscoveryStatus),
		string(realtime.EventDiscoveryProfile),
		string(realtime.EventDiscoveryAnswer),
		string(realtime.EventDiscoveryProject),
		string(realtime.EventDocProcess),
		string(realtime.EventAnalysisStatus),
		string(realtime.EventGenAIStream),
		string(realtime.EventArtifactGenerationStarted),
		string(realtime.EventArtifactSectionGenerating),
		string(realtime.EventArtifactSectionChunk),
		string(realtime.EventArtifactSectionCompleted),
		string(realtime.EventArtifactAssemblyStarted),
		string(realtime.EventArtifactAssemblyChunk),
		string(realtime.EventArtifactAssemblyCompleted),
		string(realtime.EventArtifactGenerationCompleted),
		string(realtime.EventArtifactGenerationError),
		string(realtime.EventAgentUpdate),
	)

	srv := &http.Server{Addr: ":" + addr, Handler: http.DefaultServeMux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("ws-broker listening on :%s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
