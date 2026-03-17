package watchruntime

import (
	"context"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type LensAttentionView struct {
	LensID      string                    `json:"lensId"`
	LensName    string                    `json:"lensName,omitempty"`
	Subjects    []*LensAttentionSubject   `json:"subjects"`
	Feed        []*LensAttentionFeedEntry `json:"feed"`
	GeneratedAt time.Time                 `json:"generatedAt"`
}

type LensAttentionSubject struct {
	SubjectID           string                       `json:"subjectId"`
	SubjectKind         string                       `json:"subjectKind"`
	DisplayLabel        string                       `json:"displayLabel"`
	AnchorCanonicalPath string                       `json:"anchorCanonicalPath"`
	AnchorFragmentID    string                       `json:"anchorFragmentId,omitempty"`
	Status              string                       `json:"status"`
	DomainModes         []string                     `json:"domainModes,omitempty"`
	Roles               []string                     `json:"roles,omitempty"`
	MacroAgentClass     string                       `json:"macroAgentClass,omitempty"`
	UniversePaths       []string                     `json:"universePaths,omitempty"`
	ArtifactSummary     map[string]any               `json:"artifactSummary,omitempty"`
	SourceMixPolicy     map[string]any               `json:"sourceMixPolicy,omitempty"`
	LastRun             *horizonmodels.ScopeAgentRun `json:"lastRun,omitempty"`
	LastTrigger         *horizonmodels.WatchTrigger  `json:"lastTrigger,omitempty"`
	PromotedProposalIDs []string                     `json:"promotedProposalIds,omitempty"`
	CoverageGaps        []string                     `json:"coverageGaps,omitempty"`
}

type LensAttentionFeedEntry struct {
	Kind           string         `json:"kind"`
	SubjectID      string         `json:"subjectId"`
	Title          string         `json:"title"`
	Summary        string         `json:"summary,omitempty"`
	CanonicalPaths []string       `json:"canonicalPaths,omitempty"`
	ProposalID     string         `json:"proposalId,omitempty"`
	Decision       string         `json:"decision,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

func (s *Service) LensAttention(ctx context.Context, tenantID, lensID string, limit int) (*LensAttentionView, error) {
	if s == nil || s.lensesRepo == nil || s.subjectsRepo == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	lens, err := s.lensesRepo.Get(ctx, strings.TrimSpace(lensID))
	if err != nil || lens == nil {
		return nil, err
	}
	subjectLimit := limit
	if subjectLimit <= 0 || subjectLimit < 100 {
		subjectLimit = 100
	}
	subjects, err := s.subjectsRepo.ListByLens(ctx, strings.TrimSpace(firstNonEmpty(tenantID, lens.TenantID, "global")), strings.TrimSpace(lensID), subjectLimit)
	if err != nil {
		return nil, err
	}
	subjectViews := make([]*LensAttentionSubject, 0, len(subjects))
	feed := make([]*LensAttentionFeedEntry, 0, len(subjects)*2)
	for _, subject := range subjects {
		if subject == nil || strings.TrimSpace(subject.ID) == "" {
			continue
		}
		detail, err := s.SubjectDetail(ctx, subject.ID, 12)
		if err != nil || detail == nil {
			continue
		}
		subjectView, subjectFeed := s.buildLensAttentionSubject(detail)
		subjectViews = append(subjectViews, subjectView)
		feed = append(feed, subjectFeed...)
	}
	sort.SliceStable(subjectViews, func(i, j int) bool {
		if subjectViews[i].LastRun == nil {
			return false
		}
		if subjectViews[j].LastRun == nil {
			return true
		}
		return subjectViews[i].LastRun.UpdatedAt.After(subjectViews[j].LastRun.UpdatedAt)
	})
	sort.SliceStable(feed, func(i, j int) bool { return feed[i].CreatedAt.After(feed[j].CreatedAt) })
	if limit > 0 && len(feed) > limit {
		feed = feed[:limit]
	}
	return &LensAttentionView{
		LensID:      strings.TrimSpace(lens.ID),
		LensName:    strings.TrimSpace(lens.Name),
		Subjects:    subjectViews,
		Feed:        feed,
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func (s *Service) buildLensAttentionSubject(detail *SubjectDetail) (*LensAttentionSubject, []*LensAttentionFeedEntry) {
	if detail == nil || detail.Subject == nil {
		return nil, nil
	}
	subject := detail.Subject
	view := &LensAttentionSubject{
		SubjectID:           strings.TrimSpace(subject.ID),
		SubjectKind:         string(subject.SubjectKind),
		DisplayLabel:        strings.TrimSpace(subject.DisplayLabel),
		AnchorCanonicalPath: strings.TrimSpace(subject.AnchorCanonicalPath),
		AnchorFragmentID:    strings.TrimSpace(subject.AnchorFragmentID),
		Status:              string(subject.Status),
		DomainModes:         append([]string(nil), subject.DomainModes...),
		Roles:               append([]string(nil), subject.Roles...),
		MacroAgentClass:     strings.TrimSpace(asString(subject.Metadata["macroAgentClass"])),
		UniversePaths:       compactUnique(subject.UniversePaths),
		SourceMixPolicy:     mapFromAny(subject.Metadata["sourceMixPolicy"]),
		CoverageGaps:        []string{},
	}
	if artifactKey := strings.TrimSpace(asString(subject.Metadata["artifactCanonicalKey"])); artifactKey != "" {
		view.ArtifactSummary = map[string]any{
			"artifactCanonicalKey":   artifactKey,
			"materializedArtifactId": asString(subject.Metadata["materializedArtifactId"]),
			"materializedKind":       asString(subject.Metadata["materializedKind"]),
			"legIndices":             subject.Metadata["legIndices"],
			"backingCanonicalPaths":  subject.Metadata["backingCanonicalPaths"],
		}
	}
	if len(detail.Runs) > 0 {
		view.LastRun = detail.Runs[0]
	}
	if len(detail.Triggers) > 0 {
		view.LastTrigger = detail.Triggers[0]
	}
	feed := make([]*LensAttentionFeedEntry, 0, len(detail.Triggers)+4)
	for _, trigger := range detail.Triggers {
		if trigger == nil {
			continue
		}
		feed = append(feed, &LensAttentionFeedEntry{
			Kind:           "trigger",
			SubjectID:      subject.ID,
			Title:          strings.TrimSpace(firstNonEmpty(trigger.TriggerReason, string(trigger.TriggerType))),
			Summary:        string(trigger.Decision),
			CanonicalPaths: compactUnique(trigger.CanonicalPaths),
			CreatedAt:      trigger.CreatedAt,
			Metadata: map[string]any{
				"triggerType":    string(trigger.TriggerType),
				"decision":       string(trigger.Decision),
				"decisionReason": strings.TrimSpace(trigger.DecisionReason),
				"matchMode":      string(trigger.MatchMode),
			},
		})
	}
	proposalSeen := map[string]struct{}{}
	for _, run := range detail.Runs {
		if run == nil {
			continue
		}
		for _, proposalID := range run.ProducedProposalIDs {
			proposalID = strings.TrimSpace(proposalID)
			if proposalID == "" {
				continue
			}
			if _, ok := proposalSeen[proposalID]; ok {
				continue
			}
			proposalSeen[proposalID] = struct{}{}
			proposal, decision := s.proposalWithDecision(context.Background(), proposalID)
			if proposal == nil {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(proposal.ProposalType), string(horizonmodels.AgentProposalTypeCoverageGap)) {
				view.CoverageGaps = append(view.CoverageGaps, strings.TrimSpace(proposal.Explanation))
			}
			if decision != nil && strings.EqualFold(strings.TrimSpace(decision.Decision), string(horizonmodels.ArbitrationDecisionAccept)) {
				view.PromotedProposalIDs = append(view.PromotedProposalIDs, proposal.ID)
			}
			feed = append(feed, &LensAttentionFeedEntry{
				Kind:           "proposal",
				SubjectID:      subject.ID,
				Title:          strings.TrimSpace(firstNonEmpty(stringValueFromAnyMap(proposal.Payload, "title"), proposal.ProposalType)),
				Summary:        strings.TrimSpace(firstNonEmpty(stringValueFromAnyMap(proposal.Payload, "summary"), proposal.Explanation)),
				CanonicalPaths: compactUnique(proposal.TargetCanonicalPaths),
				ProposalID:     proposal.ID,
				Decision:       decisionString(decision),
				CreatedAt:      proposal.CreatedAt,
				Metadata: map[string]any{
					"proposalType":    proposal.ProposalType,
					"confidence":      proposal.Confidence,
					"macroAgentClass": asString(proposal.Payload["macroAgentClass"]),
				},
			})
		}
	}
	view.CoverageGaps = compactUnique(view.CoverageGaps)
	view.PromotedProposalIDs = compactUnique(view.PromotedProposalIDs)
	return view, feed
}

func (s *Service) proposalWithDecision(ctx context.Context, proposalID string) (*horizonmodels.AgentProposal, *horizonmodels.ArbitrationDecision) {
	if s == nil || s.proposalsRepo == nil || strings.TrimSpace(proposalID) == "" {
		return nil, nil
	}
	proposal, err := s.proposalsRepo.Get(ctx, strings.TrimSpace(proposalID))
	if err != nil || proposal == nil {
		return nil, nil
	}
	if s.decisionsRepo == nil {
		return proposal, nil
	}
	decisions, err := s.decisionsRepo.ListByProposalID(ctx, proposalID)
	if err != nil || len(decisions) == 0 {
		return proposal, nil
	}
	return proposal, decisions[0]
}

func decisionString(decision *horizonmodels.ArbitrationDecision) string {
	if decision == nil {
		return ""
	}
	return strings.TrimSpace(decision.Decision)
}

func mapFromAny(raw any) map[string]any {
	if typed, ok := raw.(map[string]any); ok {
		return typed
	}
	return nil
}

func stringValueFromAnyMap(payload map[string]any, key string) string {
	if len(payload) == 0 {
		return ""
	}
	return strings.TrimSpace(asString(payload[key]))
}
