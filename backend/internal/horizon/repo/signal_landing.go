package repo

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type SignalLandingSessionsRepository struct {
	col *firestore.CollectionRef
}

func NewSignalLandingSessionsRepository(db *firestore.Client) *SignalLandingSessionsRepository {
	return &SignalLandingSessionsRepository{col: db.Collection("horizon_signal_landing_sessions")}
}

func (r *SignalLandingSessionsRepository) Upsert(ctx context.Context, session *models.SignalLandingSession) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("signal landing sessions repo not configured")
	}
	if session == nil {
		return ErrInvalidDocument
	}
	session.Normalize()
	if session.ID == "" {
		session.ID = signalLandingDocID("sls", session.TenantID, session.SourceEventID, session.SourceFactID)
	}
	_, err := r.col.Doc(session.ID).Set(ctx, session)
	return err
}

func (r *SignalLandingSessionsRepository) Get(ctx context.Context, id string) (*models.SignalLandingSession, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("signal landing sessions repo not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrInvalidDocument
	}
	doc, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var session models.SignalLandingSession
	if err := doc.DataTo(&session); err != nil {
		return nil, err
	}
	if session.ID == "" {
		session.ID = doc.Ref.ID
	}
	return &session, nil
}

func (r *SignalLandingSessionsRepository) ListRecent(ctx context.Context, tenantID string, limit int) ([]*models.SignalLandingSession, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("signal landing sessions repo not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	if limit <= 0 {
		limit = 20
	}
	scanLimit := limit
	if scanLimit < 50 {
		scanLimit = 50
	}
	if scanLimit > 200 {
		scanLimit = 200
	}
	docs, err := r.col.Where("tenantId", "==", tenantID).Limit(scanLimit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.SignalLandingSession, 0, len(docs))
	for _, doc := range docs {
		var session models.SignalLandingSession
		if err := doc.DataTo(&session); err != nil {
			continue
		}
		if session.ID == "" {
			session.ID = doc.Ref.ID
		}
		out = append(out, &session)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *SignalLandingSessionsRepository) ListBySourceFactID(ctx context.Context, tenantID, sourceFactID string, limit int) ([]*models.SignalLandingSession, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("signal landing sessions repo not configured")
	}
	sourceFactID = strings.TrimSpace(sourceFactID)
	if sourceFactID == "" {
		return nil, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	if limit <= 0 {
		limit = 12
	}
	docs, err := r.col.Where("tenantId", "==", tenantID).Where("sourceFactId", "==", sourceFactID).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.SignalLandingSession, 0, len(docs))
	for _, doc := range docs {
		var session models.SignalLandingSession
		if err := doc.DataTo(&session); err != nil {
			continue
		}
		if session.ID == "" {
			session.ID = doc.Ref.ID
		}
		out = append(out, &session)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

type SignalImpactClaimsRepository struct {
	col *firestore.CollectionRef
}

func NewSignalImpactClaimsRepository(db *firestore.Client) *SignalImpactClaimsRepository {
	return &SignalImpactClaimsRepository{col: db.Collection("horizon_signal_impact_claims")}
}

func (r *SignalImpactClaimsRepository) Upsert(ctx context.Context, claim *models.SignalImpactClaim) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("signal impact claims repo not configured")
	}
	if claim == nil {
		return ErrInvalidDocument
	}
	claim.Normalize()
	if claim.ID == "" {
		claim.ID = signalLandingDocID("slc", claim.TenantID, claim.SessionID, claim.SignalID, claim.Axis, claim.Mechanism, strings.Join(claim.ClaimLabels, "|"))
	}
	_, err := r.col.Doc(claim.ID).Set(ctx, claim)
	return err
}

func (r *SignalImpactClaimsRepository) ListBySession(ctx context.Context, sessionID string, limit int) ([]*models.SignalImpactClaim, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("signal impact claims repo not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	docs, err := r.col.Where("sessionId", "==", sessionID).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.SignalImpactClaim, 0, len(docs))
	for _, doc := range docs {
		var claim models.SignalImpactClaim
		if err := doc.DataTo(&claim); err != nil {
			continue
		}
		if claim.ID == "" {
			claim.ID = doc.Ref.ID
		}
		out = append(out, &claim)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity == out[j].Severity {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].Severity > out[j].Severity
	})
	return out, nil
}

type SignalLandingHitsRepository struct {
	col *firestore.CollectionRef
}

func NewSignalLandingHitsRepository(db *firestore.Client) *SignalLandingHitsRepository {
	return &SignalLandingHitsRepository{col: db.Collection("horizon_signal_landing_hits")}
}

func (r *SignalLandingHitsRepository) Upsert(ctx context.Context, hit *models.SignalLandingHit) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("signal landing hits repo not configured")
	}
	if hit == nil {
		return ErrInvalidDocument
	}
	hit.Normalize()
	if hit.ID == "" {
		hit.ID = signalLandingDocID("slh", hit.TenantID, hit.SessionID, hit.ClaimID, hit.CanonicalRef, string(hit.LandingClass), string(hit.LandingMethod))
	}
	_, err := r.col.Doc(hit.ID).Set(ctx, hit)
	return err
}

func (r *SignalLandingHitsRepository) ListBySession(ctx context.Context, sessionID string, limit int) ([]*models.SignalLandingHit, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("signal landing hits repo not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 300
	}
	docs, err := r.col.Where("sessionId", "==", sessionID).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.SignalLandingHit, 0, len(docs))
	for _, doc := range docs {
		var hit models.SignalLandingHit
		if err := doc.DataTo(&hit); err != nil {
			continue
		}
		if hit.ID == "" {
			hit.ID = doc.Ref.ID
		}
		out = append(out, &hit)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Importance == out[j].Importance {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].Importance > out[j].Importance
	})
	return out, nil
}

