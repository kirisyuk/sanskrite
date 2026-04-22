package scout

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// canonicalPathPrefixes maps the recognised canonical-path prefixes onto the
// AH (Class, Subtype) tuple they imply. Keys are kept ordered by prefix length
// implicitly via the switch in InferClassSubtypeFromCanonicalPath; this var is
// only used to derive the dual single-class lookup below.
//
// Prefixes whose subtype is non-empty correspond to legacy subtype-carrying
// families folded under an AH parent class (media_channel → org,
// deposit/supply_link → site, affected_zone → geo, domain/sphere → evidence).
// Callers that only need the AH class can use InferFragmentTypeFromCanonicalPath
// (this discards the subtype hint); callers that need to distinguish, for
// example, /deposit/ from /fac/ must use InferClassSubtypeFromCanonicalPath.

// InferClassSubtypeFromCanonicalPath returns a best-effort (Class, Subtype)
// tuple based on canonical path prefix. The subtype hint is non-empty only for
// historical subtype families that fold into a shared parent AH class
// (media_channel, deposit, supply_link, affected_zone, domain, sphere).
//
// The function is intentionally conservative; unknown / non-canonical paths
// fall back to (org, ""). Trim and case-fold the input upstream if needed.
func InferClassSubtypeFromCanonicalPath(path string) horizonmodels.ClassSubtype {
	p := strings.TrimSpace(path)
	switch {
	case strings.HasPrefix(p, "/domain/"):
		return horizonmodels.CS(ah.FragmentClassEvidence, "domain")
	case strings.HasPrefix(p, "/evidence/"):
		return horizonmodels.CS(ah.FragmentClassEvidence, "")
	case strings.HasPrefix(p, "/sphere/"):
		return horizonmodels.CS(ah.FragmentClassEvidence, "sphere")
	case strings.HasPrefix(p, "/decision/"):
		return horizonmodels.CS(ah.FragmentClassDecision, "")
	case strings.HasPrefix(p, "/mechanism/"):
		return horizonmodels.CS(ah.FragmentClassMechanism, "")
	case strings.HasPrefix(p, "/product/"):
		return horizonmodels.CS(ah.FragmentClassProduct, "")
	case strings.HasPrefix(p, "/project/"):
		return horizonmodels.CS(ah.FragmentClassProject, "")
	case strings.HasPrefix(p, "/market/"):
		return horizonmodels.CS(ah.FragmentClassMarket, "")
	case strings.HasPrefix(p, "/fac/"):
		return horizonmodels.CS(ah.FragmentClassSite, "")
	case strings.HasPrefix(p, "/loc/"), strings.HasPrefix(p, "/region/"):
		return horizonmodels.CS(ah.FragmentClassGeo, "")
	case strings.HasPrefix(p, "/person/"):
		return horizonmodels.CS(ah.FragmentClassPerson, "")
	case strings.HasPrefix(p, "/media/"):
		return horizonmodels.CS(ah.FragmentClassOrg, "media_channel")
	case strings.HasPrefix(p, "/deposit/"):
		return horizonmodels.CS(ah.FragmentClassSite, "deposit")
	case strings.HasPrefix(p, "/track/"):
		return horizonmodels.CS(ah.FragmentClassMobileAsset, "")
	case strings.HasPrefix(p, "/supply-link/"):
		return horizonmodels.CS(ah.FragmentClassSite, "supply_link")
	case strings.HasPrefix(p, "/community/"):
		return horizonmodels.CS(ah.FragmentClassCommunity, "")
	case strings.HasPrefix(p, "/zone/"):
		return horizonmodels.CS(ah.FragmentClassGeo, "affected_zone")
	case strings.HasPrefix(p, "/org/"):
		return horizonmodels.CS(ah.FragmentClassOrg, "")
	case strings.HasPrefix(p, "/gov/"):
		return horizonmodels.CS(ah.FragmentClassOrg, "gov")
	default:
		return horizonmodels.CS(ah.FragmentClassOrg, "")
	}
}

// InferFragmentTypeFromCanonicalPath returns a best-effort AH FragmentClass
// based on canonical path prefix. The subtype is discarded; callers that need
// to distinguish (e.g.) /deposit/ from /fac/ should use
// InferClassSubtypeFromCanonicalPath instead.
//
// Behaviour for legacy collapsed types:
//
//	/media/       -> org   (subtype=media_channel)
//	/deposit/     -> site  (subtype=deposit)
//	/supply-link/ -> site  (subtype=supply_link)
//	/zone/        -> geo   (subtype=affected_zone)
//	/project/     -> project
//	/domain/      -> evidence (subtype=domain)
//	/evidence/    -> evidence
//	/sphere/      -> evidence (subtype=sphere)
//	/gov/         -> org   (subtype=gov)
func InferFragmentTypeFromCanonicalPath(path string) FragmentType {
	return InferClassSubtypeFromCanonicalPath(path).Class
}
