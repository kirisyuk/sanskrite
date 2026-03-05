package taxonomy

import (
	"testing"
)

func TestValidateCanonicalPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "invalid org path without geo",
			path:    "/org/tesla",
			wantErr: true,
		},
		{
			name:    "valid org path with country",
			path:    "/org/us/tesla",
			wantErr: false,
		},
		{
			name:    "valid org path with unknown country",
			path:    "/org/na/tesla",
			wantErr: false,
		},
		{
			name:    "invalid org path with non-iso2 country",
			path:    "/org/usa/tesla",
			wantErr: true,
		},
		{
			name:    "valid org path with regional token (EU-level org)",
			path:    "/org/eu/european-commission",
			wantErr: false,
		},
		{
			name:    "valid org path with int (international org)",
			path:    "/org/int/united-nations",
			wantErr: false,
		},
		{
			name:    "valid gov path",
			path:    "/gov/ae/ministry-of-defence",
			wantErr: false,
		},
		{
			name:    "valid gov path unknown country",
			path:    "/gov/na/ministry-of-defence",
			wantErr: false,
		},
		{
			name:    "invalid gov path missing country segment",
			path:    "/gov/ministry-of-defence",
			wantErr: true,
		},
		{
			name:    "invalid gov path bad country segment",
			path:    "/gov/uae/ministry-of-defence",
			wantErr: true,
		},
		{
			name:    "valid gov path with regional token (EU-level body)",
			path:    "/gov/eu/european-commission",
			wantErr: false,
		},
		{
			name:    "valid person path",
			path:    "/person/john-doe",
			wantErr: false,
		},
		{
			name:    "valid facility path",
			path:    "/fac/tw/fab-21",
			wantErr: false,
		},
		{
			name:    "valid location path",
			path:    "/loc/us/tx/austin",
			wantErr: false,
		},
		{
			name:    "valid sphere path",
			path:    "/sphere/ai/models",
			wantErr: false,
		},
		{
			name:    "valid segment path",
			path:    "/segment/finance/retail-banking/kz",
			wantErr: false,
		},
		{
			name:    "valid segment path global",
			path:    "/segment/technology/semiconductors/global",
			wantErr: false,
		},
		{
			name:    "invalid segment path missing geo",
			path:    "/segment/technology/semiconductors",
			wantErr: true,
		},
		{
			name:    "invalid segment path bad geo",
			path:    "/segment/technology/semiconductors/uae",
			wantErr: true,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
		{
			name:    "root path",
			path:    "/",
			wantErr: true,
		},
		{
			name:    "missing leading slash",
			path:    "org/tesla",
			wantErr: true,
		},
		{
			name:    "invalid namespace",
			path:    "/invalid/tesla",
			wantErr: true,
		},
		{
			name:    "empty segment",
			path:    "/org//tesla",
			wantErr: true,
		},
		{
			name:    "invalid characters",
			path:    "/org/tesla_inc",
			wantErr: true,
		},
		{
			name:    "uppercase characters",
			path:    "/org/Tesla",
			wantErr: true,
		},
		{
			name:    "valid product path",
			path:    "/product/finished/mechanical/assembly/gemini",
			wantErr: false,
		},
		{
			name:    "valid product path fruit fresh",
			path:    "/product/raw/fruit/fresh/fruits",
			wantErr: false,
		},
		{
			name:    "valid program path",
			path:    "/program/kazakhstan/alem-ai",
			wantErr: false,
		},
		{
			name:    "valid market path",
			path:    "/market/global/energy/secondary-liquidity",
			wantErr: false,
		},
		{
			name:    "valid event path",
			path:    "/event/launch-2024",
			wantErr: false,
		},
		{
			name:    "valid media channel path",
			path:    "/media/telegram/v-kazani-poymut",
			wantErr: false,
		},
		{
			name:    "valid deposit path (preferred)",
			path:    "/deposit/crude-oil/permian-basin",
			wantErr: false,
		},
		{
			name:    "invalid deposit path without resource",
			path:    "/deposit/ghawar",
			wantErr: true,
		},
		{
			name:    "namespace-only path is invalid",
			path:    "/org",
			wantErr: true,
		},
		{
			name:    "valid zone path with descriptive slug",
			path:    "/zone/mm/military-conflict/northern-rakhine",
			wantErr: false,
		},
		{
			name:    "valid zone path with composite geo",
			path:    "/zone/tr-sy/military-conflict/northern-syria",
			wantErr: false,
		},
		{
			name:    "invalid zone path with na slug",
			path:    "/zone/mm/military-conflict/na",
			wantErr: true,
		},
		{
			name:    "valid zone path with underscore in condition_type (LLM output)",
			path:    "/zone/ye/military_conflict/hodeidah-region",
			wantErr: false,
		},
		{
			name:    "valid zone path political_tension",
			path:    "/zone/mena/political_tension/strait-of-hormuz",
			wantErr: false,
		},
		{
			name:    "valid track path mmsi",
			path:    "/track/sea/id/mmsi-525123456",
			wantErr: false,
		},
		{
			name:    "valid track path icao24",
			path:    "/track/air/us/icao24-a1b2c3",
			wantErr: false,
		},
		{
			name:    "valid track path global geo",
			path:    "/track/sea/global/imo-9123456",
			wantErr: false,
		},
		{
			name:    "valid track path flight callsign",
			path:    "/track/air/ru/flight-su123",
			wantErr: false,
		},
		{
			name:    "invalid track path bad kind",
			path:    "/track/space/us/icao24-abc123",
			wantErr: true,
		},
		{
			name:    "invalid track path too few segments",
			path:    "/track/air/us",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCanonicalPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCanonicalPath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestSlugify_BasicASCII(t *testing.T) {
	if got := Slugify("Tesla, Inc."); got != "tesla-inc" {
		t.Fatalf("Slugify(Tesla, Inc.) = %q, want %q", got, "tesla-inc")
	}
}

func TestSlugify_TransliterationCyrillic(t *testing.T) {
	got := Slugify("В Казани поймут")
	want := "v-kazani-poymut"
	if got != want {
		t.Fatalf("Slugify(cyrillic) = %q, want %q", got, want)
	}
}

func TestJoinPath_UsesSlugify(t *testing.T) {
	if got := JoinPath("media", "telegram", "В Казани поймут"); got != "/media/telegram/v-kazani-poymut" {
		t.Fatalf("JoinPath(media, telegram, cyrillic) = %q, want %q", got, "/media/telegram/v-kazani-poymut")
	}
}

func TestOrgCanonicalPath_ParentheticalAnnotations(t *testing.T) {
	// Legal form is NOT included in org paths; slug is base name only.
	cases := []struct {
		cc, name, want string
	}{
		{"sa", "STC Bank PJSC", "/org/sa/stc-bank"},
		{"na", "stc Bank PLC", "/org/na/stc-bank"},
		{"sa", "Stc Bank Jsc", "/org/sa/stc-bank"},
		{"nl", "Nexperia B.V.", "/org/nl/nexperia"},
		{"nl", "Nexperia B V", "/org/nl/nexperia"},
		// Parenthetical "(formerly ...)" must be stripped — slug core should be "stc-bank".
		{"sa", "STC Bank (formerly stc pay) PJSC", "/org/sa/stc-bank"},
		{"sa", "STC Bank (formerly stc pay)", "/org/sa/stc-bank"},
	}
	for _, c := range cases {
		got := OrgCanonicalPath(c.cc, c.name)
		if got != c.want {
			t.Errorf("OrgCanonicalPath(%q, %q) = %q, want %q", c.cc, c.name, got, c.want)
		}
	}
}

func TestOrgCanonicalPath_GeoVariants(t *testing.T) {
	cases := []struct {
		geo  string
		want string
	}{
		{geo: "us", want: "/org/us/acme"},
		{geo: "int", want: "/org/int/acme"},
		{geo: "eu", want: "/org/eu/acme"},
		{geo: "ae-sa-om", want: "/org/ae-sa-om/acme"},
		{geo: "bad-geo-token", want: "/org/na/acme"},
	}
	for _, c := range cases {
		got := OrgCanonicalPath(c.geo, "Acme LLC")
		if got != c.want {
			t.Errorf("OrgCanonicalPath(%q) = %q, want %q", c.geo, got, c.want)
		}
	}
}

func TestGovCanonicalPath_GeoVariants(t *testing.T) {
	cases := []struct {
		geo  string
		want string
	}{
		{geo: "us", want: "/gov/us/ministry-of-justice"},
		{geo: "eu", want: "/gov/eu/ministry-of-justice"},
		{geo: "ae-sa-om", want: "/gov/ae-sa-om/ministry-of-justice"},
		{geo: "int", want: "/gov/int/ministry-of-justice"},
	}
	for _, c := range cases {
		got := GovCanonicalPath(c.geo, "Ministry of Justice")
		if got != c.want {
			t.Errorf("GovCanonicalPath(%q) = %q, want %q", c.geo, got, c.want)
		}
	}
}

func TestGeoCanonicalPath_GeoVariants(t *testing.T) {
	if got := GeoCanonicalPath("ae-sa-om", "na", "na"); got != "/loc/ae-sa-om" {
		t.Fatalf("GeoCanonicalPath composite = %q, want %q", got, "/loc/ae-sa-om")
	}
	if got := GeoCanonicalPath("eu", "na", "na"); got != "/loc/eu" {
		t.Fatalf("GeoCanonicalPath regional = %q, want %q", got, "/loc/eu")
	}
}

func TestProgramCanonicalPath_JurisdictionNormalization(t *testing.T) {
	cases := []struct {
		jurisdiction string
		wantPrefix   string
	}{
		{jurisdiction: "us", wantPrefix: "/program/us/"},
		{jurisdiction: "int", wantPrefix: "/program/int/"},
		{jurisdiction: "global", wantPrefix: "/program/global/"},
		{jurisdiction: "eu", wantPrefix: "/program/eu/"},
		{jurisdiction: "kazakhstan", wantPrefix: "/program/global/"},
	}
	for _, c := range cases {
		got := ProgramCanonicalPath(c.jurisdiction, "Alem AI")
		if len(got) == 0 || got[:len(c.wantPrefix)] != c.wantPrefix {
			t.Errorf("ProgramCanonicalPath(%q) = %q, want prefix %q", c.jurisdiction, got, c.wantPrefix)
		}
	}
}

func TestIsISO2CountryCode(t *testing.T) {
	for _, s := range []string{"us", "sa", "kz", "ae", "gb"} {
		if !IsISO2CountryCode(s) {
			t.Errorf("IsISO2CountryCode(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "usa", "uae", "1", "a"} {
		if IsISO2CountryCode(s) {
			t.Errorf("IsISO2CountryCode(%q) = true, want false", s)
		}
	}
	if !IsISO2CountryCode("  sa  ") {
		t.Errorf("IsISO2CountryCode(\"  sa  \") = false, want true (trimmed)")
	}
}

func TestIsValidGeoForOrgGovFac(t *testing.T) {
	for _, s := range []string{"na", "int", "us", "sa", "eu", "gcc", "asean", "tr-sy", "ae-sa-om"} {
		if !IsValidGeoForOrgGovFac(s) {
			t.Errorf("IsValidGeoForOrgGovFac(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "usa", "uae", "xyz123"} {
		if IsValidGeoForOrgGovFac(s) {
			t.Errorf("IsValidGeoForOrgGovFac(%q) = true, want false", s)
		}
	}
}
