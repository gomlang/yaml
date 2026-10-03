# Published YAML reference fixtures

The 36 input/expected JSON pairs in this directory are copied without alteration from [yaml/yaml-test-suite](https://github.com/yaml/yaml-test-suite), release [`data-2022-01-17`](https://github.com/yaml/yaml-test-suite/tree/data-2022-01-17), commit `6e6c296ae9c9d2d5c4134b4b64d01b29ac19ff6f`.

Each `<ID>.yaml` is the upstream `<ID>/in.yaml`; each `<ID>.json` is `<ID>/in.json`. The filenames retain the upstream test IDs. They cover nested block/flow collections, implicit and explicit scalars, escapes, folded/literal blocks, anchors/aliases, and document streams. `adapter/adapter_test.go::TestPublishedYAMLReferenceCorpus` compares every parsed document with the independently supplied JSON expectation. All 36 fixtures must pass; this selection is not a claim that the complete YAML test suite passes.

`LICENSE` is the upstream YAML test suite MIT license, obtained from its `main/License`. It applies to these third-party fixtures, not to the surrounding GoML library. The library does not copy the native parser implementation; its separately versioned module dependency carries its own license.
