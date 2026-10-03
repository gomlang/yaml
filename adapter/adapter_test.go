package adapter

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func standard() Limits {
	return Limits{Input: 1048576, Depth: 128, Nodes: 100000, Aliases: 1000, Decoded: 4194304, Documents: 64, Output: 4194304}
}

func limitsJSON(value Limits) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func parsed(t *testing.T, input string, limits Limits, policy string) Response {
	t.Helper()
	var result Response
	if err := json.Unmarshal([]byte(Parse(input, limitsJSON(limits), policy)), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCoreSchemaAndTags(t *testing.T) {
	tests := []struct{ input, kind, text string }{
		{"yes", "string", "yes"}, {"ON", "string", "ON"}, {"2026-10-03", "string", "2026-10-03"},
		{"012", "integer", "12"}, {"0o17", "integer", "15"}, {"0xFF", "integer", "255"},
		{"0b11", "string", "0b11"}, {"1_000", "string", "1_000"}, {"+.Inf", "float", ".inf"},
		{"null", "null", ""}, {"1.25e+2", "float", "125.0"}, {".5", "float", "0.5"},
		{"!!str 1e999", "string", "1e999"}, {"!!str \"1e999\"", "string", "1e999"},
		{"! true", "string", "true"}, {"\ufeff! true", "string", "true"}, {"! 123", "string", "123"}, {"! 1e999", "string", "1e999"},
		{"&anchor ! 123", "string", "123"}, {"! &anchor 123", "string", "123"},
		{"&anchor\n! 123", "string", "123"}, {"!!float 1", "float", "1.0"},
		{"'! true'", "string", "! true"}, {"|\n  ! true\n", "string", "! true\n"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			result := parsed(t, test.input, standard(), "reject")
			if result.Kind != "" || len(result.Tokens) != 1 || result.Tokens[0].Kind != test.kind || result.Tokens[0].Text != test.text {
				t.Fatalf("%+v", result)
			}
		})
	}
}

func TestCRSeparatorsRetainTagPositions(t *testing.T) {
	for _, input := range []string{"a: 1\rb: ! true\r", "%YAML 1.2\r---\ra: ! true\r", "a: 1\r\nb: ! true\r\n"} {
		result := parsed(t, input, standard(), "reject")
		if result.Kind != "" || result.Tokens[len(result.Tokens)-1].Kind != "string" {
			t.Fatal(result)
		}
	}
}

func TestEmptyAnchoredValueDoesNotStealSiblingTag(t *testing.T) {
	result := parsed(t, "first: &anchor\n! true: second\n", standard(), "reject")
	if result.Kind != "" || result.Tokens[2].Kind != "null" || result.Tokens[3].Kind != "string" {
		t.Fatal(result)
	}
}

func TestAliasAndDuplicateFailures(t *testing.T) {
	tests := []struct{ input, kind string }{
		{"a: &a [*a]", "alias"}, {"---\na: &x 123\n---\nb: *x\n", "alias"},
		{"a: 1\na: 2", "duplicate"}, {"+1: a\n01: b", "duplicate"},
		{"-0.0: first\n0.0: second", "duplicate"}, {".nan: first\n.NaN: second", "duplicate"},
		{"? [a, b]\n: value", "unsupported"}, {"!unknown value", "unsupported"},
		{"!!bool yes", "syntax"}, {"!!int 0b10", "syntax"}, {"<<: {a: 1}", "unsupported"},
		{"[unclosed", "syntax"}, {"a: *missing", "syntax"}, {"1e999", "unsupported"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			result := parsed(t, test.input, standard(), "reject")
			if result.Kind != test.kind || len(result.Tokens) != 0 {
				t.Fatalf("%+v", result)
			}
		})
	}
	for _, policy := range []string{"first", "last"} {
		result := parsed(t, "a: 1\na: 2", standard(), policy)
		want := "1"
		if policy == "last" {
			want = "2"
		}
		if result.Kind != "" || len(result.Tokens) != 3 || result.Tokens[2].Text != want {
			t.Fatalf("%s: %+v", policy, result)
		}
	}
}

