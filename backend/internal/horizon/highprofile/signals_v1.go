package highprofile

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func (c *Calculator) computeDynastySignals(
	hp *models.HighProfile,
	frag *models.HorizonFragment,
	inEdges []*models.DependencyEdge,
	outEdges []*models.DependencyEdge,
	neighborByID map[string]*models.HorizonFragment,
) {
	if hp == nil || frag == nil {
		return
	}

	isDynasty := frag.Type == models.FragmentTypeCommunity && strings.EqualFold(strings.TrimSpace(frag.CommunityType), "dynasty")
	isTribe := frag.Type == models.FragmentTypeCommunity && strings.EqualFold(strings.TrimSpace(frag.CommunityType), "tribe")
	if isDynasty || isTribe {
		// "Power" of dynasty: does it control/own/regulate/administer other entities?
		best := 0.0
		ev := make([]scoredEdge, 0)
		for _, e := range outEdges {
			if e == nil {
				continue
			}
			switch e.Type {
			case models.DependencyTypeControls, models.DependencyTypeOwns, models.DependencyTypeAdministers, models.DependencyTypeRegulates:
				// strong "power" edges
			default:
				continue
			}
			target := neighborByID[strings.TrimSpace(e.TargetFragmentID)]
			targetPath := ""
			if target != nil {
				targetPath = strings.TrimSpace(target.CanonicalPath)
			}
			score := clamp01(e.Properties.Criticality)
			if score > best {
				best = score
			}
			ev = append(ev, scoredEdge{edge: e, score: score, neighbor: targetPath})
		}
		if best > 0 {
			edgeIDs, neighborPaths := topKEvidence(ev, c.maxEvidenceK)
			hp.Signals["dynasty_power"] = models.HighProfileSignal{
				Score:      best,
				Level:      levelFromScore(best),
				Confidence: best,
				Evidence: &models.HighProfileEvidence{
					EdgeIDs:        edgeIDs,
					NeighborPaths:  neighborPaths,
					Note:           "Dynasty/tribe has direct power edges (controls/owns/administers/regulates).",
				},
			}
		}
	}

	// Influence ON this fragment by dynasty/tribe.
	best := 0.0
	ev := make([]scoredEdge, 0)
	for _, e := range inEdges {
		if e == nil {
			continue
		}
		src := neighborByID[strings.TrimSpace(e.SourceFragmentID)]
		if src == nil || src.Type != models.FragmentTypeCommunity {
			continue
		}
		st := strings.ToLower(strings.TrimSpace(src.CommunityType))
		if st != "dynasty" && st != "tribe" {
			continue
		}

		weight := 0.0
		switch e.Type {
		case models.DependencyTypeControls, models.DependencyTypeAdministers, models.DependencyTypeRegulates:
			weight = 1.0
		case models.DependencyTypeOwns:
			weight = 0.9
		case models.DependencyTypeInvestsIn:
			weight = 0.7
		case models.DependencyTypeAffiliatedWith:
			weight = 0.6
		default:
			continue
		}

		srcPath := strings.TrimSpace(src.CanonicalPath)
		score := clamp01(weight * clamp01(e.Properties.Criticality))
		if score > best {
			best = score
		}
		ev = append(ev, scoredEdge{edge: e, score: score, neighbor: srcPath})
	}
	if best > 0 {
		edgeIDs, neighborPaths := topKEvidence(ev, c.maxEvidenceK)
		hp.Signals["dynasty_influence"] = models.HighProfileSignal{
			Score:      best,
			Level:      levelFromScore(best),
			Confidence: best,
			Evidence: &models.HighProfileEvidence{
				EdgeIDs:       edgeIDs,
				NeighborPaths: neighborPaths,
				Note:          "Entity has incoming power ties from dynasty/tribe community.",
			},
		}
	}
}

