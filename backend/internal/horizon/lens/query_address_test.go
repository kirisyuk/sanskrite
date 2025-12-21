package lens

import (
	"testing"
)

func TestIsQueryAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    bool
	}{
		{
			name:    "valid query address",
			address: "/query/sector:/Technology/Semiconductors/**",
			want:    true,
		},
		{
			name:    "static canonical path",
			address: "/org/tsmc",
			want:    false,
		},
		{
			name:    "empty string",
			address: "",
			want:    false,
		},
		{
			name:    "query without prefix",
			address: "sector:/Technology/**",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsQueryAddress(tt.address)
			if got != tt.want {
				t.Errorf("IsQueryAddress(%q) = %v, want %v", tt.address, got, tt.want)
			}
		})
	}
}

func TestParseQueryAddress(t *testing.T) {
	tests := []struct {
		name        string
		address     string
		wantErr     bool
		checkFields func(*testing.T, *QueryAddress)
	}{
		{
			name:    "simple sector query",
			address: "/query/sector:/Technology/Semiconductors/**",
			wantErr: false,
			checkFields: func(t *testing.T, qa *QueryAddress) {
				if len(qa.SectorPatterns) != 1 {
					t.Errorf("expected 1 sector pattern, got %d", len(qa.SectorPatterns))
				}
				if qa.SectorPatterns[0] != "/Technology/Semiconductors/**" {
					t.Errorf("expected sector pattern '/Technology/Semiconductors/**', got %q", qa.SectorPatterns[0])
				}
				if qa.Operator != "AND" {
					t.Errorf("expected operator AND, got %q", qa.Operator)
				}
			},
		},
		{
			name:    "OR operator",
			address: "/query/location:/loc/id/** OR location:/loc/ir/**",
			wantErr: false,
			checkFields: func(t *testing.T, qa *QueryAddress) {
				if qa.Operator != "OR" {
					t.Errorf("expected operator OR, got %q", qa.Operator)
				}
				if len(qa.LocationPatterns) != 2 {
					t.Errorf("expected 2 location patterns, got %d", len(qa.LocationPatterns))
				}
			},
		},
		{
			name:    "AND operator explicit",
			address: "/query/sector:/Technology/** AND entityType:/Infrastructure/Logistics/Seaport",
			wantErr: false,
			checkFields: func(t *testing.T, qa *QueryAddress) {
				if qa.Operator != "AND" {
					t.Errorf("expected operator AND, got %q", qa.Operator)
				}
				if len(qa.SectorPatterns) != 1 {
					t.Errorf("expected 1 sector pattern, got %d", len(qa.SectorPatterns))
				}
				if len(qa.EntityTypePatterns) != 1 {
					t.Errorf("expected 1 entity type pattern, got %d", len(qa.EntityTypePatterns))
				}
			},
		},
		{
			name:    "multiple criteria",
			address: "/query/sphere:/sphere/logistics/maritime AND sector:/Technology/** AND location:/loc/id/**",
			wantErr: false,
			checkFields: func(t *testing.T, qa *QueryAddress) {
				if len(qa.SpherePaths) != 1 {
					t.Errorf("expected 1 sphere path, got %d", len(qa.SpherePaths))
				}
				if len(qa.SectorPatterns) != 1 {
					t.Errorf("expected 1 sector pattern, got %d", len(qa.SectorPatterns))
				}
				if len(qa.LocationPatterns) != 1 {
					t.Errorf("expected 1 location pattern, got %d", len(qa.LocationPatterns))
				}
			},
		},
		{
			name:    "invalid query address format",
			address: "/org/tsmc",
			wantErr: true,
		},
		{
			name:    "empty criteria",
			address: "/query/",
			wantErr: true,
		},
		{
			name:    "unknown field",
			address: "/query/unknownField:value",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qa, err := ParseQueryAddress(tt.address)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseQueryAddress() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && qa != nil && tt.checkFields != nil {
				tt.checkFields(t, qa)
			}
		})
	}
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		pattern string
		want    bool
	}{
		{
			name:    "exact match",
			value:   "/Technology/Semiconductors",
			pattern: "/Technology/Semiconductors",
			want:    true,
		},
		{
			name:    "wildcard match",
			value:   "/Technology/Semiconductors/Design",
			pattern: "/Technology/Semiconductors/**",
			want:    true,
		},
		{
			name:    "wildcard no match",
			value:   "/Technology/Hardware",
			pattern: "/Technology/Semiconductors/**",
			want:    false,
		},
		{
			name:    "wildcard prefix match",
			value:   "/Technology/Semiconductors/Manufacturing/TSMC",
			pattern: "/Technology/Semiconductors/**",
			want:    true,
		},
		{
			name:    "no wildcard no match",
			value:   "/Technology/Semiconductors/Design",
			pattern: "/Technology/Semiconductors",
			want:    false,
		},
		{
			name:    "empty pattern",
			value:   "/Technology/Semiconductors",
			pattern: "",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesPattern(tt.value, tt.pattern)
			if got != tt.want {
				t.Errorf("MatchesPattern(%q, %q) = %v, want %v", tt.value, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestQueryAddressValidate(t *testing.T) {
	tests := []struct {
		name    string
		qa      *QueryAddress
		wantErr bool
	}{
		{
			name: "valid with sector",
			qa: &QueryAddress{
				Raw:            "/query/sector:/Technology/**",
				SectorPatterns: []string{"/Technology/**"},
				Operator:       "AND",
			},
			wantErr: false,
		},
		{
			name: "valid with multiple criteria",
			qa: &QueryAddress{
				Raw:            "/query/sector:/Technology/** AND location:/loc/id/**",
				SectorPatterns: []string{"/Technology/**"},
				LocationPatterns: []string{"/loc/id/**"},
				Operator:       "AND",
			},
			wantErr: false,
		},
		{
			name: "empty query address",
			qa: &QueryAddress{
				Raw:      "",
				Operator: "AND",
			},
			wantErr: true,
		},
		{
			name: "no criteria",
			qa: &QueryAddress{
				Raw:      "/query/",
				Operator: "AND",
			},
			wantErr: true,
		},
		{
			name: "invalid operator",
			qa: &QueryAddress{
				Raw:            "/query/sector:/Technology/**",
				SectorPatterns: []string{"/Technology/**"},
				Operator:       "XOR",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.qa.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("QueryAddress.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

