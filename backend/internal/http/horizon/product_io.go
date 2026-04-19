package horizon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/materialize"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// NOTE (v1): Product IO hypothesis workflow materializes only PRODUCT fragments.
// HorizonFragment is primary; Entity is best-effort enrichment via link.

type productIOExtractRequest struct {
	// direction: "produces" | "consumes" | "both" (default "both")
	Direction string `json:"direction,omitempty"`
	// Optional free-form hint for the LLM (kept small).
	Hint string `json:"hint,omitempty"`
	// Optional language for labels (default "en")
	Lang string `json:"lang,omitempty"`
}

type productIOCandidate struct {
	ProductName string  `json:"productName"`
	Confidence  float64 `json:"confidence"` // 0..1
	Reason      string  `json:"reason,omitempty"`
}

type productIOExtractResponse struct {
	FragmentID string               `json:"fragmentId"`
	Direction  string               `json:"direction"`
	Produces   []productIOCandidate `json:"produces"`
	Consumes   []productIOCandidate `json:"consumes"`
}

type productIOApplyRequest struct {
	ObjectiveFragmentID string               `json:"objectiveFragmentId"`
	Produces            []productIOCandidate `json:"produces"`
	Consumes            []productIOCandidate `json:"consumes"`
}

type productIOApplyResponse struct {
	LensID    string                            `json:"lensId"`
	Objective string                            `json:"objectiveFragmentId"`
	Delta     *materialize.ProductIOApplyResult `json:"delta"`
}