type SignalMissingAnchorCandidatesRepository struct {
	col *firestore.CollectionRef
}

func NewSignalMissingAnchorCandidatesRepository(db *firestore.Client) *SignalMissingAnchorCandidatesRepository {
	return &SignalMissingAnchorCandidatesRepository{col: db.Collection("horizon_signal_missing_candidates")}
}

func (r *SignalMissingAnchorCandidatesRepository) Upsert(ctx context.Context, candidate *models.SignalMissingAnchorCandidate) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("signal missing candidates repo not configured")
	}
	if candidate == nil {
		return ErrInvalidDocument
	}
	candidate.Normalize()
	if candidate.ID == "" {
		candidate.ID = signalLandingDocID("slm", candidate.TenantID, candidate.SessionID, candidate.ClaimID, string(candidate.CandidateKind), candidate.NormalizedLabel, candidate.SuggestedCanonicalPath)
	}
	_, err := r.col.Doc(candidate.ID).Set(ctx, candidate)
	return err
}

func (r *SignalMissingAnchorCandidatesRepository) ListBySession(ctx context.Context, sessionID string, limit int) ([]*models.SignalMissingAnchorCandidate, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("signal missing candidates repo not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	docs, err := r.col.Where("sessionId", "==", sessionID).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.SignalMissingAnchorCandidate, 0, len(docs))
	for _, doc := range docs {
		var candidate models.SignalMissingAnchorCandidate
		if err := doc.DataTo(&candidate); err != nil {
			continue
		}
		if candidate.ID == "" {
			candidate.ID = doc.Ref.ID
		}
		out = append(out, &candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Importance == out[j].Importance {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].Importance > out[j].Importance
	})
	return out, nil
}

type SignalLandingSummariesRepository struct {
	col *firestore.CollectionRef
}

func NewSignalLandingSummariesRepository(db *firestore.Client) *SignalLandingSummariesRepository {
	return &SignalLandingSummariesRepository{col: db.Collection("horizon_signal_landing_summaries")}
}

func (r *SignalLandingSummariesRepository) Upsert(ctx context.Context, summary *models.SignalLandingSummary) error {
	if r == nil || r.col == nil {
		return fmt.Errorf("signal landing summaries repo not configured")
	}
	if summary == nil {
		return ErrInvalidDocument
	}
	if err := summary.Validate(); err != nil {
		return err
	}
	if summary.ID == "" {
		summary.ID = signalLandingDocID("slsum", summary.TenantID, summary.ScopeRef, strings.Join(summary.SessionIDs, "|"))
	}
	_, err := r.col.Doc(summary.ID).Set(ctx, summary)
	return err
}

func (r *SignalLandingSummariesRepository) ListByScopeRefs(ctx context.Context, tenantID string, scopeRefs []string, limit int) ([]*models.SignalLandingSummary, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("signal landing summaries repo not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	scopeRefs = compactUniqueSignalLandingStrings(scopeRefs)
	if len(scopeRefs) == 0 {
		return nil, nil
	}
	if tenantID == "" {
		tenantID = "global"
	}
	if limit <= 0 {
		limit = 100
	}
	tenantOrder := []string{tenantID}
	if tenantID != "global" {
		tenantOrder = append(tenantOrder, "global")
	}
	out := make([]*models.SignalLandingSummary, 0, minSignalLanding(limit, len(scopeRefs)))
	seen := make(map[string]struct{}, limit)
	for _, scopedTenantID := range tenantOrder {
		for start := 0; start < len(scopeRefs) && len(out) < limit; start += 10 {
			end := start + 10
			if end > len(scopeRefs) {
				end = len(scopeRefs)
			}
			docs, err := r.col.
				Where("tenantId", "==", scopedTenantID).
				Where("scopeRef", "in", scopeRefs[start:end]).
				Limit(limit - len(out)).
				Documents(ctx).
				GetAll()
			if err != nil {
				return nil, err
			}
			for _, doc := range docs {
				var summary models.SignalLandingSummary
				if err := doc.DataTo(&summary); err != nil {
					continue
				}
				if summary.ID == "" {
					summary.ID = doc.Ref.ID
				}
				if _, exists := seen[summary.ScopeRef]; exists {
					continue
				}
				seen[summary.ScopeRef] = struct{}{}
				out = append(out, &summary)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (r *SignalLandingSummariesRepository) ListRecent(ctx context.Context, tenantID string, limit int) ([]*models.SignalLandingSummary, error) {
	if r == nil || r.col == nil {
		return nil, fmt.Errorf("signal landing summaries repo not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	if limit <= 0 {
		limit = 20
	}
	scanLimit := limit
	if scanLimit < 50 {
		scanLimit = 50
	}
	if scanLimit > 200 {
		scanLimit = 200
	}
	docs, err := r.col.Where("tenantId", "==", tenantID).Limit(scanLimit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.SignalLandingSummary, 0, len(docs))
	for _, doc := range docs {
		var summary models.SignalLandingSummary
		if err := doc.DataTo(&summary); err != nil {
			continue
		}
		if summary.ID == "" {
			summary.ID = doc.Ref.ID
		}
		out = append(out, &summary)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func signalLandingDocID(prefix string, parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		clean = append(clean, part)
	}
	sum := sha1.Sum([]byte(strings.Join(clean, "\n")))
	return prefix + "_" + hex.EncodeToString(sum[:8])
}

func compactUniqueSignalLandingStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func minSignalLanding(a, b int) int {
	if a < b {
		return a
	}
	return b
}
