package taxonomy

import "testing"

func TestCanonicalPathParts_Org(t *testing.T) {
	p, err := ParseCanonicalPathParts("/org/us/tesla-inc")
	if err != nil {
		t.Fatalf("ParseCanonicalPathParts(org) error: %v", err)
	}
	if p == nil || p.Namespace != "org" || p.Org == nil {
		t.Fatalf("unexpected parts: %#v", p)
	}
	if p.Org.Geo != "us" {
		t.Fatalf("geo=%q, want %q", p.Org.Geo, "us")
	}
	if p.Org.BaseSlug != "tesla" {
		t.Fatalf("baseSlug=%q, want %q", p.Org.BaseSlug, "tesla")
	}
	if p.Org.LegalForm != "inc" {
		t.Fatalf("legalForm=%q, want %q", p.Org.LegalForm, "inc")
	}
	if got, ok := p.BuildCanonicalPath(); !ok || got != "/org/us/tesla-inc" {
		t.Fatalf("BuildCanonicalPath()=(%q,%v), want (%q,true)", got, ok, "/org/us/tesla-inc")
	}
}

func TestCanonicalPathParts_OrgLegacy(t *testing.T) {
	p, err := ParseCanonicalPathParts("/org/tesla-inc")
	if err != nil {
		t.Fatalf("ParseCanonicalPathParts(org legacy) error: %v", err)
	}
	if p == nil || p.Namespace != "org" || p.Org == nil || !p.Org.Legacy {
		t.Fatalf("unexpected parts: %#v", p)
	}
	if got, ok := p.BuildCanonicalPath(); !ok || got != "/org/tesla-inc" {
		t.Fatalf("BuildCanonicalPath()=(%q,%v), want (%q,true)", got, ok, "/org/tesla-inc")
	}
}

func TestCanonicalPathParts_Gov(t *testing.T) {
	p, err := ParseCanonicalPathParts("/gov/us/sec")
	if err != nil {
		t.Fatalf("ParseCanonicalPathParts(gov) error: %v", err)
	}
	if p == nil || p.Namespace != "gov" || p.Gov == nil {
		t.Fatalf("unexpected parts: %#v", p)
	}
	if got, ok := p.BuildCanonicalPath(); !ok || got != "/gov/us/sec" {
		t.Fatalf("BuildCanonicalPath()=(%q,%v), want (%q,true)", got, ok, "/gov/us/sec")
	}
}

func TestCanonicalPathParts_ProductHierarchical(t *testing.T) {
	p, err := ParseCanonicalPathParts("/product/raw/mineral/extraction/lithium-ore")
	if err != nil {
		t.Fatalf("ParseCanonicalPathParts(product) error: %v", err)
	}
	if p == nil || p.Namespace != "product" || p.Product == nil {
		t.Fatalf("unexpected parts: %#v", p)
	}
	if p.Product.Shape != "hierarchical" {
		t.Fatalf("shape=%q, want %q", p.Product.Shape, "hierarchical")
	}
	if got, ok := p.BuildCanonicalPath(); !ok || got != "/product/raw/mineral/extraction/lithium-ore" {
		t.Fatalf("BuildCanonicalPath()=(%q,%v), want (%q,true)", got, ok, "/product/raw/mineral/extraction/lithium-ore")
	}
}

func TestCanonicalPathParts_MarketPreferred(t *testing.T) {
	p, err := ParseCanonicalPathParts("/market/global/technology/semiconductors")
	if err != nil {
		t.Fatalf("ParseCanonicalPathParts(market) error: %v", err)
	}
	if p == nil || p.Namespace != "market" || p.Market == nil {
		t.Fatalf("unexpected parts: %#v", p)
	}
	if got, ok := p.BuildCanonicalPath(); !ok || got != "/market/global/technology/semiconductors" {
		t.Fatalf("BuildCanonicalPath()=(%q,%v), want (%q,true)", got, ok, "/market/global/technology/semiconductors")
	}
}