func (c *Calculator) computeRegulatorySignals(
	hp *models.HighProfile,
	frag *models.HorizonFragment,
	inEdges []*models.DependencyEdge,
	outEdges []*models.DependencyEdge,
	neighborByID map[string]*models.HorizonFragment,
) {
	if hp == nil || frag == nil {
		return
	}

	isGov := strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/gov/")
	isRegulatorByType := false
	switch strings.ToLower(strings.TrimSpace(frag.GovType)) {
	case "regulator", "authority", "commission", "agency", "bureau", "service":
		isRegulatorByType = true
	}

	// is_regulator: gov-ish node has outgoing REGULATES edges.
	if isGov || isRegulatorByType {
		best := 0.0
		ev := make([]scoredEdge, 0)
		for _, e := range outEdges {
			if e == nil || e.Type != models.DependencyTypeRegulates {
				continue
			}
			t := neighborByID[strings.TrimSpace(e.TargetFragmentID)]
			tp := ""
			if t != nil {
				tp = strings.TrimSpace(t.CanonicalPath)
			}
			score := clamp01(e.Properties.Criticality)
			if score > best {
				best = score
			}
			ev = append(ev, scoredEdge{edge: e, score: score, neighbor: tp})
		}
		if best > 0 {
			edgeIDs, neighborPaths := topKEvidence(ev, c.maxEvidenceK)
			hp.Signals["is_regulator"] = models.HighProfileSignal{
				Score:      best,
				Level:      levelFromScore(best),
				Confidence: best,
				Evidence: &models.HighProfileEvidence{
					EdgeIDs:       edgeIDs,
					NeighborPaths: neighborPaths,
					Note:          "Government/regulator node has outgoing REGULATES edges.",
				},
			}
		}
	}

	// regulated_exposure: incoming REGULATES edges from gov-ish nodes.
	best := 0.0
	ev := make([]scoredEdge, 0)
	for _, e := range inEdges {
		if e == nil || e.Type != models.DependencyTypeRegulates {
			continue
		}
		src := neighborByID[strings.TrimSpace(e.SourceFragmentID)]
		if src == nil {
			continue
		}
		// Require /gov/ namespace for "hard" regulatory exposure.
		if !strings.HasPrefix(strings.TrimSpace(src.CanonicalPath), "/gov/") {
			continue
		}
		sp := strings.TrimSpace(src.CanonicalPath)
		score := clamp01(e.Properties.Criticality)
		if score > best {
			best = score
		}
		ev = append(ev, scoredEdge{edge: e, score: score, neighbor: sp})
	}
	if best > 0 {
		edgeIDs, neighborPaths := topKEvidence(ev, c.maxEvidenceK)
		hp.Signals["regulated_exposure"] = models.HighProfileSignal{
			Score:      best,
			Level:      levelFromScore(best),
			Confidence: best,
			Evidence: &models.HighProfileEvidence{
				EdgeIDs:       edgeIDs,
				NeighborPaths: neighborPaths,
				Note:          "Entity is directly regulated by a government body.",
			},
		}
	}
}

func (c *Calculator) computeMarketSignals(
	hp *models.HighProfile,
	frag *models.HorizonFragment,
	inEdges []*models.DependencyEdge,
	outEdges []*models.DependencyEdge,
	neighborByID map[string]*models.HorizonFragment,
) {
	if hp == nil || frag == nil {
		return
	}

	// market_dominance: outgoing DOMINATES → MARKET.
	best := 0.0
	ev := make([]scoredEdge, 0)
	for _, e := range outEdges {
		if e == nil || e.Type != models.DependencyTypeDominates {
			continue
		}
		t := neighborByID[strings.TrimSpace(e.TargetFragmentID)]
		tp := ""
		if t != nil {
			tp = strings.TrimSpace(t.CanonicalPath)
		}
		score := clamp01(e.Properties.Criticality)
		if score > best {
			best = score
		}
		ev = append(ev, scoredEdge{edge: e, score: score, neighbor: tp})
	}
	if best > 0 {
		edgeIDs, neighborPaths := topKEvidence(ev, c.maxEvidenceK)
		hp.Signals["market_dominance"] = models.HighProfileSignal{
			Score:      best,
			Level:      levelFromScore(best),
			Confidence: best,
			Evidence: &models.HighProfileEvidence{
				EdgeIDs:       edgeIDs,
				NeighborPaths: neighborPaths,
				Note:          "Entity has DOMINATES relationships to one or more markets.",
			},
		}
	}

	// For MARKET nodes: has_dominant_players if there are incoming DOMINATES.
	if frag.Type == models.FragmentTypeMarket {
		best = 0
		ev = ev[:0]
		for _, e := range inEdges {
			if e == nil || e.Type != models.DependencyTypeDominates {
				continue
			}
			src := neighborByID[strings.TrimSpace(e.SourceFragmentID)]
			sp := ""
			if src != nil {
				sp = strings.TrimSpace(src.CanonicalPath)
			}
			score := clamp01(e.Properties.Criticality)
			if score > best {
				best = score
			}
			ev = append(ev, scoredEdge{edge: e, score: score, neighbor: sp})
		}
		if best > 0 {
			edgeIDs, neighborPaths := topKEvidence(ev, c.maxEvidenceK)
			hp.Signals["has_dominant_players"] = models.HighProfileSignal{
				Score:      best,
				Level:      levelFromScore(best),
				Confidence: best,
				Evidence: &models.HighProfileEvidence{
					EdgeIDs:       edgeIDs,
					NeighborPaths: neighborPaths,
					Note:          "Market has one or more dominant players (incoming DOMINATES).",
				},
			}
		}
	}
}

