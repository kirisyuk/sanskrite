package propagation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// ProfileLookup provides cached access to propagation profiles.
type ProfileLookup struct {
	profilesRepo *repo.PropagationProfilesRepository
	cache        map[string]*horizonmodels.PropagationProfile
	mu           sync.RWMutex
	initialized  bool
}

// NewProfileLookup creates a new ProfileLookup.
func NewProfileLookup(profilesRepo *repo.PropagationProfilesRepository) *ProfileLookup {
	return &ProfileLookup{
		profilesRepo: profilesRepo,
		cache:        make(map[string]*horizonmodels.PropagationProfile),
		initialized:  false,
	}
}

const chokepointAliasesEnv = "HORIZON_CHOKEPOINT_ALIASES_JSON"

// Initialize loads all propagation profiles from Firestore into cache.
// Should be called at startup.
func (p *ProfileLookup) Initialize(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.initialized {
		return nil
	}

	// Load all profiles from Firestore
	profiles, err := p.profilesRepo.GetAll(ctx)
	if err != nil {
		// Fallback to defaults if Firestore fails
		defaultProfiles := horizonmodels.DefaultPropagationProfiles()
		for key, profile := range defaultProfiles {
			p.cache[key] = &profile
		}
		p.initialized = true
		return fmt.Errorf("failed to load profiles from Firestore, using defaults: %w", err)
	}

	// Use Firestore profiles (they override defaults)
	for key, profile := range profiles {
		p.cache[key] = profile
	}

	// Ensure all default profiles exist (fill gaps)
	defaultProfiles := horizonmodels.DefaultPropagationProfiles()
	for key, profile := range defaultProfiles {
		if _, exists := p.cache[key]; !exists {
			p.cache[key] = &profile
		}
	}

	p.initialized = true
	return nil
}

