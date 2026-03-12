package pathbuilder

import "testing"

func TestDefaultBuildPathOptions_ProductTransformationIsRequiredByDefault(t *testing.T) {
	opts := DefaultBuildPathOptions()
	if !opts.RequireProductTransformation {
		t.Fatalf("RequireProductTransformation must be true by default")
	}
}
