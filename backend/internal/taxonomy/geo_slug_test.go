package taxonomy

import "testing"

func TestExtractGeoSlugFromCanonicalPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/org/us/tesla", want: "us"},
		{path: "/fac/ae/jebel-ali-port", want: "ae"},
		{path: "/market/gcc/logistics", want: "gcc"},
		{path: "/zone/tr-sy/military_conflict/northern-syria", want: "tr-sy"},
		{path: "/domain/v1/supply-chain/product/lng/geo/qa", want: "qa"},
		{path: "/domain/v1/bilateral-relations/geo/sa/geo/ae", want: "ae-sa"},
		{path: "/product/raw/mineral/extraction/lithium-ore", want: ""},
	}
	for _, tt := range tests {
		if got := ExtractGeoSlugFromCanonicalPath(tt.path); got != tt.want {
			t.Fatalf("ExtractGeoSlugFromCanonicalPath(%q)=%q, want %q", tt.path, got, tt.want)
		}
	}
}
