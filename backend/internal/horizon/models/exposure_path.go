package models

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ExposureType is a strict scenario/type enum for exposure paths.
// IMPORTANT: keep this intentionally small; new types should be added explicitly.
type ExposureType string

const (
	ExposureTypeSupplyChainRisk ExposureType = "SUPPLY_CHAIN_RISK"
)

func (t ExposureType) Valid() bool {
	switch t {
	case ExposureTypeSupplyChainRisk:
		return true
	default:
		return false
	}
}

func ParseExposureType(s string) (ExposureType, bool) {
	t := ExposureType(strings.ToUpper(strings.TrimSpace(s)))
	return t, t.Valid()
}

// ExposureKeyParts describes the canonical structure of the exposure key string.
// Key format:
//   /exposure/<EXPOSURE_TYPE>/objective/<kind>/<id>[/related/<kind>/<id>][/path/<pathId>]
type ExposureKeyParts struct {
	Type ExposureType

	ObjectiveKind string
	ObjectiveID   string

	RelatedKind string
	RelatedID   string

	// Optional: identifies a specific path instance under the same prefix.
	PathID string
}

func (p ExposureKeyParts) PrefixKey() string {
	key, _ := BuildExposureKey(p.Type, p.ObjectiveKind, p.ObjectiveID, p.RelatedKind, p.RelatedID, "")
	return key
}

func (p ExposureKeyParts) CanonicalKey() string {
	key, _ := BuildExposureKey(p.Type, p.ObjectiveKind, p.ObjectiveID, p.RelatedKind, p.RelatedID, p.PathID)
	return key
}

// BuildExposureKey returns a canonical exposure key string.
func BuildExposureKey(t ExposureType, objectiveKind, objectiveID, relatedKind, relatedID, pathID string) (string, error) {
	if !t.Valid() {
		return "", fmt.Errorf("invalid exposure type: %q", string(t))
	}
	ok := normalizeKind(objectiveKind)
	oid := strings.TrimSpace(objectiveID)
	if ok == "" || oid == "" {
		return "", fmt.Errorf("objective kind and id are required")
	}

	out := []string{"exposure", string(t), "objective", ok, oid}

	rk := normalizeKind(relatedKind)
	rid := strings.TrimSpace(relatedID)
	if rk != "" || rid != "" {
		if rk == "" || rid == "" {
			return "", fmt.Errorf("related kind and id must be both set or both empty")
		}
		out = append(out, "related", rk, rid)
	}

	pid := strings.TrimSpace(pathID)
	if pid != "" {
		out = append(out, "path", pid)
	}

	return "/" + strings.Join(out, "/"), nil
}

var kindRe = regexp.MustCompile(`[^a-z0-9_]+`)

func normalizeKind(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "-", "_")
	s = kindRe.ReplaceAllString(s, "")
	return s
}

