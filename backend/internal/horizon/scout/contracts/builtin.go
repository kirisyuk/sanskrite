package contracts

func registerBuiltinHandlers(r *Registry) {
	// Handlers are registered in their respective files.
	// Keep this function to centralize built-in registration.
	if r == nil {
		return
	}
	r.Register(NewOwnershipStructureHandler())
	r.Register(NewCommonalityAnalysisHandler())
}
