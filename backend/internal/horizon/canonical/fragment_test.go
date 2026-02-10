package canonical

import "testing"

func TestNormalizeCanonicalPathForPersist_GeoStrict(t *testing.T) {
	got := NormalizeCanonicalPathForPersist("/loc/ma")
	want := "/loc/ma/na/na"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNormalizeCanonicalPathForPersist_OrgSlugify(t *testing.T) {
	got := NormalizeCanonicalPathForPersist("/org/CN/Changan Automobile LTD")
	want := "/org/cn/changan-automobile-ltd"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNormalizeCanonicalPathForPersist_MarketLegacyGeoDefault(t *testing.T) {
	got := NormalizeCanonicalPathForPersist("/market/Battery Materials")
	want := "/market/global/battery-materials"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNormalizeCanonicalPathForPersist_CommunityShape(t *testing.T) {
	got := NormalizeCanonicalPathForPersist("/community/AE/Dynasty/ Al Nahyan ")
	want := "/community/ae/dynasty/al-nahyan"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
