package scout

import "testing"

func TestParseMarketDedupScope(t *testing.T) {
	cases := []struct {
		name          string
		canonicalPath string
		wantGeo       string
		wantDomain    string
		wantVertical  string
	}{
		{
			name:          "segment-first market path",
			canonicalPath: "/market/us/technology/semiconductors",
			wantGeo:       "us",
			wantDomain:    "technology",
			wantVertical:  "semiconductors",
		},
		{
			name:          "domain alias normalized",
			canonicalPath: "/market/sa/financials/banking",
			wantGeo:       "sa",
			wantDomain:    "finance",
			wantVertical:  "banking",
		},
		{
			name:          "vertical alias normalized via segment rules",
			canonicalPath: "/market/global/finance/cryptocurrency",
			wantGeo:       "global",
			wantDomain:    "finance",
			wantVertical:  "crypto",
		},
		{
			name:          "legacy market path rejected",
			canonicalPath: "/market/sa/banking",
			wantGeo:       "",
			wantDomain:    "",
			wantVertical:  "",
		},
		{
			name:          "non-market path rejected",
			canonicalPath: "/org/sa/riyad-bank",
			wantGeo:       "",
			wantDomain:    "",
			wantVertical:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotGeo, gotDomain, gotVertical := parseMarketDedupScope(tc.canonicalPath)
			if gotGeo != tc.wantGeo || gotDomain != tc.wantDomain || gotVertical != tc.wantVertical {
				t.Fatalf("parseMarketDedupScope(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tc.canonicalPath, gotGeo, gotDomain, gotVertical, tc.wantGeo, tc.wantDomain, tc.wantVertical)
			}
		})
	}
}
