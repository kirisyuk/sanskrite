package agents

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
)

func domainScaffoldRelationTypesPromptString() string {
	return edgesemantics.DomainScaffoldRelationTypesString()
}

func relationTypesPromptForDomainMode(mode string) string {
	types := edgesemantics.EdgeAllowlistStrings([]string{mode})
	if len(types) == 0 {
		return ""
	}
	return strings.Join(types, ", ")
}
