package scout

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
)

func jobAllowsDeferredScopeAdd(job *horizonmodels.ScoutingJob, canonicalPath string) bool {
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" {
		return false
	}
	if job == nil || job.State == nil {
		return true
	}
	return workscope.StateAllowsScopeAutoAdd(job.State, canonicalPath)
}
