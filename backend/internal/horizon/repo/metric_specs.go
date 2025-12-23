package repo

import (
	"context"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/metrics"
)

// MetricSpecsRepository manages metric specifications in Firestore.
type MetricSpecsRepository struct {
	col *firestore.CollectionRef
}

// NewMetricSpecsRepository creates a new MetricSpecsRepository.
func NewMetricSpecsRepository(client *firestore.Client) *MetricSpecsRepository {
	return &MetricSpecsRepository{
		col: client.Collection("horizon_metric_specs"),
	}
}

// GetSpecs retrieves all metric specifications.
func (r *MetricSpecsRepository) GetSpecs(ctx context.Context) ([]*metrics.MetricSpec, error) {
	docs, err := r.col.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	specs := make([]*metrics.MetricSpec, 0, len(docs))
	for _, doc := range docs {
		var spec metrics.MetricSpec
		if err := doc.DataTo(&spec); err != nil {
			continue
		}
		specs = append(specs, &spec)
	}

	return specs, nil
}

