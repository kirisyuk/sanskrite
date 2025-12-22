package gcp

import (
	"context"
	"fmt"
	"io"
	"strings"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
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

// ObjectExists checks if an object exists in GCS bucket
func (g *GCSClient) ObjectExists(ctx context.Context, bucket, object string) (bool, error) {
	_, err := g.client.Bucket(bucket).Object(object).Attrs(ctx)
	if err != nil {
		// Check if error is "object doesn't exist" (can be ErrObjectNotExist or 404 error)
		if err == storage.ErrObjectNotExist {
			return false, nil
		}
		// Check for 404 error (googleapi.Error with Code 404)
		if apiErr, ok := err.(*googleapi.Error); ok && apiErr.Code == 404 {
			return false, nil
		}
		// Check for 404 error in error message (fallback)
		if errStr := err.Error(); errStr != "" {
			if strings.Contains(errStr, "404") || strings.Contains(errStr, "No such object") {
				return false, nil
			}
		}
		// Other errors (permissions, network, etc.) - return error
		return false, err
	}
	return true, nil
}

// UpdateMetadata updates metadata for an existing GCS object without re-uploading content
func (g *GCSClient) UpdateMetadata(ctx context.Context, bucket, object string, metadata map[string]string) error {
	obj := g.client.Bucket(bucket).Object(object)
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get object attributes: %w", err)
	}

	// Merge new metadata with existing metadata
	newMetadata := make(map[string]string)
	if attrs.Metadata != nil {
		for k, v := range attrs.Metadata {
			newMetadata[k] = v
		}
	}
	for k, v := range metadata {
		newMetadata[k] = v
	}

	// Update object with new metadata
	objAttrs := storage.ObjectAttrsToUpdate{
		Metadata: newMetadata,
	}
	_, err = obj.Update(ctx, objAttrs)
	return err
}

// GetMetadata retrieves metadata for a GCS object
// Returns empty map (not nil) if metadata is empty, for easier iteration
func (g *GCSClient) GetMetadata(ctx context.Context, bucket, object string) (map[string]string, error) {
	obj := g.client.Bucket(bucket).Object(object)
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get object attributes: %w", err)
	}
	if attrs.Metadata == nil {
		return make(map[string]string), nil
	}
	return attrs.Metadata, nil
}
