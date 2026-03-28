package models

import (
	"strings"
	"testing"
	"time"
)

func TestBuildSupplyLinkCanonicalPath(t *testing.T) {
	tests := []struct {
		name        string
		tenantID    string
		supplierRef *string
		productRef  *string
		customerRef *string
		wantErr     bool
		wantPath    string
	}{
		{
			name:        "valid supply link with all refs",
			tenantID:    "tenant-123",
			supplierRef: strPtr("/org/albemarle"),
			productRef:  strPtr("/product/lithium-carbonate"),
			customerRef: strPtr("/org/catl"),
			wantErr:     false,
			wantPath:    "/supply-link/tenant-123/" + buildSupplyLinkEndpointToken("natural", strPtr("/org/albemarle")) + "/" + buildSupplyLinkEndpointToken("", strPtr("/product/lithium-carbonate")) + "/" + buildSupplyLinkEndpointToken("end-consumer", strPtr("/org/catl")),
		},
		{
			name:        "valid supply link without supplier",
			tenantID:    "tenant-123",
			supplierRef: nil,
			productRef:  strPtr("/product/lithium-carbonate"),
			customerRef: strPtr("/org/catl"),
			wantErr:     false,
			wantPath:    "/supply-link/tenant-123/" + buildSupplyLinkEndpointToken("natural", nil) + "/" + buildSupplyLinkEndpointToken("", strPtr("/product/lithium-carbonate")) + "/" + buildSupplyLinkEndpointToken("end-consumer", strPtr("/org/catl")),
		},
		{
			name:        "valid supply link without customer",
			tenantID:    "tenant-123",
			supplierRef: strPtr("/org/albemarle"),
			productRef:  strPtr("/product/lithium-carbonate"),
			customerRef: nil,
			wantErr:     false,
			wantPath:    "/supply-link/tenant-123/" + buildSupplyLinkEndpointToken("natural", strPtr("/org/albemarle")) + "/" + buildSupplyLinkEndpointToken("", strPtr("/product/lithium-carbonate")) + "/" + buildSupplyLinkEndpointToken("end-consumer", nil),
		},
		{
			name:        "valid: global tenant (assumption)",
			tenantID:    "global",
			supplierRef: strPtr("/org/albemarle"),
			productRef:  strPtr("/product/lithium-carbonate"),
			customerRef: strPtr("/org/catl"),
			wantErr:     false,
			wantPath:    "/supply-link/global/" + buildSupplyLinkEndpointToken("natural", strPtr("/org/albemarle")) + "/" + buildSupplyLinkEndpointToken("", strPtr("/product/lithium-carbonate")) + "/" + buildSupplyLinkEndpointToken("end-consumer", strPtr("/org/catl")),
		},
		{
			name:        "error: empty tenant",
			tenantID:    "",
			supplierRef: strPtr("/org/albemarle"),
			productRef:  strPtr("/product/lithium-carbonate"),
			customerRef: strPtr("/org/catl"),
			wantErr:     true,
		},
		{
			name:        "error: missing productRef",
			tenantID:    "tenant-123",
			supplierRef: strPtr("/org/albemarle"),
			productRef:  nil,
			customerRef: strPtr("/org/catl"),
			wantErr:     true,
		},
		{
			name:        "valid with facility refs",
			tenantID:    "tenant-123",
			supplierRef: strPtr("/fac/us/albemarle-plant-1"),
			productRef:  strPtr("/product/lithium-carbonate"),
			customerRef: strPtr("/fac/cn/catl-factory-1"),
			wantErr:     false,
			wantPath:    "/supply-link/tenant-123/" + buildSupplyLinkEndpointToken("natural", strPtr("/fac/us/albemarle-plant-1")) + "/" + buildSupplyLinkEndpointToken("", strPtr("/product/lithium-carbonate")) + "/" + buildSupplyLinkEndpointToken("end-consumer", strPtr("/fac/cn/catl-factory-1")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := BuildSupplyLinkCanonicalPath(tt.tenantID, tt.supplierRef, tt.productRef, tt.customerRef)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildSupplyLinkCanonicalPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if path == "" {
					t.Errorf("BuildSupplyLinkCanonicalPath() returned empty path")
					return
				}
				if tt.wantPath != "" && path != tt.wantPath {
					t.Errorf("BuildSupplyLinkCanonicalPath() = %v, want %v", path, tt.wantPath)
				}
			}
		})
	}
}