func TestEveryBudgetAndSourcePosition(t *testing.T) {
	tests := []struct {
		name, input string
		change      func(*Limits)
	}{
		{"input", "abcd", func(l *Limits) { l.Input = 3 }},
		{"depth", "[[[x]]]", func(l *Limits) { l.Depth = 3 }},
		{"nodes", "[a,b,c]", func(l *Limits) { l.Nodes = 3 }},
		{"aliases", "[&a x,*a,*a]", func(l *Limits) { l.Aliases = 1 }},
		{"expanded bytes", "[&a abc,*a]", func(l *Limits) { l.Decoded = 5 }},
		{"documents", "---\n1\n---\n2", func(l *Limits) { l.Documents = 1 }},
		{"digits", strings.Repeat("9", 4097), func(l *Limits) {}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := standard()
			test.change(&limits)
			result := parsed(t, test.input, limits, "reject")
			if result.Kind != "limit" {
				t.Fatalf("%+v", result)
			}
		})
	}
	result := parsed(t, "unicode: 中\nunicode: value", standard(), "reject")
	if result.Line != 2 || result.Column != 1 {
		t.Fatalf("position: %+v", result)
	}
	if result := parsed(t, "a", Limits{}, "reject"); result.Kind != "argument" {
		t.Fatal(result)
	}
}

func TestExponentialAliasesNeverExpandUnbounded(t *testing.T) {
	input := "a: &a [x,x,x,x,x,x,x,x,x,x]\nb: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a,*a]\nc: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b,*b]\nd: [*c,*c,*c,*c,*c,*c,*c,*c,*c,*c]"
	limits := standard()
	limits.Nodes = 1000
	if result := parsed(t, input, limits, "reject"); result.Kind != "limit" {
		t.Fatal(result)
	}
}

func TestDirectivesAreNormalizedOnlyAfterParserDiagnosis(t *testing.T) {
	for _, input := range []string{
		"%YAML 1.2\n---\nyes",
		"# first\n%YAML 1.2 # core\n---\nyes",
		"\ufeff%YAML 1.2\r\n---\r\nyes",
		"%YAML 1.2\n---\na\n...\n%YAML 1.2\n---\nb",
	} {
		result := parsed(t, input, standard(), "reject")
		if result.Kind != "" || result.Tokens[0].Kind != "string" {
			t.Fatalf("%q: %+v", input, result)
		}
	}
	input := "\"a\n%YAML 1.2\nb\"\n...\n%YAML 1.2\n---\n|\n  %YAML 1.2\n"
	result := parsed(t, input, standard(), "reject")
	if result.Kind != "" || result.Documents != 2 || result.Tokens[0].Text != "a %YAML 1.2 b" || result.Tokens[1].Text != "%YAML 1.2\n" {
		t.Fatal(result)
	}
	for _, input := range []string{"%YAML 1.3\n---\na", "%YAML 1.2\n%YAML 1.2\n---\na", "%YAML 1.2junk\n---\na"} {
		if result := parsed(t, input, standard(), "reject"); result.Kind != "syntax" {
			t.Fatal(result)
		}
	}
}

func TestEmissionValidatesValuesAndOutputBudget(t *testing.T) {
	tokens := []Token{{Kind: "mapping", Size: 2}, {Kind: "string", Text: "literal"}, {Kind: "string", Text: "yes"}, {Kind: "string", Text: "lines"}, {Kind: "string", Text: "a\nb\n"}}
	data, _ := json.Marshal(tokens)
	var result Response
	if err := json.Unmarshal([]byte(Emit(string(data), limitsJSON(standard()), 1, 2)), &result); err != nil {
		t.Fatal(err)
	}
	if result.Kind != "" || !strings.Contains(result.Output, "\"yes\"") {
		t.Fatal(result)
	}
	reparsed := parsed(t, result.Output, standard(), "reject")
	if reparsed.Kind != "" || reparsed.Tokens[4].Text != "a\nb\n" {
		t.Fatal(reparsed)
	}
	limits := standard()
	limits.Output = 2
	json.Unmarshal([]byte(Emit(string(data), limitsJSON(limits), 1, 2)), &result)
	if result.Kind != "limit" {
		t.Fatal(result)
	}
	invalid, _ := json.Marshal([]Token{{Kind: "integer", Text: "not-number"}})
	json.Unmarshal([]byte(Emit(string(invalid), limitsJSON(standard()), 1, 2)), &result)
	if result.Kind != "syntax" {
		t.Fatal(result)
	}
}

