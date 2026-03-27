package workscope

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// WatchPolicyVersion bumps when WorkScope mapping from WatchSubject changes (idempotency contract).
const WatchPolicyVersion = "v1"

// FromWatchSubject maps a watch subject into a WorkScope for shared pipelines (hydration, future risk jobs).
func FromWatchSubject(subject *horizonmodels.WatchSubject) WorkScope {
	if subject == nil {
		return WorkScope{}
	}
	refs := horizonmodels.SimulationBaselineRefs{
		LensID: strings.TrimSpace(subject.LensID),
	}
	ap := strings.TrimSpace(subject.AnchorCanonicalPath)
	switch subject.SubjectKind {
	case horizonmodels.WatchSubjectKindLensUniverse:
		refs.ScopePaths = append(refs.ScopePaths, compactWatchPaths(subject.UniversePaths)...)
		if ap != "" {
			refs.ScopePaths = append(refs.ScopePaths, ap)
		}
	case horizonmodels.WatchSubjectKindFragment, horizonmodels.WatchSubjectKindExplicitTarget:
		if ap != "" {
			refs.ScopePaths = append(refs.ScopePaths, ap)
		}
		if id := strings.TrimSpace(subject.AnchorFragmentID); id != "" {
			refs.FragmentIDs = append(refs.FragmentIDs, id)
		}
	default:
		if ap != "" {
			refs.ScopePaths = append(refs.ScopePaths, ap)
		}
		refs.ScopePaths = append(refs.ScopePaths, compactWatchPaths(subject.UniversePaths)...)
	}
	return Normalize(FromBaselineRefs(strings.TrimSpace(subject.TenantID), strings.TrimSpace(subject.ProjectID), refs))
}

func compactWatchPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// WatchScopeIntentDigest is a stable digest of the subject's WorkScope before repo resolution (for job idempotency).
func WatchScopeIntentDigest(subject *horizonmodels.WatchSubject) string {
	return Digest(ResolvedFromScope(FromWatchSubject(subject)))
}
