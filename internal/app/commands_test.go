package app

import "testing"

func TestNormalizeUniverseKey(t *testing.T) {
	cases := map[string]string{
		"":           "tum",
		"tum":        "tum",
		"bist-tum":   "tum",
		"BIST_100":   "bist100",
		"bist 100":   "bist100",
		"100":        "bist100",
		"XU100":      "bist100",
		"bilinmeyen": "bilinmeyen",
	}

	for input, expected := range cases {
		if got := normalizeUniverseKey(input); got != expected {
			t.Fatalf("normalizeUniverseKey(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestSymbolFromCommand(t *testing.T) {
	app := &App{
		knownSymbols: map[string]struct{}{
			"ALARK": {},
			"A1CAP": {},
			"EREGL": {},
		},
	}

	cases := []struct {
		command string
		want    string
		ok      bool
	}{
		{command: "/alark", want: "ALARK", ok: true},
		{command: "alark", want: "ALARK", ok: true},
		{command: "/A1CAP", want: "A1CAP", ok: true},
		{command: "EREGL.IS", want: "EREGL", ok: true},
		{command: "/help", ok: false},
		{command: "/NOTBIST", ok: false},
		{command: "NOTBIST", ok: false},
	}

	for _, tc := range cases {
		got, ok := app.symbolFromCommand(tc.command)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("symbolFromCommand(%q) = %q, %v; want %q, %v", tc.command, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNormalizeCommandName(t *testing.T) {
	cases := map[string]string{
		"/gunici100":         "gunici100",
		"gunicitum":          "gunicitum",
		"/durum@SomeBot":     "durum",
		" EREGL ":            "eregl",
		"/ALARK@bist_bot":    "alark",
		"gunluk100@bist_bot": "gunluk100",
	}

	for input, expected := range cases {
		if got := normalizeCommandName(input); got != expected {
			t.Fatalf("normalizeCommandName(%q) = %q, want %q", input, got, expected)
		}
	}
}
