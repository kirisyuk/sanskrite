package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var (
	projectID   = flag.String("project", "nv-horizon", "GCP project ID")
	limit       = flag.Int("limit", 100, "Max zones to audit (0 = all)")
	model       = flag.String("model", "gemini-2.5-flash-lite", "LLM model (e.g. gemini-2.5-flash-lite, gemini-3-flash-preview, gpt-5-nano, grok-4-1-fast-non-reasoning)")
	outPath     = flag.String("out", "", "Output JSONL file (default: stdout)")
	includeZero = flag.Bool("include-zero", false, "Include zones with radiusKm=0 (suggest radius)")
	onlyZero    = flag.Bool("only-zero", false, "Audit only zones with radiusKm=0")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	fs, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("Firestore: %v", err)
	}
	defer fs.Close()

	zones, err := listZoneFragmentsWithRadius(ctx, fs, *limit, *includeZero, *onlyZero)
	if err != nil {
		log.Fatalf("list zones: %v", err)
	}
	if *onlyZero {
		log.Printf("Found %d AFFECTED_ZONE fragments with radiusKm=0", len(zones))
	} else if *includeZero {
		log.Printf("Found %d AFFECTED_ZONE fragments (including radiusKm=0)", len(zones))
	} else {
		log.Printf("Found %d AFFECTED_ZONE fragments with radiusKm > 0", len(zones))
	}
	if len(zones) == 0 {
		return
	}

	exec, mm := newExecutor(strings.TrimSpace(*projectID))
	if exec == nil || mm == nil {
		log.Fatal("AI executor not configured (GEMINI_API_KEY or vertex)")
	}

	var out *os.File
	if p := strings.TrimSpace(*outPath); p != "" {
		out, err = os.Create(p)
		if err != nil {
			log.Fatalf("create out file: %v", err)
		}
		defer out.Close()
	} else {
		out = os.Stdout
	}

	enc := json.NewEncoder(out)
	if out == os.Stdout {
		enc.SetIndent("", "  ")
	}

	for i, z := range zones {
		assessment, err := evaluateRadius(ctx, exec, mm, z)
		if err != nil {
			log.Printf("[%d/%d] %s: LLM error: %v", i+1, len(zones), z.CanonicalPath, err)
			enc.Encode(map[string]interface{}{
				"canonicalPath": z.CanonicalPath,
				"radiusKm":      z.RadiusKm,
				"label":         z.Labels["en"],
				"error":         err.Error(),
			})
			continue
		}
		row := map[string]interface{}{
			"canonicalPath": z.CanonicalPath,
			"radiusKm":      z.RadiusKm,
			"label":         z.Labels["en"],
			"conditionType": extractConditionType(z.CanonicalPath),
			"verdict":       assessment.Verdict,
			"confidence":    assessment.Confidence,
			"reason":        assessment.Reason,
			"suggestedKm":   assessment.SuggestedKm,
		}
		if err := enc.Encode(row); err != nil {
			log.Printf("encode: %v", err)
		}
		if (i+1)%5 == 0 {
			log.Printf("Audited %d/%d", i+1, len(zones))
		}
	}
	log.Printf("Done. Audited %d zones", len(zones))
}

func listZoneFragmentsWithRadius(ctx context.Context, db *firestore.Client, limit int, includeZero, onlyZero bool) ([]*models.HorizonFragment, error) {
	col := db.Collection("horizon_fragments")
	q := col.
		Where("tenantId", "==", "global").
		Where("canonicalPath", ">=", "/zone/").
		Where("canonicalPath", "<", "/zone/\ufffd").
		OrderBy("canonicalPath", firestore.Asc).
		Limit(5000)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.HorizonFragment, 0, len(docs))
	for _, d := range docs {
		var f models.HorizonFragment
		if err := d.DataTo(&f); err != nil {
			continue
		}
		if f.ID == "" {
			f.ID = d.Ref.ID
		}
		if onlyZero {
			if f.RadiusKm > 0 {
				continue
			}
		} else if !includeZero && f.RadiusKm <= 0 {
			continue
		}
		out = append(out, &f)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func extractConditionType(cp string) string {
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) >= 3 && parts[0] == "zone" {
		return parts[2]
	}
	return ""
}

type radiusAssessment struct {
	Verdict     string  `json:"verdict"`     // "adequate" | "overly_broad" | "uncertain" | "suggest_radius"
	Confidence  float64 `json:"confidence"` // 0-1
	Reason      string  `json:"reason"`
	SuggestedKm float64 `json:"suggestedKm,omitempty"` // 0 = no suggestion
}

