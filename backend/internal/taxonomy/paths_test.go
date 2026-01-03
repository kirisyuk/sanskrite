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
			name:    "valid org path",
			path:    "/org/tesla",
			wantErr: false,
		},
		{
			name:    "valid person path",
			path:    "/person/john-doe",
			wantErr: false,
		},
		{
			name:    "valid facility path",
			path:    "/fac/tsmc/fab-21",
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
			path:    "/product/google/gemini",
			wantErr: false,
		},
		{
			name:    "valid program path",
			path:    "/program/kazakhstan/alem-ai",
			wantErr: false,
		},
		{
			name:    "valid market path",
			path:    "/market/energy/secondary-liquidity",
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
			name:    "namespace-only path is invalid",
			path:    "/org",
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

func TestSlugify_TransliterationCyrillic(t *testing.T) {
	got := Slugify("В казани поймут")
	// Expected best-effort transliteration + slugging.
	// Note: exact transliteration scheme is internal; update this test if mapping changes.
	want := "v-kazani-poymut"
	if got != want {
		t.Fatalf("Slugify(cyrillic) = %q, want %q", got, want)
	}
}







