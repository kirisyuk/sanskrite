package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchGoogleStaticSatelliteMap_ClampsAndSuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "k" {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("maptype") != "satellite" {
			http.Error(w, "bad maptype", http.StatusBadRequest)
			return
		}
		// Clamped zoom 99 -> 22, size 9999 -> 640
		if got := r.URL.Query().Get("zoom"); got != "22" {
			t.Fatalf("zoom: got %q", got)
		}
		if got := r.URL.Query().Get("size"); got != "640x640" {
			t.Fatalf("size: got %q", got)
		}
		if got := r.URL.Query().Get("scale"); got != "1" {
			t.Fatalf("scale: got %q", got)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	}))
	t.Cleanup(srv.Close)

	orig := googleStaticMapBaseURL
	googleStaticMapBaseURL = srv.URL
	t.Cleanup(func() { googleStaticMapBaseURL = orig })

	b, ct, err := FetchGoogleStaticSatelliteMap(context.Background(), "k", 1.2, 3.4, 99, 9999, 9999, 9)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "image/png" {
		t.Fatalf("content-type: %s", ct)
	}
	if len(b) != 4 {
		t.Fatalf("body len: %d", len(b))
	}
}