func evaluateRadius(ctx context.Context, exec *ai.AITaskExecutor, _ *ai.AIModelManager, z *models.HorizonFragment) (*radiusAssessment, error) {
	label := strings.TrimSpace(z.Labels["en"])
	if label == "" {
		label = z.CanonicalPath
	}
	ct := extractConditionType(z.CanonicalPath)
	desc := strings.TrimSpace(z.Description)
	if len(desc) > 500 {
		desc = desc[:500] + "..."
	}

	hasRadius := z.RadiusKm > 0
	var prompt string
	if hasRadius {
		prompt = fmt.Sprintf(`You are an analyst auditing geographic zone radius estimates for a risk/exposure system.

ZONE: %s
Canonical path: %s
Condition type: %s
Current radius: %.1f km
Description: %s

Task: Evaluate whether this radius is ADEQUATE (evidence-based, conservative) or OVERLY BROAD (exaggerated, speculative).
Consider: typical conflict zone extent, disaster-affected area, known geographic boundaries. Be conservative—prefer smaller, verifiable estimates.

Return JSON:
{
  "verdict": "adequate" | "overly_broad" | "uncertain",
  "confidence": 0.0-1.0,
  "reason": "Brief explanation",
  "suggestedKm": 0 or a more conservative estimate if overly_broad
}

Rules:
- adequate: radius seems evidence-based and reasonable for this zone
- overly_broad: radius appears exaggerated; suggest a smaller value if you can justify it
- uncertain: not enough context; do not suggest
- suggestedKm: only when verdict is overly_broad and you have a defensible alternative; else 0`,
			label, z.CanonicalPath, ct, z.RadiusKm, desc)
	} else {
		prompt = fmt.Sprintf(`You are an analyst auditing geographic zone radius estimates for a risk/exposure system.

ZONE: %s
Canonical path: %s
Condition type: %s
Current radius: 0 km (missing/unknown)
Description: %s

Task: Can you suggest an evidence-based radius for this zone? Consider: typical conflict zone extent, disaster-affected area, known geographic boundaries. Be conservative—prefer smaller, verifiable estimates.

Return JSON:
{
  "verdict": "suggest_radius" | "adequate" | "uncertain",
  "confidence": 0.0-1.0,
  "reason": "Brief explanation",
  "suggestedKm": 0 or your suggested radius in km (only when verdict is suggest_radius)
}

Rules:
- suggest_radius: you have enough context to propose a conservative radius; set suggestedKm > 0
- adequate: 0 is fine (zone is point-like, administrative, or radius truly unknown); suggestedKm=0
- uncertain: not enough context to suggest; suggestedKm=0`,
			label, z.CanonicalPath, ct, desc)
	}

	verifier := ai.NewGenericVerifier(func(a radiusAssessment) error {
		valid := map[string]bool{"adequate": true, "overly_broad": true, "uncertain": true, "suggest_radius": true}
		if !valid[a.Verdict] {
			return fmt.Errorf("invalid verdict: %s", a.Verdict)
		}
		return nil
	})
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			PreferredModel:     strings.TrimSpace(*model),
			MinContextWindow:   500,
			MaxOutputSize:     1024,
			MinCapabilityScore: 6,
		},
		Verifier: verifier,
	}

	llmCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	parsed, _, err := exec.ExecuteStructuredTaskWithModel(llmCtx, task)
	if err != nil {
		return nil, err
	}
	a := parsed.(radiusAssessment)
	return &a, nil
}

func newExecutor(projectID string) (*ai.AITaskExecutor, *ai.AIModelManager) {
	ctx := context.Background()
	mm := ai.NewAIModelManager()
	services := make([]ai.AIService, 0, 3)

	// Gemini (primary)
	geminiKey := loadGeminiKey(ctx, projectID)
	if geminiKey == "" {
		log.Printf("WARNING: GEMINI_API_KEY not found in env or Secret Manager")
		return nil, nil
	}
	_ = os.Setenv("GEMINI_API_KEY", geminiKey)
	services = append(services, ai.NewGeminiService(geminiKey, mm))

	// OpenAI (fallback)
	openAIKey := loadOpenAIKey(ctx, projectID)
	if openAIKey != "" {
		_ = os.Setenv("OPENAI_API_KEY", openAIKey)
		services = append(services, ai.NewOpenAIService(openAIKey, mm))
	} else {
		log.Printf("INFO: OpenAI API key not found; OpenAI provider will not be registered")
	}

	// Grok (fallback)
	grokKey := loadGrokKey(ctx, projectID)
	if grokKey != "" {
		_ = os.Setenv("GROK_API_KEY", grokKey)
		services = append(services, ai.NewGrokService(grokKey, mm))
	} else {
		log.Printf("INFO: Grok API key not found; Grok provider will not be registered")
	}

	log.Printf("horizon_audit_zone_radii: registered %d AI providers (Gemini + optional OpenAI/Grok)", len(services))
	return ai.NewAITaskExecutor(services, mm), mm
}

func loadGeminiKey(ctx context.Context, projectID string) string {
	if v := strings.TrimSpace(os.Getenv("GEMINI_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("VERTEX_API_KEY")); v != "" {
		return v
	}
	for _, name := range []string{"gemini_api_key", "GEMINI_API_KEY", "vertex_api_key", "VERTEX_API_KEY"} {
		if v, err := gcp.LoadSecret(ctx, projectID, name); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func loadOpenAIKey(ctx context.Context, projectID string) string {
	if v := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); v != "" {
		return v
	}
	for _, name := range []string{"openai_api_key", "OPENAI_API_KEY"} {
		if v, err := gcp.LoadSecret(ctx, projectID, name); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func loadGrokKey(ctx context.Context, projectID string) string {
	if v := strings.TrimSpace(os.Getenv("GROK_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("XAI_API_KEY")); v != "" {
		return v
	}
	for _, name := range []string{"grok_api_key", "GROK_API_KEY", "xai_api_key", "XAI_API_KEY"} {
		if v, err := gcp.LoadSecret(ctx, projectID, name); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