// POST /api/horizon/fragments/{id}/product-io:extract
func (h *HorizonHandlers) ExtractProductIO(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	fragmentID = strings.TrimSpace(strings.TrimSuffix(fragmentID, "/product-io:extract"))
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	frag, err := h.service.Fragments().Get(r.Context(), fragmentID)
	if err != nil || frag == nil || frag.ID == "" {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	var req productIOExtractRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	dir := strings.ToLower(strings.TrimSpace(req.Direction))
	if dir == "" {
		dir = "both"
	}
	if dir != "both" && dir != "produces" && dir != "consumes" {
		http.Error(w, "Invalid direction", http.StatusBadRequest)
		return
	}
	lang := strings.TrimSpace(req.Lang)
	if lang == "" {
		lang = "en"
	}
	label := ""
	if frag.Labels != nil {
		label = strings.TrimSpace(frag.Labels[lang])
		if label == "" {
			label = strings.TrimSpace(frag.Labels["en"])
		}
	}
	if label == "" {
		label = strings.TrimSpace(frag.CanonicalPath)
	}

	prompt := buildProductIOExtractPrompt(frag, label, dir, strings.TrimSpace(req.Hint))
	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeDomainClassification, // structured extraction
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       7,
			MinContextWindow:         4000,
			MaxOutputSize:            1200,
		},
		Verifier: verifier,
	}

	raw, err := h.service.AIExecutor().ExecuteStructuredTask(r.Context(), task)
	if err != nil {
		http.Error(w, fmt.Sprintf("LLM extraction failed: %v", err), http.StatusInternalServerError)
		return
	}

	// ExecuteStructuredTask returns verifier-parsed result; marshal/unmarshal into typed struct.
	b, _ := json.Marshal(raw)
	var out struct {
		Produces []productIOCandidate `json:"produces"`
		Consumes []productIOCandidate `json:"consumes"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		http.Error(w, "Failed to parse LLM response", http.StatusInternalServerError)
		return
	}

	// Best-effort sanitize
	out.Produces = sanitizeProductCandidates(out.Produces)
	out.Consumes = sanitizeProductCandidates(out.Consumes)

	resp := productIOExtractResponse{
		FragmentID: fragmentID,
		Direction:  dir,
		Produces:   out.Produces,
		Consumes:   out.Consumes,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// POST /api/horizon/lenses/{lensId}/product-io:apply
func (h *HorizonHandlers) ApplyProductIO(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Extract lensID from path: /api/horizon/lenses/{lensId}/product-io:apply
	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	lensID = strings.TrimSpace(strings.TrimSuffix(lensID, "/product-io:apply"))
	if lensID == "" {
		http.Error(w, "Lens ID required", http.StatusBadRequest)
		return
	}

	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	var req productIOApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.ObjectiveFragmentID = strings.TrimSpace(req.ObjectiveFragmentID)
	if req.ObjectiveFragmentID == "" {
		http.Error(w, "objectiveFragmentId is required", http.StatusBadRequest)
		return
	}

	obj, err := h.service.Fragments().Get(r.Context(), req.ObjectiveFragmentID)
	if err != nil || obj == nil || obj.ID == "" {
		http.Error(w, "Objective fragment not found", http.StatusNotFound)
		return
	}

	// Materialize through a shared workflow that uses the same repos/rules as Scout.
	m := materialize.NewProductIOMaterializer(h.service.Fragments(), h.service.Edges())
	// Best-effort: reuse the same canonicalization/dedup resolver as Scout to keep paths stable.
	if se := h.service.Scout(); se != nil {
		m.WithCanonicalPathResolver(se.CanonicalPathResolver())
	}
	out, err := m.Apply(r.Context(), &materialize.ProductIOApplyRequest{
		TenantID:          "global",
		ProjectID:         lens.ProjectID,
		LensID:            lensID,
		ObjectiveFragment: obj,
		Produces:          toMat(req.Produces),
		Consumes:          toMat(req.Consumes),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(productIOApplyResponse{
		LensID:    lensID,
		Objective: obj.ID,
		Delta:     out,
	})
}

func toMat(in []productIOCandidate) []materialize.ProductIOCandidate {
	out := make([]materialize.ProductIOCandidate, 0, len(in))
	for _, c := range in {
		name := strings.TrimSpace(c.ProductName)
		if name == "" {
			continue
		}
		conf := c.Confidence
		if conf < 0 {
			conf = 0
		}
		if conf > 1 {
			conf = 1
		}
		out = append(out, materialize.ProductIOCandidate{
			ProductName: name,
			Confidence:  conf,
			Reason:      strings.TrimSpace(c.Reason),
		})
	}
	return out
}

func sanitizeProductCandidates(in []productIOCandidate) []productIOCandidate {
	seen := map[string]struct{}{}
	out := make([]productIOCandidate, 0, len(in))
	for _, c := range in {
		name := strings.TrimSpace(c.ProductName)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		conf := c.Confidence
		if conf < 0 {
			conf = 0
		}
		if conf > 1 {
			conf = 1
		}
		out = append(out, productIOCandidate{
			ProductName: name,
			Confidence:  conf,
			Reason:      strings.TrimSpace(c.Reason),
		})
	}
	return out
}

func buildProductIOExtractPrompt(f *horizonmodels.HorizonFragment, label, direction, hint string) string {
	// Keep it strict: only products, return JSON only.
	// We don't ask for suppliers yet in v1.
	return fmt.Sprintf(`You are an analyst extracting product relationships for a knowledge graph.

OBJECT:
{
  "fragmentId": %q,
  "canonicalPath": %q,
  "type": %q,
  "label": %q
}

TASK:
Infer which PRODUCTS this object produces and/or consumes.

CONSTRAINTS:
- Return ONLY PRODUCTS (tangible or intangible goods/services that are products in the context).
- Do NOT return organizations, facilities, markets, countries, or generic categories.
- If unsure, omit.
- Return confidence in [0,1].
- Output ONLY JSON, no markdown, no extra text.

DIRECTION: %q
HINT: %q

OUTPUT SCHEMA (exact):
{
  "produces": [
    { "productName": "string", "confidence": 0.0, "reason": "string" }
  ],
  "consumes": [
    { "productName": "string", "confidence": 0.0, "reason": "string" }
  ]
}
`, f.ID, strings.TrimSpace(f.CanonicalPath), horizonmodels.FormatClassSubtype(f.Class, f.Subtype), strings.TrimSpace(label), direction, hint)
}


