package horizon

import "testing"

func TestParseLensSimulationRoute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path   string
		lensID string
		kind   lensSimulationRouteKind
	}{
		{
			path:   "/api/horizon/lenses/l8dOd17uKYYTKD0CI9hy/simulations",
			lensID: "l8dOd17uKYYTKD0CI9hy",
			kind:   lensSimulationRouteList,
		},
		{
			path:   "/api/horizon/lenses/l8dOd17uKYYTKD0CI9hy/simulations/latest",
			lensID: "l8dOd17uKYYTKD0CI9hy",
			kind:   lensSimulationRouteLatest,
		},
		{
			path:   "/api/horizon/lenses/l8dOd17uKYYTKD0CI9hy/simulations/latest/bundle-view",
			lensID: "l8dOd17uKYYTKD0CI9hy",
			kind:   lensSimulationRouteBundleView,
		},
		{
			path:   "/api/horizon/lenses/l8dOd17uKYYTKD0CI9hy/simulations/latest/overlays",
			lensID: "l8dOd17uKYYTKD0CI9hy",
			kind:   lensSimulationRouteOverlays,
		},
		{
			path:   "/api/horizon/lenses/l8dOd17uKYYTKD0CI9hy/simulations/older",
			lensID: "",
			kind:   lensSimulationRouteInvalid,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			lensID, kind := parseLensSimulationRoute(tc.path)
			if lensID != tc.lensID || kind != tc.kind {
				t.Fatalf("parseLensSimulationRoute(%q) = (%q, %q), want (%q, %q)", tc.path, lensID, kind, tc.lensID, tc.kind)
			}
		})
	}
}
