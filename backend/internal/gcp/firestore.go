package gcp

import (
    "context"

    "cloud.google.com/go/firestore"
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

