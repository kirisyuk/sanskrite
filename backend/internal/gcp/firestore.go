package gcp

import (
	"context"
	"io"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
)

type FirestoreClient struct {
	client *firestore.Client
}

func NewFirestoreClient(ctx context.Context, projectID string) (*FirestoreClient, error) {
	c, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &FirestoreClient{client: c}, nil
}

func (f *FirestoreClient) Close() error { return f.client.Close() }

// Client exposes the underlying Firestore client for repositories.
func (f *FirestoreClient) Client() *firestore.Client { return f.client }

// --- GCS helper ---

type GCSClient struct {
	client *storage.Client
}

func NewGCSClient(ctx context.Context, projectID string) (*GCSClient, error) {
	c, err := storage.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return &GCSClient{client: c}, nil
}

func (g *GCSClient) UploadBytes(ctx context.Context, bucket, object string, data []byte, contentType string, metadata map[string]string) error {
	w := g.client.Bucket(bucket).Object(object).NewWriter(ctx)
	if contentType != "" {
		w.ContentType = contentType
	}
	if metadata != nil {
		w.Metadata = metadata
	}
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func (g *GCSClient) DeleteObject(ctx context.Context, bucket, object string) error {
	return g.client.Bucket(bucket).Object(object).Delete(ctx)
}

func (g *GCSClient) ReadObject(ctx context.Context, bucket, object string) ([]byte, error) {
	r, err := g.client.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
