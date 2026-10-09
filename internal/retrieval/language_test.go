package retrieval

import "testing"

// TestNormalizeFTSConfig pins the language -> PostgreSQL full-text search
// configuration mapping: unset keeps the baseline 'english', a supported
// language (or its primary subtag) selects its configuration, and anything
// unsupported falls back to 'simple'. The configuration is chosen from a fixed
// table, so it can never be interpolated from caller input.
func TestNormalizeFTSConfig(t *testing.T) {
	tests := []struct {
		name     string
		language string
		want     string
	}{
		{name: "unset is english baseline", language: "", want: DefaultFTSConfig},
		{name: "whitespace is english baseline", language: "   ", want: DefaultFTSConfig},
		{name: "english", language: "en", want: "english"},
		{name: "english uppercase", language: "EN", want: "english"},
		{name: "english region", language: "en-US", want: "english"},
		{name: "english region underscore", language: "en_US", want: "english"},
		{name: "german", language: "de", want: "german"},
		{name: "german region hyphen", language: "de-DE", want: "german"},
		{name: "german region underscore", language: "de_AT", want: "german"},
		{name: "french", language: "fr", want: "french"},
		{name: "spanish", language: "es", want: "spanish"},
		{name: "russian", language: "ru", want: "russian"},
		{name: "unsupported language uses simple", language: "zh", want: FallbackFTSConfig},
		{name: "unknown tag uses simple", language: "klingon", want: FallbackFTSConfig},
		{name: "empty primary subtag uses simple", language: "-DE", want: FallbackFTSConfig},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizeFTSConfig(test.language); got != test.want {
				t.Fatalf(
					"NormalizeFTSConfig(%q) = %q, want %q",
					test.language,
					got,
					test.want,
				)
			}
		})
	}
}

// TestFTSConfigConstants pins the two sentinel configurations so a change to
// either name is deliberate.
func TestFTSConfigConstants(t *testing.T) {
	if DefaultFTSConfig != "english" {
		t.Fatalf("DefaultFTSConfig = %q, want %q", DefaultFTSConfig, "english")
	}

	if FallbackFTSConfig != "simple" {
		t.Fatalf("FallbackFTSConfig = %q, want %q", FallbackFTSConfig, "simple")
	}
}

// TestFTSConfigByLanguageValuesAreKnown guards the mapping table: every value
// must be a non-empty lowercase configuration name (no SQL, no quotes), so the
// table can never introduce an interpolated value.
func TestFTSConfigByLanguageValuesAreKnown(t *testing.T) {
	for language, config := range ftsConfigByLanguage {
		if config == "" {
			t.Fatalf("language %q maps to an empty configuration", language)
		}

		for _, r := range config {
			if r < 'a' || r > 'z' {
				t.Fatalf("language %q maps to %q, want a lowercase config name", language, config)
			}
		}
	}
}
