package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const missingInfoStatusCollection = "horizon_missing_info_status"

var ErrMissingInfoStatusNotFound = errors.New("missing info status not found")

// MissingInfoStatusRepository handles CRUD for MissingInfoStatus documents.
type MissingInfoStatusRepository struct {
	db *firestore.Client
}

// NewMissingInfoStatusRepository creates a new repository.
func NewMissingInfoStatusRepository(db *firestore.Client) *MissingInfoStatusRepository {
	return &MissingInfoStatusRepository{db: db}
}

// GenerateID creates a deterministic ID from fragment ID and question text.
func GenerateMissingInfoID(fragmentID, questionText string) string {
	normalized := strings.ToLower(strings.TrimSpace(questionText))
	hash := sha256.Sum256([]byte(fragmentID + ":" + normalized))
	return hex.EncodeToString(hash[:16]) // 32 hex chars
}

// GenerateQuestionHash creates a hash of the question text for comparison.
func GenerateQuestionHash(questionText string) string {
	normalized := strings.ToLower(strings.TrimSpace(questionText))
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:8]) // 16 hex chars
}

// Get retrieves a MissingInfoStatus by ID.
func (r *MissingInfoStatusRepository) Get(ctx context.Context, id string) (*models.MissingInfoStatus, error) {
	doc, err := r.db.Collection(missingInfoStatusCollection).Doc(id).Get(ctx)
	if err != nil {
		if grpcstatus.Code(err) == codes.NotFound {
			return nil, ErrMissingInfoStatusNotFound
		}
		return nil, fmt.Errorf("failed to get missing info status: %w", err)
	}

	var status models.MissingInfoStatus
	if err := doc.DataTo(&status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal missing info status: %w", err)
	}
	status.ID = doc.Ref.ID
	return &status, nil
}

// GetByFragmentAndQuestion retrieves a MissingInfoStatus by fragment ID and question.
func (r *MissingInfoStatusRepository) GetByFragmentAndQuestion(ctx context.Context, fragmentID, questionText string) (*models.MissingInfoStatus, error) {
	id := GenerateMissingInfoID(fragmentID, questionText)
	return r.Get(ctx, id)
}

// GetAllForFragment retrieves all MissingInfoStatus records for a fragment.
func (r *MissingInfoStatusRepository) GetAllForFragment(ctx context.Context, fragmentID string, limit int) ([]*models.MissingInfoStatus, error) {
	if limit <= 0 {
		limit = 100
	}
	
	iter := r.db.Collection(missingInfoStatusCollection).
		Where("fragmentId", "==", fragmentID).
		OrderBy("createdAt", firestore.Desc).
		Limit(limit).
		Documents(ctx)
	
	docs, err := iter.GetAll()
	if err != nil {
		return nil, fmt.Errorf("failed to list missing info statuses: %w", err)
	}

	result := make([]*models.MissingInfoStatus, 0, len(docs))
	for _, doc := range docs {
		var status models.MissingInfoStatus
		if err := doc.DataTo(&status); err != nil {
			continue
		}
		status.ID = doc.Ref.ID
		result = append(result, &status)
	}
	return result, nil
}

// Create creates a new MissingInfoStatus.
func (r *MissingInfoStatusRepository) Create(ctx context.Context, status *models.MissingInfoStatus) error {
	if status.ID == "" {
		status.ID = GenerateMissingInfoID(status.FragmentID, status.QuestionText)
	}
	status.QuestionHash = GenerateQuestionHash(status.QuestionText)
	status.CreatedAt = time.Now().UTC()
	status.UpdatedAt = status.CreatedAt

	_, err := r.db.Collection(missingInfoStatusCollection).Doc(status.ID).Set(ctx, status)
	if err != nil {
		return fmt.Errorf("failed to create missing info status: %w", err)
	}
	return nil
}

// Upsert creates or updates a MissingInfoStatus.
func (r *MissingInfoStatusRepository) Upsert(ctx context.Context, status *models.MissingInfoStatus) error {
	if status.ID == "" {
		status.ID = GenerateMissingInfoID(status.FragmentID, status.QuestionText)
	}
	status.QuestionHash = GenerateQuestionHash(status.QuestionText)
	status.UpdatedAt = time.Now().UTC()
	if status.CreatedAt.IsZero() {
		status.CreatedAt = status.UpdatedAt
	}

	_, err := r.db.Collection(missingInfoStatusCollection).Doc(status.ID).Set(ctx, status)
	if err != nil {
		return fmt.Errorf("failed to upsert missing info status: %w", err)
	}
	return nil
}

