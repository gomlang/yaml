# Published YAML reference fixtures

The 36 input/expected JSON pairs in this directory are copied without alteration from [yaml/yaml-test-suite](https://github.com/yaml/yaml-test-suite), release [`data-2022-01-17`](https://github.com/yaml/yaml-test-suite/tree/data-2022-01-17), commit `6e6c296ae9c9d2d5c4134b4b64d01b29ac19ff6f`.

Each `<ID>.yaml` is the upstream `<ID>/in.yaml`; each `<ID>.json` is `<ID>/in.json`. The filenames retain the upstream test IDs. They cover nested block/flow collections, implicit and explicit scalars, escapes, folded/literal blocks, anchors/aliases, and document streams. `tests/reference_test.goml::published_yaml_reference_corpus` compares every parsed document with the independently supplied JSON expectation. All 36 fixtures must pass; this selection is not a claim that the complete YAML test suite passes.

`LICENSE` is the upstream YAML test suite MIT license, obtained from its `main/License`. It applies to these third-party fixtures, not to the surrounding GoML library. The library parser is implemented in GoML; these fixtures and their independent JSON expectations retain their upstream license.

`../syntax-regressions.json` additionally embeds 20 unmodified `in.yaml` inputs from the same pinned release, identified by their upstream IDs. Their typed node expectations retain this library's core-schema distinctions, ordered mappings, and scalar-key support. These embedded source fixtures are covered by the same `LICENSE`. They are exercised by `tests/syntax_regression_test.goml`.

Three expectations intentionally correct the former adapter's syntax interpretation: Y2GN treats `an:chor` as the full anchor name, yielding the string `value` as required by upstream `in.json`; 4ABK treats the colon in `omitted value:,` as the mapping separator, matching upstream `test.event` and `out.yaml`. 652Z retains the leading `?` in the plain scalar key `?foo`, matching upstream `test.event`. These corrections do not alter core-schema resolution. Malformed document-marker, comment, property-separation, and compact-collection cases use explicit syntax-error regressions; permissive behavior of the former Go parser is not a conformance oracle.
