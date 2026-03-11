package contracts

import (
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func registerBuiltinHandlers(r *Registry) {
	// Handlers are registered in their respective files.
	// Keep this function to centralize built-in registration.
	if r == nil {
		return
	}
	for _, ct := range taxonomy.DomainContractTypes() {
		r.Register(NewDomainAnswerHandler(ct))
	}
	// Register specialized handlers after generic domain handlers so they win.
	r.Register(NewOwnershipStructureHandler())
	r.Register(NewCommonalityAnalysisHandler())
}
