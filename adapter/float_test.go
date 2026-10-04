package adapter

import (
	"encoding/json"
	"testing"
)

func TestExplicitFloatUsesOriginalDecimalSpelling(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"!!float -0", "-0.0"},
		{"!!float '-00'", "-0.0"},
		{"!!float \"-000\"", "-0.0"},
		{"!!float +0", "0.0"},
		{"!!float 012", "12.0"},
		{"!!float -12", "-12.0"},
	} {
		t.Run(test.input, func(t *testing.T) {
			result := parsed(t, test.input, standard(), "reject")
			if result.Kind != "" || len(result.Tokens) != 1 || result.Tokens[0].Kind != "float" || result.Tokens[0].Text != test.want {
				t.Fatalf("got %+v, want float %s", result, test.want)
			}
		})
	}
	for _, text := range []string{"-0", "-00"} {
		raw, err := json.Marshal([]Token{{Kind: "float", Text: text}})
		if err != nil {
			t.Fatal(err)
		}
		var result Response
		if err := json.Unmarshal([]byte(Emit(string(raw), limitsJSON(standard()), 1, 2)), &result); err != nil {
			t.Fatal(err)
		}
		if result.Kind != "" {
			t.Fatal(result)
		}
		reparsed := parsed(t, result.Output, standard(), "reject")
		if reparsed.Kind != "" || len(reparsed.Tokens) != 1 || reparsed.Tokens[0].Text != "-0.0" {
			t.Fatalf("emitting %s lost the sign: %+v", text, reparsed)
		}
	}
}

func TestExplicitFloatRejectsNondecimalIntegerSpellings(t *testing.T) {
	for _, text := range []string{"0xFF", "0o17", "0x0", "0o0"} {
		for _, input := range []string{"!!float " + text, "!!float '" + text + "'"} {
			result := parsed(t, input, standard(), "reject")
			if result.Kind != "syntax" || len(result.Tokens) != 0 {
				t.Errorf("%s: %+v", input, result)
			}
		}
		raw, err := json.Marshal([]Token{{Kind: "float", Text: text}})
		if err != nil {
			t.Fatal(err)
		}
		var result Response
		if err := json.Unmarshal([]byte(Emit(string(raw), limitsJSON(standard()), 1, 2)), &result); err != nil {
			t.Fatal(err)
		}
		if result.Kind != "syntax" || result.Output != "" {
			t.Errorf("float emission accepted %s: %+v", text, result)
		}
	}
}
