package agents

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
)

func domainScaffoldRelationTypesPromptString() string {
	return edgesemantics.DomainScaffoldRelationTypesString()
}

func relationTypesPromptForDomainMode(mode string) string {
	types := contractsemantics.AllowedEdgeStringsForDomainMode(mode)
	if len(types) == 0 {
		return ""
	}
	return strings.Join(types, ", ")
}
