package risk

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	horizonfeedback "github.com/igorkirisyuk/sanskrite/internal/horizon/feedback"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type ConcentrationItem struct {
	Key            string  `json:"key"`
	Count          int     `json:"count"`
	AvgScore       float64 `json:"avgScore"`
	MaxScore       float64 `json:"maxScore"`
	Representative string  `json:"representative,omitempty"`
}

type ConcentrationSnapshot struct {
	Polarity string              `json:"polarity"`
	ByAxis   []ConcentrationItem `json:"byAxis"`
	ByGeo    []ConcentrationItem `json:"byGeo"`
	ByMarket []ConcentrationItem `json:"byMarket"`
}

type ConcentrationAnalyzer struct {
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository
	feedback      horizonfeedback.Emitter
	mu            sync.Mutex
	seenHotspots  map[string]struct{}
}

func NewConcentrationAnalyzer(fragmentsRepo *horizonrepo.HorizonFragmentsRepository) *ConcentrationAnalyzer {
	return &ConcentrationAnalyzer{
		fragmentsRepo: fragmentsRepo,
		seenHotspots:  make(map[string]struct{}),
	}
}

func (a *ConcentrationAnalyzer) WithFeedbackEmitter(emitter horizonfeedback.Emitter) *ConcentrationAnalyzer {
	a.feedback = emitter
	return a
}

func (a *ConcentrationAnalyzer) Snapshot(ctx context.Context, tenantID, polarity string, limit int) (*ConcentrationSnapshot, error) {
	if a == nil || a.fragmentsRepo == nil {
		return &ConcentrationSnapshot{Polarity: polarity}, nil
	}
	if limit <= 0 {
		limit = 10
	}
	frags := make([]*horizonmodels.HorizonFragment, 0, 1000)
	// Legacy PROGRAM folds onto Class=org/Subtype=program; it is enumerated
	// implicitly via FragmentTypeOrganization (Class=org) below — there is
	// no separate ListByTypeAndTenant call for the subtype because the
	// repository keys on Class only and the concentration aggregator does
	// not need to distinguish subtypes.
	types := []FragmentType{
		FragmentTypeOrganization,
		FragmentTypeFacility,
		FragmentTypeMarket,
		FragmentTypeProduct,
		FragmentTypeGeoRegion,
	}
	for _, t := range types {
		items, err := a.fragmentsRepo.ListByTypeAndTenant(ctx, strings.TrimSpace(tenantID), t, 300)
		if err != nil {
			continue
		}
		frags = append(frags, items...)
	}
	axisAgg := map[string]*ConcentrationItem{}
	geoAgg := map[string]*ConcentrationItem{}
	marketAgg := map[string]*ConcentrationItem{}

	for _, f := range frags {
		if f == nil {
			continue
		}
		geo := geoFromFragment(f)
		marketKey := primaryMarketFromFragment(f)
		for metricPath, mv := range f.State {
			if !strings.HasPrefix(metricPath, "/metric/"+polarity+"/") {
				continue
			}
			axis := axisFromMetricPath(metricPath)
			updateAgg(axisAgg, axis, mv.Value, f.CanonicalPath)
			if geo != "" {
				updateAgg(geoAgg, geo, mv.Value, f.CanonicalPath)
			}
			if marketKey != "" {
				updateAgg(marketAgg, marketKey, mv.Value, f.CanonicalPath)
			}
		}
	}

	snapshot := &ConcentrationSnapshot{
		Polarity: polarity,
		ByAxis:   toSorted(axisAgg, limit),
		ByGeo:    toSorted(geoAgg, limit),
		ByMarket: toSorted(marketAgg, limit),
	}
	a.emitAttentionEscalations(ctx, snapshot)
	return snapshot, nil
}

func updateAgg(dst map[string]*ConcentrationItem, key string, score float64, rep string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	item, ok := dst[key]
	if !ok {
		dst[key] = &ConcentrationItem{
			Key:            key,
			Count:          1,
			AvgScore:       score,
			MaxScore:       score,
			Representative: strings.TrimSpace(rep),
		}
		return
	}
	item.AvgScore = ((item.AvgScore * float64(item.Count)) + score) / float64(item.Count+1)
	item.Count++
	if score > item.MaxScore {
		item.MaxScore = score
	}
}

func toSorted(src map[string]*ConcentrationItem, limit int) []ConcentrationItem {
	out := make([]ConcentrationItem, 0, len(src))
	for _, v := range src {
		if v == nil {
			continue
		}
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MaxScore == out[j].MaxScore {
			return out[i].Count > out[j].Count
		}
		return out[i].MaxScore > out[j].MaxScore
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func axisFromMetricPath(metricPath string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(metricPath), "/"), "/")
	if len(parts) < 4 {
		return ""
	}
	return parts[2] + "/" + parts[3]
}

func geoFromFragment(f *horizonmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	cp := strings.TrimSpace(f.CanonicalPath)
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "org", "gov", "loc", "market", "community":
		return strings.ToLower(strings.TrimSpace(parts[1]))
	default:
		return ""
	}
}

func primaryMarketFromFragment(f *horizonmodels.HorizonFragment) string {
	if f == nil || len(f.MarketSegments) == 0 {
		return ""
	}
	seg := strings.TrimSpace(f.MarketSegments[0])
	parts := strings.Split(strings.Trim(seg, "/"), "/")
	if len(parts) == 4 && parts[0] == "segment" {
		return "/market/" + parts[3] + "/" + parts[1] + "/" + parts[2]
	}
	return ""
}

func (a *ConcentrationAnalyzer) emitAttentionEscalations(ctx context.Context, snapshot *ConcentrationSnapshot) {
	if a == nil || a.feedback == nil || snapshot == nil {
		return
	}
	emitGroup := func(group string, items []ConcentrationItem) {
		for _, item := range items {
			// First-time hotspot escalation only.
			if item.MaxScore < 0.85 || item.Count < 3 {
				continue
			}
			key := strings.TrimSpace(snapshot.Polarity) + "|" + group + "|" + strings.TrimSpace(item.Key)
			a.mu.Lock()
			_, seen := a.seenHotspots[key]
			if !seen {
				a.seenHotspots[key] = struct{}{}
			}
			a.mu.Unlock()
			if seen {
				continue
			}
			_ = a.feedback.Emit(ctx, horizonfeedback.Event{
				Type:      "attention_escalation",
				Polarity:  strings.TrimSpace(snapshot.Polarity),
				Reason:    "first_time_concentration_hotspot",
				EmittedAt: time.Now().UTC(),
				Payload: map[string]any{
					"group":                 group,
					"key":                   strings.TrimSpace(item.Key),
					"count":                 item.Count,
					"maxScore":              item.MaxScore,
					"representative":        strings.TrimSpace(item.Representative),
					"forceExposurePathGate": true,
				},
			})
		}
	}
	emitGroup("axis", snapshot.ByAxis)
	emitGroup("geo", snapshot.ByGeo)
	emitGroup("market", snapshot.ByMarket)
}
