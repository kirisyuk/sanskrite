package regulatorytransform

import (
	"context"
	"strings"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const defaultFirestoreCollection = "horizon_regulatory_transform_pairs"

// LoadRegistryFromFirestore loads authoritative decision→mechanism / mechanism→condition pairs.
// Duplicate fromPath+toPath values collapse to a single entry; later documents overwrite earlier ones (last write wins).
// Documents: tenantId, fromPath, toPath, relationKind (uses_mechanism | imposes_condition), optional canonicalEdgeType.
func LoadRegistryFromFirestore(ctx context.Context, db *firestore.Client) (*Registry, error) {
	if db == nil {
		return NewRegistry(), nil
	}
	col := db.Collection(defaultFirestoreCollection)
	q := col.Where("tenantId", "==", "global")
	iter := q.Documents(ctx)
	reg := NewRegistry()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return reg, err
		}
		var row struct {
			FromPath          string `firestore:"fromPath"`
			ToPath            string `firestore:"toPath"`
			RelationKind      string `firestore:"relationKind"`
			CanonicalEdgeType string `firestore:"canonicalEdgeType"`
		}
		if err := doc.DataTo(&row); err != nil {
			continue
		}
		from := strings.TrimSpace(row.FromPath)
		to := strings.TrimSpace(row.ToPath)
		if from == "" || to == "" {
			continue
		}
		if err := taxonomy.ValidateCanonicalPath(from); err != nil {
			continue
		}
		if err := taxonomy.ValidateCanonicalPath(to); err != nil {
			continue
		}
		p := Pair{FromPath: from, ToPath: to, RelationKind: strings.TrimSpace(row.RelationKind)}
		if ct := strings.TrimSpace(row.CanonicalEdgeType); ct != "" {
			p.CanonicalType = models.DependencyType(ct)
		}
		reg.RegisterPair(p)
	}
	return reg, nil
}
