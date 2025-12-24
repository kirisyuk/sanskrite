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
		checkEval   func(*testing.T, *QueryAddress)
	}{
		{
			name:    "simple sector query",
			address: "/query/sector:/Technology/Semiconductors/**",
			wantErr: false,
			checkEval: func(t *testing.T, qa *QueryAddress) {
				f1 := &HorizonFragmentView{CanonicalPath: "/org/a", SectorPath: "/Technology/Semiconductors/Design"}
				f2 := &HorizonFragmentView{CanonicalPath: "/org/b", SectorPath: "/Energy/Fossil_Fuels"}
				if !qa.Expr.Eval(f1, &EvalContext{}) {
					t.Errorf("expected f1 to match")
				}
				if qa.Expr.Eval(f2, &EvalContext{}) {
					t.Errorf("expected f2 not to match")
				}
			},
		},
		{
			name:    "OR with parentheses + AND",
			address: `/query/(canonicalPath:"/org/tsmc" OR canonicalPath:"/org/samsung") AND sector:/Technology/**`,
			wantErr: false,
			checkEval: func(t *testing.T, qa *QueryAddress) {
				ctx := &EvalContext{}
				ok1 := qa.Expr.Eval(&HorizonFragmentView{CanonicalPath: "/org/tsmc", SectorPath: "/Technology/Semiconductors"}, ctx)
				ok2 := qa.Expr.Eval(&HorizonFragmentView{CanonicalPath: "/org/samsung", SectorPath: "/Technology/Hardware"}, ctx)
				ok3 := qa.Expr.Eval(&HorizonFragmentView{CanonicalPath: "/org/tsmc", SectorPath: "/Energy/Fossil_Fuels"}, ctx)
				ok4 := qa.Expr.Eval(&HorizonFragmentView{CanonicalPath: "/org/other", SectorPath: "/Technology/Semiconductors"}, ctx)
				if !ok1 || !ok2 {
					t.Errorf("expected tsmc/samsung + technology to match")
				}
				if ok3 {
					t.Errorf("expected tsmc + energy NOT to match due to sector constraint")
				}
				if ok4 {
					t.Errorf("expected other org NOT to match due to canonicalPath disjunction")
				}
			},
		},
		{
			name:    "NOT precedence",
			address: "/query/NOT sector:/Energy/** AND sector:/Technology/**",
			wantErr: false,
			checkEval: func(t *testing.T, qa *QueryAddress) {
				ctx := &EvalContext{}
				if !qa.Expr.Eval(&HorizonFragmentView{SectorPath: "/Technology/Semiconductors"}, ctx) {
					t.Errorf("expected technology to match")
				}
				if qa.Expr.Eval(&HorizonFragmentView{SectorPath: "/Energy/Fossil_Fuels"}, ctx) {
					t.Errorf("expected energy to NOT match")
				}
			},
		},
		{
			name:    "mixed OR/AND precedence (AND binds tighter than OR)",
			address: "/query/canonicalPath:/org/a OR canonicalPath:/org/b AND sector:/Technology/**",
			wantErr: false,
			checkEval: func(t *testing.T, qa *QueryAddress) {
				// Expected: (a) OR (b AND sector=Technology)
				ctx := &EvalContext{}
				if !qa.Expr.Eval(&HorizonFragmentView{CanonicalPath: "/org/a", SectorPath: "/Energy/Fossil_Fuels"}, ctx) {
					t.Errorf("expected /org/a to match regardless of sector")
				}
				if !qa.Expr.Eval(&HorizonFragmentView{CanonicalPath: "/org/b", SectorPath: "/Technology/Software"}, ctx) {
					t.Errorf("expected /org/b + tech to match")
				}
				if qa.Expr.Eval(&HorizonFragmentView{CanonicalPath: "/org/b", SectorPath: "/Energy/Fossil_Fuels"}, ctx) {
					t.Errorf("expected /org/b + energy NOT to match")
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
		{
			name:    "unbalanced parentheses",
			address: "/query/(sector:/Technology/**",
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
			if !tt.wantErr && qa != nil && tt.checkEval != nil {
				tt.checkEval(t, qa)
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
				Raw:  "/query/sector:/Technology/**",
				Expr: termExpr{t: Term{Field: FieldSector, Pattern: "/Technology/**"}},
			},
			wantErr: false,
		},
		{
			name: "valid with multiple criteria",
			qa: &QueryAddress{
				Raw: "/query/sector:/Technology/** AND location:/loc/id/**",
				Expr: andExpr{
					a: termExpr{t: Term{Field: FieldSector, Pattern: "/Technology/**"}},
					b: termExpr{t: Term{Field: FieldLocation, Pattern: "/loc/id/**"}},
				},
			},
			wantErr: false,
		},
		{
			name: "empty query address",
			qa: &QueryAddress{
				Raw: "",
			},
			wantErr: true,
		},
		{
			name: "no criteria",
			qa: &QueryAddress{
				Raw: "/query/",
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



