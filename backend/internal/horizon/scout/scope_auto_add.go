package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func shouldAutoAddRelationTargetToScope(targetPath string, targetType models.FragmentType, relationAllowed bool) bool {
	if !relationAllowed {
		return false
	}
	if strings.TrimSpace(targetPath) == "" {
		return false
	}
	return isScopeEligibleEntityType(targetType)
}
