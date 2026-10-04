package adapter

import (
	"encoding/json"
	"testing"
)

func TestEmissionChargesNormalizedScalarsAcrossDocuments(t *testing.T) {
	for _, test := range []struct {
		name      string
		tokens    []Token
		bytes     int
		documents int
	}{
		{"float normalization", []Token{{Kind: "float", Text: "1"}, {Kind: "float", Text: "2"}}, 6, 2},
		{"null spelling", []Token{{Kind: "null"}, {Kind: "null"}}, 8, 2},
		{"mixed normalization", []Token{{Kind: "sequence", Size: 2}, {Kind: "integer", Text: "000000"}, {Kind: "float", Text: "1"}}, 9, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			tokens, err := json.Marshal(test.tokens)
			if err != nil {
				t.Fatal(err)
			}
			for _, budget := range []int{test.bytes - 1, test.bytes} {
				limits := standard()
				limits.Decoded = budget
				var result Response
				if err := json.Unmarshal([]byte(Emit(string(tokens), limitsJSON(limits), test.documents, 2)), &result); err != nil {
					t.Fatal(err)
				}
				if budget < test.bytes {
					if result.Kind != "limit" {
						t.Fatalf("normalized stream exceeds %d bytes: %+v", budget, result)
					}
				} else {
					if result.Kind != "" {
						t.Fatal(result)
					}
					if reparsed := parsed(t, result.Output, limits, "reject"); reparsed.Kind != "" || reparsed.Documents != test.documents {
						t.Fatal(reparsed)
					}
				}
			}
		})
	}
}