func (c *Calculator) computeChokepointSignals(
	hp *models.HighProfile,
	frag *models.HorizonFragment,
	inEdges []*models.DependencyEdge,
	outEdges []*models.DependencyEdge,
	neighborByID map[string]*models.HorizonFragment,
) {
	if hp == nil || frag == nil {
		return
	}
	// Reserved for future v1 extensions (e.g. outgoing chokepoints); keep signature stable.
	_ = outEdges

	// chokepoint_proxy: many high-criticality incoming supply/dependency edges.
	// This is an intentionally cheap proxy (not full centrality).
	count := 0
	bestCrit := 0.0
	ev := make([]scoredEdge, 0)
	for _, e := range inEdges {
		if e == nil {
			continue
		}
		switch e.Type {
		case models.DependencyTypeSuppliesTo, models.DependencyTypeDependsOn, models.DependencyTypeProduces,
			models.DependencyTypeTransformsInto, models.DependencyTypeExtractsFrom:
			// ok
		default:
			continue
		}
		if e.Properties.Criticality < 0.7 {
			continue
		}
		count++
		if e.Properties.Criticality > bestCrit {
			bestCrit = e.Properties.Criticality
		}
		src := neighborByID[strings.TrimSpace(e.SourceFragmentID)]
		sp := ""
		if src != nil {
			sp = strings.TrimSpace(src.CanonicalPath)
		}
		ev = append(ev, scoredEdge{edge: e, score: clamp01(e.Properties.Criticality), neighbor: sp})
	}

	if count > 0 {
		// Normalize count (soft cap at 5).
		score := clamp01((float64(count) / 5.0) * clamp01(bestCrit))
		edgeIDs, neighborPaths := topKEvidence(ev, c.maxEvidenceK)
		hp.Signals["chokepoint_proxy"] = models.HighProfileSignal{
			Score:      score,
			Level:      levelFromScore(score),
			Confidence: clamp01(bestCrit),
			Evidence: &models.HighProfileEvidence{
				EdgeIDs:       edgeIDs,
				NeighborPaths: neighborPaths,
				Note:          "Proxy: many high-criticality incoming supply/dependency edges (potential chokepoint).",
			},
		}
	}

	// low_substitutability_dependency: incoming dependency edges with substitutability ~= 0 and high criticality.
	best := 0.0
	ev2 := make([]scoredEdge, 0)
	for _, e := range inEdges {
		if e == nil {
			continue
		}
		// Use only edges where substitutability qualifier is present (0..1).
		sub := e.GetSubstitutability()
		if sub <= 0 {
			continue
		}
		if sub > 0.2 {
			continue
		}
		if e.Properties.Criticality < 0.7 {
			continue
		}
		// More irreplaceable => higher score.
		score := clamp01((1.0 - sub) * clamp01(e.Properties.Criticality))
		if score > best {
			best = score
		}
		src := neighborByID[strings.TrimSpace(e.SourceFragmentID)]
		sp := ""
		if src != nil {
			sp = strings.TrimSpace(src.CanonicalPath)
		}
		ev2 = append(ev2, scoredEdge{edge: e, score: score, neighbor: sp})
	}
	if best > 0 {
		edgeIDs, neighborPaths := topKEvidence(ev2, c.maxEvidenceK)
		hp.Signals["low_substitutability_dependency"] = models.HighProfileSignal{
			Score:      best,
			Level:      levelFromScore(best),
			Confidence: best,
			Evidence: &models.HighProfileEvidence{
				EdgeIDs:       edgeIDs,
				NeighborPaths: neighborPaths,
				Note:          "Incoming dependency has low substitutability and high criticality (hard-to-replace).",
			},
		}
	}
}