func TestCanonicalPathParts_MarketExtended(t *testing.T) {
	// Tolerant parsing for deeper paths (e.g. /market/kz/investment/all/logistics)
	p, err := ParseCanonicalPathParts("/market/kz/investment/all/logistics")
	if err != nil {
		t.Fatalf("ParseCanonicalPathParts(market extended) error: %v", err)
	}
	if p == nil || p.Namespace != "market" || p.Market == nil {
		t.Fatalf("unexpected parts: %#v", p)
	}
	if p.Market.Geo != "kz" || p.Market.Domain != "investment" || p.Market.Vertical != "all/logistics" {
		t.Fatalf("Market parts: geo=%q domain=%q vertical=%q, want kz investment all/logistics",
			p.Market.Geo, p.Market.Domain, p.Market.Vertical)
	}
}

func TestCanonicalPathParts_ProductTail(t *testing.T) {
	// Example from prompts: product with model/SKU tail segment(s)
	p, err := ParseCanonicalPathParts("/product/finished/mechanical/manufacturing/excavator/cat-390f")
	if err != nil {
		t.Fatalf("ParseCanonicalPathParts(product tail) error: %v", err)
	}
	if p == nil || p.Namespace != "product" || p.Product == nil {
		t.Fatalf("unexpected parts: %#v", p)
	}
	if p.Product.Shape != "hierarchical" {
		t.Fatalf("shape=%q, want %q", p.Product.Shape, "hierarchical")
	}
	if len(p.Product.Tail) != 1 || p.Product.Tail[0] != "cat-390f" {
		t.Fatalf("tail=%v, want [cat-390f]", p.Product.Tail)
	}
	if got, ok := p.BuildCanonicalPath(); !ok || got != "/product/finished/mechanical/manufacturing/excavator/cat-390f" {
		t.Fatalf("BuildCanonicalPath()=(%q,%v), want (%q,true)", got, ok, "/product/finished/mechanical/manufacturing/excavator/cat-390f")
	}
}

func TestCanonicalPathParts_CommunityPreferred(t *testing.T) {
	p, err := ParseCanonicalPathParts("/community/ae/trade/hawala-dubai-network")
	if err != nil {
		t.Fatalf("ParseCanonicalPathParts(community) error: %v", err)
	}
	if p == nil || p.Namespace != "community" || p.Community == nil {
		t.Fatalf("unexpected parts: %#v", p)
	}
	if got, ok := p.BuildCanonicalPath(); !ok || got != "/community/ae/trade/hawala-dubai-network" {
		t.Fatalf("BuildCanonicalPath()=(%q,%v), want (%q,true)", got, ok, "/community/ae/trade/hawala-dubai-network")
	}
}

func TestCanonicalPathParts_CommunityLegacy(t *testing.T) {
	p, err := ParseCanonicalPathParts("/community/global/osint-researchers")
	if err != nil {
		t.Fatalf("ParseCanonicalPathParts(community legacy) error: %v", err)
	}
	if p == nil || p.Namespace != "community" || p.Community == nil || !p.Community.Legacy {
		t.Fatalf("unexpected parts: %#v", p)
	}
	if got, ok := p.BuildCanonicalPath(); !ok || got != "/community/global/osint-researchers" {
		t.Fatalf("BuildCanonicalPath()=(%q,%v), want (%q,true)", got, ok, "/community/global/osint-researchers")
	}
}

func TestCanonicalPathParts_Decision(t *testing.T) {
	p, err := ParseCanonicalPathParts("/decision/ai-chips/euv-export-control")
	if err != nil {
		t.Fatalf("ParseCanonicalPathParts(decision) error: %v", err)
	}
	if p == nil || p.Namespace != "decision" || p.Decision == nil {
		t.Fatalf("unexpected parts: %#v", p)
	}
	if got, ok := p.BuildCanonicalPath(); !ok || got != "/decision/ai-chips/euv-export-control" {
		t.Fatalf("BuildCanonicalPath()=(%q,%v), want (%q,true)", got, ok, "/decision/ai-chips/euv-export-control")
	}
}

func TestCanonicalPathParts_Mechanism(t *testing.T) {
	p, err := ParseCanonicalPathParts("/mechanism/regulatory/export-control")
	if err != nil {
		t.Fatalf("ParseCanonicalPathParts(mechanism) error: %v", err)
	}
	if p == nil || p.Namespace != "mechanism" || p.Mechanism == nil {
		t.Fatalf("unexpected parts: %#v", p)
	}
	if got, ok := p.BuildCanonicalPath(); !ok || got != "/mechanism/regulatory/export-control" {
		t.Fatalf("BuildCanonicalPath()=(%q,%v), want (%q,true)", got, ok, "/mechanism/regulatory/export-control")
	}
}
