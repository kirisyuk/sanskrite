package pathquery

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
)

func TestPathTouchesFromCanonical(t *testing.T) {
	p := &pathbuilder.SupplyChainPath{
		Segments: []pathbuilder.PathSegment{{SupplierRef: "/org/a", CustomerRef: "/org/b"}},
	}
	if !pathTouchesFromCanonical(p, "/org/a") {
		t.Fatal()
	}
	if pathTouchesFromCanonical(p, "/org/z") {
		t.Fatal()
	}
	if !pathTouchesFromCanonical(&pathbuilder.SupplyChainPath{}, "") {
		t.Fatal("empty fromCP matches any")
	}
}
