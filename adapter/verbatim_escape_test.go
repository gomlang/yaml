package adapter

import (
	"strings"
	"testing"
)

func TestEscapedSlashesAfterVerbatimCoreTags(t *testing.T) {
	for _, input := range []string{
		`!<tag:yaml.org,2002:str> "a\/b"`,
		`&anchor !<tag:yaml.org,2002:str> "a\/b"`,
		`!<tag:yaml.org,2002:str> &anchor "a\/b"`,
		"!<tag:yaml.org,2002:str> # comment\n  \"a\\/b\"",
		"\ufeff!<tag:yaml.org,2002:str> \"a\\/b\"",
		"%YAML 1.2\n---\n!<tag:yaml.org,2002:str> \"a\\/b\"",
		`!<tag:yaml.org%2C2002:str> "a\/b"`,
		`!!str "a\/b"`,
	} {
		t.Run(input, func(t *testing.T) {
			result := parsed(t, input, standard(), "reject")
			if result.Kind != "" || result.Documents != 1 || len(result.Tokens) != 1 || result.Tokens[0].Kind != "string" || result.Tokens[0].Text != "a/b" {
				t.Fatalf("%+v", result)
			}
		})
	}
}

func TestVerbatimSlashNormalizationRetainsAliasesStylesAndColumns(t *testing.T) {
	input := `{unicode: "中", tagged: &a !<tag:yaml.org,2002:str> "a\/b", alias: *a, plain: a\/b, single: 'a\/b', escaped: !<tag:yaml.org,2002:str> "a\\/b", "a\/b": first, !<tag:yaml.org,2002:str> "a\/b": second}`
	result := parsed(t, input, standard(), "last")
	if result.Kind != "" {
		t.Fatal(result)
	}
	index := 0
	object := referenceValue(result.Tokens, &index).(map[string]any)
	for key, want := range map[string]any{"unicode": "中", "tagged": "a/b", "alias": "a/b", "plain": `a\/b`, "single": `a\/b`, "escaped": `a\/b`, "a/b": "second"} {
		if object[key] != want {
			t.Fatalf("%s: actual=%#v expected=%#v", key, object[key], want)
		}
	}
	result = parsed(t, input, standard(), "reject")
	wantColumn := len([]rune(input[:strings.LastIndex(input, "!<tag:")])) + 1
	if result.Kind != "duplicate" || result.Line != 1 || result.Column != wantColumn {
		t.Fatalf("expected column %d: %+v", wantColumn, result)
	}
}

func TestVerbatimSlashNormalizationPreservesValidationAndLimits(t *testing.T) {
	for _, test := range []struct{ input, kind string }{
		{`!<tag:example.test,2026:custom> "a\/b"`, "unsupported"},
		{`!<tag:yaml.org,2002:int> "a\/b"`, "syntax"},
		{`!<tag:yaml.org,2002:str> "a\/b\q"`, "syntax"},
		{`!<tag:yaml.org,2002:str "a\/b"`, "syntax"},
	} {
		if result := parsed(t, test.input, standard(), "reject"); result.Kind != test.kind {
			t.Errorf("%q: %+v", test.input, result)
		}
	}
	input := `!<tag:yaml.org,2002:str> "a\/b"`
	limits := standard()
	limits.Input, limits.Decoded = len(input), 3
	if result := parsed(t, input, limits, "reject"); result.Kind != "" {
		t.Fatal(result)
	}
	limits.Decoded = 2
	if result := parsed(t, input, limits, "reject"); result.Kind != "limit" {
		t.Fatal(result)
	}
	limits = standard()
	limits.Input = len(input) - 1
	if result := parsed(t, input, limits, "reject"); result.Kind != "limit" {
		t.Fatal(result)
	}
}
