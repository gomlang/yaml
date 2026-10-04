package adapter

import "testing"

func TestNonSpecificTagDisablesMergeKeyResolution(t *testing.T) {
	// YAML 1.2.2 sections 6.9.1 and 10.3.2 resolve an explicitly tagged ! scalar as a string.
	// The yaml 2.8.1 JavaScript parser independently resolves these to {"<<": "value"}.
	for _, input := range []string{"! <<: value", "{! <<: value}", "&key ! <<: value", "! &key <<: value", "? ! <<\n: value"} {
		t.Run(input, func(t *testing.T) {
			result := parsed(t, input, standard(), "reject")
			if result.Kind != "" || len(result.Tokens) != 3 || result.Tokens[1].Kind != "string" || result.Tokens[1].Text != "<<" || result.Tokens[2].Text != "value" {
				t.Fatalf("%+v", result)
			}
		})
	}
}

func TestNonSpecificMergeSpellingUsesOrdinaryDuplicateKeyPolicy(t *testing.T) {
	input := "'<<': first\n! <<: second"
	if result := parsed(t, input, standard(), "reject"); result.Kind != "duplicate" {
		t.Fatalf("duplicate string keys: %+v", result)
	}
	for _, policy := range []string{"first", "last"} {
		result := parsed(t, input, standard(), policy)
		want := "first"
		if policy == "last" {
			want = "second"
		}
		if result.Kind != "" || len(result.Tokens) != 3 || result.Tokens[2].Text != want {
			t.Fatalf("%s: %+v", policy, result)
		}
	}
	for _, input := range []string{"<<: {a: 1}", "!!merge <<: {a: 1}"} {
		if result := parsed(t, input, standard(), "reject"); result.Kind != "unsupported" {
			t.Fatalf("actual merge key must remain unsupported: %+v", result)
		}
	}
}
