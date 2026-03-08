package repo

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestValidateForbiddenSyntheticPlaceholderForCreate(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "facility synthetic", path: "/fac/na/general-production", wantErr: true},
		{name: "org synthetic", path: "/org/na/retailer", wantErr: true},
		{name: "gov synthetic", path: "/gov/na/ministry-of-defense", wantErr: true},
		{name: "real facility unknown geo", path: "/fac/na/port-of-durban", wantErr: false},
		{name: "real org", path: "/org/us/intel", wantErr: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateForbiddenSyntheticPlaceholderForCreate(&models.HorizonFragment{
				CanonicalPath: tc.path,
			})
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.path)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.path, err)
			}
		})
	}
}