// RecordAttempt records a research attempt and updates cooldown.
func (r *MissingInfoStatusRepository) RecordAttempt(ctx context.Context, fragmentID, questionText string, found bool, cooldownHours int) error {
	id := GenerateMissingInfoID(fragmentID, questionText)
	
	return r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		docRef := r.db.Collection(missingInfoStatusCollection).Doc(id)
		doc, err := tx.Get(docRef)
		
		now := time.Now().UTC()
		var status models.MissingInfoStatus
		
		if err != nil {
			// Create new record ONLY when doc doesn't exist. Any other error should propagate.
			if grpcstatus.Code(err) != codes.NotFound {
				return fmt.Errorf("failed to read missing info status doc in tx: %w", err)
			}
			// Create new record
			status = models.MissingInfoStatus{
				ID:           id,
				FragmentID:   fragmentID,
				QuestionHash: GenerateQuestionHash(questionText),
				QuestionText: questionText,
				Status:       models.MissingInfoStatusOpen,
				Attempts:     0,
				CreatedAt:    now,
			}
		} else {
			if err := doc.DataTo(&status); err != nil {
				return fmt.Errorf("failed to unmarshal status: %w", err)
			}
		}

		status.Attempts++
		status.LastAttemptAt = &now
		status.UpdatedAt = now

		if found {
			// Mark as resolved
			status.Status = models.MissingInfoStatusResolved
			status.ResolvedAt = &now
			status.NextAttemptAt = nil
		} else {
			// Check if exhausted
			if status.Attempts >= models.DefaultMaxAttempts {
				status.Status = models.MissingInfoStatusExhausted
				status.ExhaustedAt = &now
				status.NextAttemptAt = nil
			} else {
				// Set cooldown
				if cooldownHours <= 0 {
					cooldownHours = models.DefaultCooldownHours
				}
				next := now.Add(time.Duration(cooldownHours) * time.Hour)
				status.NextAttemptAt = &next
			}
		}

		return tx.Set(docRef, &status)
	})
}

// ResetForNewSources resets an exhausted status when new sources are available.
func (r *MissingInfoStatusRepository) ResetForNewSources(ctx context.Context, fragmentID, questionText, newFingerprint string) error {
	id := GenerateMissingInfoID(fragmentID, questionText)
	
	return r.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		docRef := r.db.Collection(missingInfoStatusCollection).Doc(id)
		doc, err := tx.Get(docRef)
		if err != nil {
			if grpcstatus.Code(err) == codes.NotFound {
				return nil // Nothing to reset
			}
			return fmt.Errorf("failed to read missing info status doc in tx: %w", err)
		}

		var status models.MissingInfoStatus
		if err := doc.DataTo(&status); err != nil {
			return fmt.Errorf("failed to unmarshal status: %w", err)
		}

		if status.ShouldResetForNewSources(newFingerprint) {
			status.Status = models.MissingInfoStatusOpen
			status.Attempts = 0
			status.SourceFingerprint = newFingerprint
			status.ExhaustedAt = nil
			status.NextAttemptAt = nil
			status.UpdatedAt = time.Now().UTC()
			return tx.Set(docRef, &status)
		}
		return nil
	})
}

// FilterSearchable filters a list of questions to only those that are searchable.
// Returns: (searchable questions, filtered out count)
func (r *MissingInfoStatusRepository) FilterSearchable(ctx context.Context, fragmentID string, questions []string) ([]string, int, error) {
	if len(questions) == 0 {
		return nil, 0, nil
	}

	searchable := make([]string, 0, len(questions))
	filtered := 0

	for _, q := range questions {
		status, err := r.GetByFragmentAndQuestion(ctx, fragmentID, q)
		if err != nil {
			// Not found - means it's new, so it's searchable. Any other error should bubble up.
			if errors.Is(err, ErrMissingInfoStatusNotFound) {
				searchable = append(searchable, q)
				continue
			}
			return nil, 0, fmt.Errorf("missing info status lookup failed: fragmentId=%s: %w", fragmentID, err)
		}

		if status.IsSearchable() {
			searchable = append(searchable, q)
		} else {
			filtered++
		}
	}

	return searchable, filtered, nil
}

