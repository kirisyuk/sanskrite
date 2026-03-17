package contracts

import "testing"

func TestRegisterBuiltinHandlers_SpecializedHandlersWin(t *testing.T) {
	r := NewRegistry()
	registerBuiltinHandlers(r)

	if _, ok := r.Get("ownership-structure").(*OwnershipStructureHandler); !ok {
		t.Fatalf("expected ownership-structure to resolve to OwnershipStructureHandler, got %T", r.Get("ownership-structure"))
	}
	if _, ok := r.Get("control-map").(*ControlMapHandler); !ok {
		t.Fatalf("expected control-map to resolve to ControlMapHandler, got %T", r.Get("control-map"))
	}
}
