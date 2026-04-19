package scout

import (
	"strings"

)

func shouldAutoAddRelationTargetToScope(targetPath string, targetType FragmentType, relationAllowed bool) bool {
	if !relationAllowed {
		return false
	}
	if strings.TrimSpace(targetPath) == "" {
		return false
	}
	return isScopeEligibleEntityType(targetType)
}
