package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRSSProvider_Ingest(t *testing.T) {
	// Minimal RSS XML
	rss := `<?xml version="1.0"?><rss><channel><title>Feed</title><link>https://example.com</link>
    <item><guid>g1</guid><title>T1</title><link>https://example.com/1</link><pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate><description>Desc</description></item>
    </channel></rss>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rss))
	}))
	defer srv.Close()

	p := NewRSSProvider()
	docs, err := p.Ingest(context.Background(), "ten", srv.URL)
	if err != nil {
		t.Fatalf("ingest err: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].ExternalID == "" || docs[0].FullText == "" {
		t.Fatalf("expected fields populated: %+v", docs[0])
	}
}