// Get retrieves a propagation profile by axis key.
// Returns default profile if axis not found in cache.
func (p *ProfileLookup) Get(axisKey string) (*horizonmodels.PropagationProfile, error) {
	if !horizonmodels.IsValidAxis(axisKey) {
		return nil, fmt.Errorf("invalid axis: %s", axisKey)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	// Check cache first
	if profile, ok := p.cache[axisKey]; ok {
		return profile, nil
	}

	// Fallback to default
	defaultProfiles := horizonmodels.DefaultPropagationProfiles()
	if profile, ok := defaultProfiles[axisKey]; ok {
		return &profile, nil
	}

	return nil, fmt.Errorf("profile not found for axis: %s", axisKey)
}

// GetForSignal returns a profile adjusted by lightweight chokepoint policy hints.
// This is intentionally conservative and deterministic; deep policy semantics can be layered later.
func (p *ProfileLookup) GetForSignal(signal *horizonmodels.SignalDescriptor) (*horizonmodels.PropagationProfile, error) {
	if signal == nil {
		return nil, fmt.Errorf("signal is nil")
	}
	base, err := p.Get(signal.Axis)
	if err != nil {
		return nil, err
	}
	if !isStrategicChokepointPath(signal.LocationPath) {
		return base, nil
	}

	cp := *base
	cp.StrongEdges = append([]horizonmodels.StrongEdgeSpec(nil), base.StrongEdges...)
	cp.Transformations = append([]horizonmodels.TransformationRule(nil), base.Transformations...)

	switch strings.TrimSpace(signal.Axis) {
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		cp.DefaultDamping = minFloat(0.95, cp.DefaultDamping+0.10)
		if cp.DefaultMaxHops < 4 {
			cp.DefaultMaxHops = 4
		}
		for i := range cp.StrongEdges {
			cp.StrongEdges[i].Weight = minFloat(1.5, cp.StrongEdges[i].Weight*1.15)
		}
	case horizonmodels.AxisPhysicalFlowSupply:
		cp.DefaultDamping = minFloat(0.95, cp.DefaultDamping+0.08)
		if cp.DefaultMaxHops < 5 {
			cp.DefaultMaxHops = 5
		}
		for i := range cp.StrongEdges {
			cp.StrongEdges[i].Weight = minFloat(1.5, cp.StrongEdges[i].Weight*1.10)
		}
	}
	return &cp, nil
}

// Refresh reloads a specific profile from Firestore.
func (p *ProfileLookup) Refresh(ctx context.Context, axisKey string) error {
	if !horizonmodels.IsValidAxis(axisKey) {
		return fmt.Errorf("invalid axis: %s", axisKey)
	}

	profile, err := p.profilesRepo.Get(ctx, axisKey)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache[axisKey] = profile

	return nil
}

// RefreshAll reloads all profiles from Firestore.
func (p *ProfileLookup) RefreshAll(ctx context.Context) error {
	return p.Initialize(ctx)
}

func isStrategicChokepointPath(path string) bool {
	return strategicChokepointID(path) != ""
}

// IsChokepointFragment returns true if the fragment is a strategic chokepoint (strait, canal, pass, pipeline-corridor).
// Used for transit amplification during propagation.
func IsChokepointFragment(fragment *horizonmodels.HorizonFragment) bool {
	if fragment == nil {
		return false
	}
	path := strings.TrimSpace(fragment.CanonicalPath)
	switch fragment.Type {
	case horizonmodels.FragmentTypeOrganization, horizonmodels.FragmentTypeProgram:
		// Organizations and institutions may be strategically important, but they are not
		// geographic chokepoints just because noisy enrichment attached port/strait-style tags.
		return isStrategicChokepointPath(path)
	}
	gt := strings.ToLower(strings.TrimSpace(fragment.GeoFeatureType))
	switch gt {
	case "strait", "canal", "pass", "pipeline-corridor":
		return true
	}
	for _, t := range fragment.Tags {
		if strings.ToLower(strings.TrimSpace(t)) == "chokepoint" {
			return true
		}
	}
	// Fallback: path-based detection (for fragments without geoFeatureType)
	return isStrategicChokepointPath(path)
}

// TransitAmplify applies chokepoint transit amplification. Signal passing through a chokepoint
// is strengthened (bottleneck: disruption cascades with amplification). Returns amplitude clamped to 1.0.
func TransitAmplify(amplitude float64, axis string) float64 {
	if amplitude <= 0 {
		return amplitude
	}
	axis = strings.TrimSpace(axis)
	var mult float64
	switch axis {
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		mult = 1.25
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand:
		mult = 1.20
	case horizonmodels.AxisGeoConflict:
		mult = 1.15
	default:
		mult = 1.10
	}
	out := amplitude * mult
	if out > 1.0 {
		return 1.0
	}
	return out
}

func strategicChokepointID(path string) string {
	path = normalizeGeoPath(path)
	if path == "" {
		return ""
	}
	for canonicalID, aliases := range loadStrategicChokepointAliases() {
		for _, alias := range aliases {
			alias = normalizeGeoPath(alias)
			if alias == "" {
				continue
			}
			if strings.Contains(path, alias) {
				return canonicalID
			}
		}
	}
	return ""
}

func normalizeGeoPath(path string) string {
	path = strings.ToLower(strings.TrimSpace(path))
	path = strings.ReplaceAll(path, " ", "_")
	path = strings.ReplaceAll(path, "-", "_")
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	return strings.TrimSuffix(path, "/")
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

var (
	strategicChokepointAliasesOnce sync.Once
	strategicChokepointAliases     map[string][]string
)

func loadStrategicChokepointAliases() map[string][]string {
	strategicChokepointAliasesOnce.Do(func() {
		aliases := cloneChokepointAliases(defaultStrategicChokepointAliases)
		raw := strings.TrimSpace(os.Getenv(chokepointAliasesEnv))
		if raw == "" {
			strategicChokepointAliases = aliases
			return
		}
		var user map[string][]string
		if err := json.Unmarshal([]byte(raw), &user); err != nil {
			strategicChokepointAliases = aliases
			return
		}
		for k, arr := range user {
			key := normalizeGeoPath(k)
			if key == "" || len(arr) == 0 {
				continue
			}
			clean := make([]string, 0, len(arr))
			for _, a := range arr {
				a = strings.TrimSpace(a)
				if a == "" {
					continue
				}
				clean = append(clean, a)
			}
			if len(clean) == 0 {
				continue
			}
			aliases[key] = clean
		}
		strategicChokepointAliases = aliases
	})
	return strategicChokepointAliases
}

func cloneChokepointAliases(src map[string][]string) map[string][]string {
	out := make(map[string][]string, len(src))
	for k, v := range src {
		out[k] = append([]string{}, v...)
	}
	return out
}

var defaultStrategicChokepointAliases = map[string][]string{
	"chokepoint_hormuz": {
		"/loc/ir/hormuz",
		"/loc/ir/hormozgan/hormuz_strait",
		"/loc/ir/hormozgan/strait_of_hormuz",
	},
	"chokepoint_bab_el_mandeb": {
		"/loc/ye/bab-el-mandeb",
		"/loc/ye/taizz/bab_el_mandeb",
		"/loc/ye/taizz/strait_of_bab_el_mandeb",
	},
	"chokepoint_suez": {
		"/loc/eg/suez",
		"/loc/eg/al_ismailiyah/suez_canal",
		"/loc/eg/al_ismailiyah/suez",
	},
	"chokepoint_malacca": {
		"/loc/my/malacca",
		"/loc/my/melaka/strait_of_malacca",
		"/loc/my/melaka/malacca_strait",
	},
	"chokepoint_panama": {
		"/loc/pa/panama",
		"/loc/pa/panama/panama_canal",
	},
}
