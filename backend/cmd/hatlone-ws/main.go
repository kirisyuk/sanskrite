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

	// Override Firebase Auth Project ID if specified
	firebaseProjectID := getEnv("FIREBASE_AUTH_PROJECT_ID", projectID)
	log.Printf("Initializing Firebase App with ProjectID: %s (GCP_PROJECT_ID: %s)", firebaseProjectID, projectID)

	fbApp, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: firebaseProjectID}, opts...)
	if err != nil {
		log.Fatalf("firebase init: %v", err)
	}

	fs, err := gcp.NewFirestoreClient(ctx, projectID)
	if err != nil {
		log.Fatalf("firestore init: %v", err)
	}

	logger := log.New(os.Stdout, "[hatlone-ws] ", log.LstdFlags|log.Lshortfile)
	users := repo.NewUsersRepository(fs.Client())

	// Hub
	hub := realtime.NewHub(logger)

	// WS endpoint: /ws
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	// LB-friendly health check (some setups route only /api/* to the service)
	http.HandleFunc("/api/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
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
			logger.Printf("ws: auth init error: %v", err)
			http.Error(w, "auth init error", http.StatusInternalServerError)
			return
		}
		tok, err := client.VerifyIDToken(r.Context(), idToken)
		if err != nil {
			logger.Printf("ws: verify token error: %v", err)
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		uid := tok.UID
		tenant := ""
		if u, err := users.GetByUID(r.Context(), uid); err == nil && u != nil {
			tenant = u.TenantID
		} else if err != nil {
			logger.Printf("ws: failed to get user by uid %s: %v", uid, err)
		}
		if tenant == "" {
			logger.Printf("ws: tenant not resolved for uid %s", uid)
			http.Error(w, "tenant not resolved", http.StatusForbidden)
			return
		}
		hub.ServeWS(w, r, tenant)
	})

	// Pub/Sub subscriber: subscribe to aggregated realtime topics and route to hub
	psClient, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("pubsub init: %v", err)
	}
	sub := realtime.NewSubscriber(psClient, hub, logger)
	_ = sub.Start(ctx, realtime.AggregatedTopics...)

	srv := &http.Server{Addr: ":" + addr, Handler: http.DefaultServeMux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("hatlone-ws listening on :%s", addr)
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