func referenceValue(tokens []Token, index *int) any {
	token := tokens[*index]
	*index++
	switch token.Kind {
	case "null":
		return nil
	case "bool":
		return token.Text == "true"
	case "string":
		return token.Text
	case "integer", "float":
		var value any
		json.Unmarshal([]byte(token.Text), &value)
		return value
	case "sequence":
		values := []any{}
		for n := 0; n < token.Size; n++ {
			values = append(values, referenceValue(tokens, index))
		}
		return values
	case "mapping":
		values := map[string]any{}
		for n := 0; n < token.Size; n++ {
			key := referenceValue(tokens, index).(string)
			values[key] = referenceValue(tokens, index)
		}
		return values
	}
	panic("unknown token")
}

func TestPublishedYAMLReferenceCorpus(t *testing.T) {
	inputs, err := filepath.Glob("../tests/data/reference/*.yaml")
	if err != nil || len(inputs) != 36 {
		t.Fatalf("corpus: %d %v", len(inputs), err)
	}
	for _, path := range inputs {
		t.Run(filepath.Base(path), func(t *testing.T) {
			input, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(strings.TrimSuffix(path, ".yaml") + ".json")
			if err != nil {
				t.Fatal(err)
			}
			result := parsed(t, string(input), standard(), "reject")
			if result.Kind != "" {
				t.Fatalf("parse: %+v", result)
			}
			decoder := json.NewDecoder(strings.NewReader(string(expected)))
			index := 0
			for document := 0; document < result.Documents; document++ {
				var want any
				if err := decoder.Decode(&want); err != nil {
					t.Fatal(err)
				}
				actual := referenceValue(result.Tokens, &index)
				if !reflect.DeepEqual(actual, want) {
					t.Fatalf("actual=%#v expected=%#v", actual, want)
				}
			}
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF {
				t.Fatalf("extra document: %v", err)
			}
		})
	}
}

func BenchmarkWideFlowSequence(b *testing.B) {
	input := "[" + strings.Repeat("x,", 10000) + "x]"
	limits := limitsJSON(standard())
	for b.Loop() {
		Parse(input, limits, "reject")
	}
}

func TestEscapedSlashNormalizationPreservesOtherScalarStyles(t *testing.T) {
	input := "{quoted: \"a\\/b\", plain: a\\/b, single: 'a\\/b', escaped: \"a\\\\/b\", \"a\\/b\": 1, duplicate: 0, duplicate: 1}"
	result := parsed(t, input, standard(), "first")
	if result.Kind != "" {
		t.Fatal(result)
	}
	index := 0
	object := referenceValue(result.Tokens, &index).(map[string]any)
	for key, want := range map[string]any{"quoted": "a/b", "plain": `a\/b`, "single": `a\/b`, "escaped": `a\/b`, "a/b": float64(1), "duplicate": float64(0)} {
		if object[key] != want {
			t.Fatalf("%s: actual=%#v expected=%#v", key, object[key], want)
		}
	}
	result = parsed(t, input, standard(), "reject")
	expected := strings.LastIndex(input, "duplicate") + 1
	if result.Kind != "duplicate" || result.Line != 1 || result.Column != expected {
		t.Fatalf("position expected=%d %+v", expected, result)
	}
	multiline := "%YAML 1.2\n---\nquote: \"a\\/b\"\nblock: |\n  a\\/b\n...\n%YAML 1.2\n---\n\"c\\/d\""
	result = parsed(t, multiline, standard(), "reject")
	if result.Kind != "" || result.Documents != 2 || result.Tokens[4].Text != "a\\/b\n" || result.Tokens[5].Text != "c/d" {
		t.Fatal(result)
	}
	if result := parsed(t, `["a\/b", "invalid\q"]`, standard(), "reject"); result.Kind != "syntax" {
		t.Fatal(result)
	}
}

func BenchmarkWideEscapedSlashSequence(b *testing.B) {
	input := "[" + strings.Repeat(`"a\/b",`, 10000) + `"end"]`
	limits := limitsJSON(standard())
	for b.Loop() {
		Parse(input, limits, "reject")
	}
}

func TestEmptyEmissionAndParserErrorCoordinates(t *testing.T) {
	var result Response
	if err := json.Unmarshal([]byte(Emit("[]", limitsJSON(standard()), 0, 2)), &result); err != nil {
		t.Fatal(err)
	}
	if result.Kind != "" || result.Output != "" {
		t.Fatal(result)
	}
	result = parsed(t, "%YAML 1.2\n%YAML 1.2\n---\na", standard(), "reject")
	if result.Kind != "syntax" || result.Line != 0 || result.Column != 0 {
		t.Fatal(result)
	}
}