func TestValidateSupplyLinkFields(t *testing.T) {
	tests := []struct {
		name     string
		fragment *HorizonFragment
		wantErr  bool
	}{
		{
			name: "valid supply link (facility refs only)",
			fragment: &HorizonFragment{
				Type:        FragmentTypeSupplyLink,
				TenantID:    "tenant-123",
				ProductRef:  strPtr("/product/lithium-carbonate"),
				SupplierRef: strPtr("/fac/us/albemarle-kings-mountain-plant"),
				CustomerRef: strPtr("/fac/cn/catl-ningde-factory"),
			},
			wantErr: false,
		},
		{
			name: "error: wrong type",
			fragment: &HorizonFragment{
				Type:     FragmentTypeOrganization,
				TenantID: "tenant-123",
			},
			wantErr: true,
		},
		{
			name: "valid: global tenant (assumption)",
			fragment: &HorizonFragment{
				Type:        FragmentTypeSupplyLink,
				TenantID:    "global",
				ProductRef:  strPtr("/product/lithium-carbonate"),
				SupplierRef: strPtr("/fac/us/albemarle-kings-mountain-plant"),
			},
			wantErr: false,
		},
		{
			name: "error: missing productRef",
			fragment: &HorizonFragment{
				Type:     FragmentTypeSupplyLink,
				TenantID: "tenant-123",
			},
			wantErr: true,
		},
		{
			name: "error: invalid productRef path",
			fragment: &HorizonFragment{
				Type:       FragmentTypeSupplyLink,
				TenantID:   "tenant-123",
				ProductRef: strPtr("/org/invalid"),
			},
			wantErr: true,
		},
		{
			name: "error: invalid supplierRef path",
			fragment: &HorizonFragment{
				Type:        FragmentTypeSupplyLink,
				TenantID:    "tenant-123",
				ProductRef:  strPtr("/product/lithium-carbonate"),
				SupplierRef: strPtr("/product/invalid"),
			},
			wantErr: true,
		},
		{
			name: "error: both supplier and customer missing",
			fragment: &HorizonFragment{
				Type:       FragmentTypeSupplyLink,
				TenantID:   "tenant-123",
				ProductRef: strPtr("/product/lithium-carbonate"),
			},
			wantErr: true,
		},
		{
			name: "valid: only supplier",
			fragment: &HorizonFragment{
				Type:        FragmentTypeSupplyLink,
				TenantID:    "tenant-123",
				ProductRef:  strPtr("/product/lithium-carbonate"),
				SupplierRef: strPtr("/fac/us/albemarle-kings-mountain-plant"),
			},
			wantErr: false,
		},
		{
			name: "valid: only customer",
			fragment: &HorizonFragment{
				Type:        FragmentTypeSupplyLink,
				TenantID:    "tenant-123",
				ProductRef:  strPtr("/product/lithium-carbonate"),
				CustomerRef: strPtr("/fac/cn/catl-ningde-factory"),
			},
			wantErr: false,
		},
		{
			name: "valid: org as supplier (full chain org→fac→product→fac→org)",
			fragment: &HorizonFragment{
				Type:        FragmentTypeSupplyLink,
				TenantID:    "tenant-123",
				ProductRef:  strPtr("/product/lithium-carbonate"),
				SupplierRef: strPtr("/org/albemarle"),
				CustomerRef: strPtr("/fac/cn/catl-ningde-factory"),
			},
			wantErr: false,
		},
		{
			name: "valid: org as customer (full chain org→fac→product→fac→org)",
			fragment: &HorizonFragment{
				Type:        FragmentTypeSupplyLink,
				TenantID:    "tenant-123",
				ProductRef:  strPtr("/product/lithium-carbonate"),
				SupplierRef: strPtr("/fac/us/albemarle-kings-mountain-plant"),
				CustomerRef: strPtr("/org/catl"),
			},
			wantErr: false,
		},
		{
			name: "valid: gov as supplier/customer",
			fragment: &HorizonFragment{
				Type:        FragmentTypeSupplyLink,
				TenantID:    "tenant-123",
				ProductRef:  strPtr("/product/lithium-carbonate"),
				SupplierRef: strPtr("/gov/us/department-of-energy"),
				CustomerRef: strPtr("/gov/eu/european-commission"),
			},
			wantErr: false,
		},
		{
			name: "valid: with transportViaRefs",
			fragment: &HorizonFragment{
				Type:             FragmentTypeSupplyLink,
				TenantID:         "tenant-123",
				ProductRef:       strPtr("/product/lithium-carbonate"),
				SupplierRef:      strPtr("/fac/us/albemarle-kings-mountain-plant"),
				CustomerRef:      strPtr("/fac/cn/catl-ningde-factory"),
				TransportViaRefs: []string{"/fac/cl/antofagasta-port", "/fac/cn/shanghai-port"},
			},
			wantErr: false,
		},
		{
			name: "error: invalid transportViaRefs path",
			fragment: &HorizonFragment{
				Type:             FragmentTypeSupplyLink,
				TenantID:         "tenant-123",
				ProductRef:       strPtr("/product/lithium-carbonate"),
				SupplierRef:      strPtr("/org/albemarle"),
				CustomerRef:      strPtr("/org/catl"),
				TransportViaRefs: []string{"/org/invalid-port"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateSupplyLinkFields(tt.fragment); (err != nil) != tt.wantErr {
				t.Errorf("ValidateSupplyLinkFields() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildSupplyLinkRouteVariantKey(t *testing.T) {
	first := BuildSupplyLinkRouteVariantKey([]string{
		"/fac/sea/strait-of-hormuz-terminal",
		"/fac/eg/suez-terminal",
	})
	second := BuildSupplyLinkRouteVariantKey([]string{
		"/fac/sea/strait-of-hormuz-terminal",
		"/fac/eg/suez-terminal",
	})
	other := BuildSupplyLinkRouteVariantKey([]string{
		"/fac/za/cape-of-good-hope-terminal",
	})
	if first == "" || first == "direct" {
		t.Fatalf("expected hashed via variant key, got %q", first)
	}
	if first != second {
		t.Fatalf("expected stable variant key, got %q and %q", first, second)
	}
	if first == other {
		t.Fatalf("expected different variant keys for different via sequences")
	}
}

func TestMergeSupplyLinkRouteVariants_PreservesDistinctRoutes(t *testing.T) {
	left := []SupplyLinkRouteVariant{
		NewSupplyLinkRouteVariant([]string{"/fac/eg/suez-terminal"}, "", 0.72),
	}
	right := []SupplyLinkRouteVariant{
		NewSupplyLinkRouteVariant([]string{"/fac/za/cape-of-good-hope-terminal"}, "", 0.81),
	}
	merged := MergeSupplyLinkRouteVariants(left, right)
	if len(merged) != 2 {
		t.Fatalf("variants=%d, want 2", len(merged))
	}
	primary := PrimarySupplyLinkRouteVariant(merged)
	if primary == nil {
		t.Fatal("expected primary variant")
	}
	if len(primary.TransportViaRefs) != 1 || primary.TransportViaRefs[0] != "/fac/za/cape-of-good-hope-terminal" {
		t.Fatalf("primary via=%v, want cape route", primary.TransportViaRefs)
	}
}

func TestIsSupplyLink(t *testing.T) {
	tests := []struct {
		name     string
		fragment *HorizonFragment
		want     bool
	}{
		{
			name: "is supply link",
			fragment: &HorizonFragment{
				Type: FragmentTypeSupplyLink,
			},
			want: true,
		},
		{
			name: "not supply link",
			fragment: &HorizonFragment{
				Type: FragmentTypeOrganization,
			},
			want: false,
		},
		{
			name:     "nil fragment",
			fragment: nil,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSupplyLink(tt.fragment); got != tt.want {
				t.Errorf("IsSupplyLink() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetSupplyLinkKey(t *testing.T) {
	tests := []struct {
		name     string
		fragment *HorizonFragment
		want     string
	}{
		{
			name: "valid supply link key",
			fragment: &HorizonFragment{
				Type:        FragmentTypeSupplyLink,
				TenantID:    "tenant-123",
				SupplierRef: strPtr("/org/albemarle"),
				ProductRef:  strPtr("/product/lithium-carbonate"),
				CustomerRef: strPtr("/org/catl"),
			},
			want: "tenant-123:/org/albemarle:/product/lithium-carbonate:/org/catl",
		},
		{
			name: "supply link without supplier",
			fragment: &HorizonFragment{
				Type:        FragmentTypeSupplyLink,
				TenantID:    "tenant-123",
				ProductRef:  strPtr("/product/lithium-carbonate"),
				CustomerRef: strPtr("/org/catl"),
			},
			want: "tenant-123:natural:/product/lithium-carbonate:/org/catl",
		},
		{
			name: "not supply link",
			fragment: &HorizonFragment{
				Type: FragmentTypeOrganization,
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetSupplyLinkKey(tt.fragment); got != tt.want {
				t.Errorf("GetSupplyLinkKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeSlug(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple slug",
			input: "albemarle",
			want:  "albemarle",
		},
		{
			name:  "with spaces",
			input: "Albemarle Corporation",
			want:  "albemarle-corporation",
		},
		{
			name:  "with underscores",
			input: "lithium_carbonate",
			want:  "lithium-carbonate",
		},
		{
			name:  "with special chars",
			input: "CATL (China)",
			want:  "catl-china",
		},
		{
			name:  "consecutive hyphens",
			input: "test--slug",
			want:  "test-slug",
		},
		{
			name:  "leading/trailing hyphens",
			input: "-test-slug-",
			want:  "test-slug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSlug(tt.input); got != tt.want {
				t.Errorf("normalizeSlug() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeOrgRefToFacilityFallback(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "org simple", in: "/org/albemarle", want: ""},
		{name: "org with geo", in: "/org/cn/tsmc", want: ""},
		{name: "org us", in: "/org/us/albemarle-corp", want: ""},
		{name: "gov with geo", in: "/gov/us/department-of-energy", want: ""},
		{name: "fac unchanged", in: "/fac/us/silver-peak", want: "/fac/us/silver-peak"},
		{name: "deposit unchanged", in: "/deposit/lithium/salar-de-atacama", want: "/deposit/lithium/salar-de-atacama"},
		{name: "product returns empty", in: "/product/lithium-carbonate", want: ""},
		{name: "empty returns empty", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeOrgRefToFacilityFallback(tt.in); got != tt.want {
				t.Errorf("NormalizeOrgRefToFacilityFallback(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExtractSlugFromPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple path",
			input: "/org/albemarle",
			want:  "albemarle",
		},
		{
			name:  "product path",
			input: "/product/lithium-carbonate",
			want:  "lithium-carbonate",
		},
		{
			name:  "nested path",
			input: "/product/raw/mineral/lithium",
			want:  "lithium",
		},
		{
			name:  "empty path",
			input: "",
			want:  "",
		},
		{
			name:  "just slash",
			input: "/",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractSlugFromPath(tt.input); got != tt.want {
				t.Errorf("extractSlugFromPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper function for tests
func strPtr(s string) *string {
	return &s
}

// Test that Validate() automatically generates canonicalPath for SupplyLink
func TestFragmentValidateAutoGeneratesCanonicalPath(t *testing.T) {
	f := &HorizonFragment{
		Type:        FragmentTypeSupplyLink,
		TenantID:    "tenant-123",
		ProductRef:  strPtr("/product/lithium-carbonate"),
		SupplierRef: strPtr("/fac/us/albemarle-kings-mountain-plant"),
		CustomerRef: strPtr("/fac/cn/catl-ningde-factory"),
		Governance:  Governance{Owner: "SYSTEM"},
		State:       make(map[string]MetricValue),
		Metadata: FragmentMetadata{
			CreatedAt:      time.Now(),
			LastUpdatedAt:  time.Now(),
			LastVerifiedAt: time.Now(),
		},
	}

	err := f.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if f.CanonicalPath == "" {
		t.Error("Validate() should have auto-generated canonicalPath")
	}

	if !strings.HasPrefix(f.CanonicalPath, "/supply-link/tenant-123/") {
		t.Errorf("canonicalPath = %v, want prefix /supply-link/tenant-123/", f.CanonicalPath)
	}
}