// CanonicalizeExposureKey parses and canonicalizes an exposure key string.
// It is tolerant to minor input variations:
// - missing "/exposure" prefix (if the first segment is an exposure type)
// - "obj" instead of "objective"
// - "rel" instead of "related"
//
// Returns (canonicalKey, parts, error).
func CanonicalizeExposureKey(raw string) (string, *ExposureKeyParts, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil, fmt.Errorf("empty exposure key")
	}

	partsRaw := strings.Split(strings.Trim(s, "/"), "/")
	parts := make([]string, 0, len(partsRaw))
	for _, p := range partsRaw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("empty exposure key")
	}

	idx := 0
	if strings.EqualFold(parts[0], "exposure") {
		idx = 1
	} else {
		// tolerate missing prefix if first segment is exposure type
		if t, ok := ParseExposureType(parts[0]); ok && t.Valid() {
			// ok
		} else {
			return "", nil, fmt.Errorf("exposure key must start with /exposure or a known exposure type, got: %q", parts[0])
		}
	}
	if idx >= len(parts) {
		return "", nil, fmt.Errorf("invalid exposure key: missing exposure type")
	}

	t, ok := ParseExposureType(parts[idx])
	if !ok {
		return "", nil, fmt.Errorf("unknown exposure type: %q", parts[idx])
	}
	idx++

	if idx >= len(parts) {
		return "", nil, fmt.Errorf("invalid exposure key: missing objective segment")
	}

	objTok := strings.ToLower(parts[idx])
	if objTok == "obj" {
		objTok = "objective"
	}
	if objTok != "objective" {
		return "", nil, fmt.Errorf("invalid exposure key: expected 'objective' segment, got: %q", parts[idx])
	}
	idx++
	if idx+1 >= len(parts) {
		return "", nil, fmt.Errorf("invalid exposure key: objective kind/id missing")
	}
	objectiveKind := normalizeKind(parts[idx])
	objectiveID := strings.TrimSpace(parts[idx+1])
	if objectiveKind == "" || objectiveID == "" {
		return "", nil, fmt.Errorf("invalid exposure key: objective kind/id invalid")
	}
	idx += 2

	out := &ExposureKeyParts{
		Type:          t,
		ObjectiveKind: objectiveKind,
		ObjectiveID:   objectiveID,
	}

	// Optional segments
	for idx < len(parts) {
		tag := strings.ToLower(parts[idx])
		switch tag {
		case "related", "rel":
			tag = "related"
			if idx+2 >= len(parts) {
				return "", nil, fmt.Errorf("invalid exposure key: related kind/id missing")
			}
			out.RelatedKind = normalizeKind(parts[idx+1])
			out.RelatedID = strings.TrimSpace(parts[idx+2])
			if out.RelatedKind == "" || out.RelatedID == "" {
				return "", nil, fmt.Errorf("invalid exposure key: related kind/id invalid")
			}
			idx += 3
		case "path":
			if idx+1 >= len(parts) {
				return "", nil, fmt.Errorf("invalid exposure key: path id missing")
			}
			out.PathID = strings.TrimSpace(parts[idx+1])
			if out.PathID == "" {
				return "", nil, fmt.Errorf("invalid exposure key: path id invalid")
			}
			idx += 2
		default:
			return "", nil, fmt.Errorf("invalid exposure key: unknown segment %q", parts[idx])
		}
	}

	canon, err := BuildExposureKey(out.Type, out.ObjectiveKind, out.ObjectiveID, out.RelatedKind, out.RelatedID, out.PathID)
	if err != nil {
		return "", nil, err
	}
	return canon, out, nil
}

// ExposurePathArtifact is a persisted artifact representing a specific exposure path.
// The canonical key is stored in the lens as a string, but full details live here.
type ExposurePathArtifact struct {
	ID        string `json:"id" firestore:"id"`
	TenantID  string `json:"tenantId" firestore:"tenantId"`
	ProjectID string `json:"projectId" firestore:"projectId"`
	LensID    string `json:"lensId" firestore:"lensId"`

	CanonicalKey string       `json:"canonicalKey" firestore:"canonicalKey"`
	PrefixKey    string       `json:"prefixKey" firestore:"prefixKey"`
	Type         ExposureType `json:"type" firestore:"type"`

	ObjectiveKind string `json:"objectiveKind" firestore:"objectiveKind"`
	ObjectiveID   string `json:"objectiveId" firestore:"objectiveId"`
	RelatedKind   string `json:"relatedKind,omitempty" firestore:"relatedKind,omitempty"`
	RelatedID     string `json:"relatedId,omitempty" firestore:"relatedId,omitempty"`
	PathID        string `json:"pathId,omitempty" firestore:"pathId,omitempty"`

	// Optional: materialized path details (can evolve later)
	NodeIDs []string `json:"nodeIds,omitempty" firestore:"nodeIds,omitempty"`
	EdgeIDs []string `json:"edgeIds,omitempty" firestore:"edgeIds,omitempty"`

	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`
}

func (a *ExposurePathArtifact) Validate() error {
	if a == nil {
		return fmt.Errorf("exposure path is nil")
	}
	if strings.TrimSpace(a.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(a.ProjectID) == "" {
		return fmt.Errorf("projectId is required")
	}
	if strings.TrimSpace(a.LensID) == "" {
		return fmt.Errorf("lensId is required")
	}
	if strings.TrimSpace(a.CanonicalKey) == "" {
		return fmt.Errorf("canonicalKey is required")
	}
	if !a.Type.Valid() {
		return fmt.Errorf("invalid exposure type: %q", string(a.Type))
	}
	if strings.TrimSpace(a.ObjectiveKind) == "" || strings.TrimSpace(a.ObjectiveID) == "" {
		return fmt.Errorf("objectiveKind/objectiveId are required")
	}
	return nil
}

// ExposureDocID returns a deterministic Firestore doc ID based on tenant/lens and canonical key.
// This avoids composite-index queries for "get by canonical key" and guarantees idempotent upserts.
func ExposureDocID(tenantID, lensID, canonicalKey string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(canonicalKey)))
	// 10 bytes is enough for low collision risk and keeps doc IDs short.
	short := hex.EncodeToString(sum[:10])
	return fmt.Sprintf("%s__%s__%s", strings.TrimSpace(tenantID), strings.TrimSpace(lensID), short)
}


