package pathbuilder

import "testing"

func TestDefaultBuildPathOptions_ProductTransformationIsSoftByDefault(t *testing.T) {
	opts := DefaultBuildPathOptions()
	if opts.RequireProductTransformation {
		t.Fatalf("RequireProductTransformation must be false by default")
	}
}
