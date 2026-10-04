package adapter

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEmissionEscapesLegacyUnicodeLineSeparatorsInMultilineStrings(t *testing.T) {
	for _, separator := range []string{"\u0085", "\u2028", "\u2029"} {
		for _, value := range []string{"a" + separator + "b", "a\nb" + separator + "c", separator + "\n", "\n" + separator} {
			t.Run(value, func(t *testing.T) {
				tokens, err := json.Marshal([]Token{{Kind: "string", Text: value}})
				if err != nil {
					t.Fatal(err)
				}
				var emitted Response
				if err := json.Unmarshal([]byte(Emit(string(tokens), limitsJSON(standard()), 1, 2)), &emitted); err != nil {
					t.Fatal(err)
				}
				if emitted.Kind != "" {
					t.Fatal(emitted)
				}
				if strings.ContainsAny(emitted.Output, "\u0085\u2028\u2029") {
					t.Errorf("emission contains a literal legacy line separator: %q", emitted.Output)
				}
				reparsed := parsed(t, emitted.Output, standard(), "reject")
				if reparsed.Kind != "" || len(reparsed.Tokens) != 1 || reparsed.Tokens[0].Text != value {
					t.Fatalf("round trip of %q through %q: %+v", value, emitted.Output, reparsed)
				}
			})
		}
	}
}
