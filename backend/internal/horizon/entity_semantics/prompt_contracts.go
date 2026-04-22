package entity_semantics

import (
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

var promptRelationTargetTypes = []ah.FragmentClass{
	ah.FragmentClassCommunity,
	ah.FragmentClassDecision,
	ah.FragmentClassEvidence,
	ah.FragmentClassGeo,
	ah.FragmentClassMarket,
	ah.FragmentClassMechanism,
	ah.FragmentClassMobileAsset,
	ah.FragmentClassOrg,
	ah.FragmentClassPerson,
	ah.FragmentClassProduct,
	ah.FragmentClassProject,
	ah.FragmentClassRegime,
	ah.FragmentClassRoute,
	ah.FragmentClassSite,
	ah.FragmentClassVesselClass,
}

// PromptRelationTargetTypes returns the stable relation target type list used by Scout/Research prompts.
// This is the single source of truth for "typical objects" in discovered relations.
func PromptRelationTargetTypes() []string {
	out := make([]string, 0, len(promptRelationTargetTypes))
	for _, t := range promptRelationTargetTypes {
		tt := strings.TrimSpace(string(t))
		if tt == "" {
			continue
		}
		out = append(out, tt)
	}
	return out
}

// PromptRelationTargetTypesPipeList returns a prompt-friendly enum string:
// "org" | "site" | ...
func PromptRelationTargetTypesPipeList() string {
	items := PromptRelationTargetTypes()
	if len(items) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, fmt.Sprintf("%q", item))
	}
	return strings.Join(quoted, " | ")
}

// PromptRelationTargetTypesCSV returns a prompt-friendly comma-separated list.
func PromptRelationTargetTypesCSV() string {
	return strings.Join(PromptRelationTargetTypes(), ", ")
}
