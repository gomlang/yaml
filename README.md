# yaml

Bounded YAML parsing, emission, and Serde conversion for GoML. Requires a current GoML toolchain supporting unversioned registry dependencies; see [the pinned ecosystem toolchain](https://github.com/gomlang/workflows/blob/main/ci/toolchain.json). Parsing, scalar resolution, bounded alias expansion, emission, and Serde conversion are implemented in GoML. There are no native adapters or external Go module dependencies.

The library implements an application-oriented subset of the [YAML 1.2.2 core schema](https://yaml.org/spec/1.2.2/#103-core-schema). It supports block and flow mappings/sequences, plain/single/double-quoted scalars, quoted escapes including `\/`, literal/folded block scalars with chomping and indentation indicators, comments, anchors and bounded alias expansion, explicit core tags, `%YAML 1.2` directives, and multiple documents. It does not claim complete YAML conformance or preserve presentation details.

```toml
[dependencies]
"ecosystem::yaml" = true
```

```goml
use ecosystem::yaml;
use std::serde::{Deserialize, Serialize};

#[derive(Serialize, Deserialize)]
struct Settings {
    name: string,
    port: u16,
    routes: Vec[string],
}

fn load(input: string) -> Result[Settings, yaml::Error] {
    yaml::from_string(input)
}

fn save(settings: Settings) -> Result[string, yaml::Error] {
    yaml::to_string(settings)
}
```

Consumers only need the GoML dependency declaration above; no `go.mod` or `[native]` configuration is required. `examples/basic` is an executable package inside this module. The separate `testdata/downstream/native` module defines consumer types and loads YAML through `ecosystem::config::Source::dynamic`, exercising layering, provenance, typed decoding, reload, and retention of the last good snapshot after invalid YAML.

## API

| API | Result |
| --- | --- |
| `parse(input)` / `parse_with_options(input, options)` | One dynamic `Value`; empty input becomes `Null`; multiple documents fail |
| `parse_documents(input)` / `parse_documents_with_options(input, options)` | A vector of documents; empty input is an empty vector |
| `emit(value)` / `emit_with_options(value, options)` | YAML for one dynamic value |
| `emit_documents(values)` / `emit_documents_with_options(values, options)` | YAML document stream |
| `from_string[T: Deserialize](input)` / `from_string_with_options[T](input, options)` | Typed Serde decoding |
| `to_string[T: Serialize](value)` / `to_string_with_options[T](value, options)` | Typed Serde emission |
| `to_json(value)` / `from_json(value)` | Checked conversion with the standard dynamic JSON model |
| `Value::field(name)`, `as_string()`, `as_sequence()` | Optional dynamic access |

`Value` has `Null`, `Bool(bool)`, `Integer(string)`, `Float(string)`, `String(string)`, `Sequence(Vec[Value])`, and ordered `Mapping(Vec[(Value, Value)])` variants. Integers retain exact decimal text rather than passing through float64. Finite floats use float64 semantics and canonical text; dynamic non-finite values use `.inf`, `-.inf`, and `.nan`. Manually constructed numeric values are validated before emission or JSON conversion. Emission quotes string scalars so values such as `"yes"`, `"true"`, and `"123"` retain their type; multiline strings use literal block notation when their characters allow it. Mapping key order is retained. `Value` equality compares the ordered representation, not mathematical equivalence of arbitrary manually supplied number spellings.

Scalar mapping keys may be strings, integers, floats, booleans, or null. Collections as keys are rejected. Duplicate keys default to `DuplicateKeys::Reject`; `FirstWins` and `LastWins` are explicit options. Numeric aliases such as `01` and `+1` collide after core-schema normalization; float negative and positive zero collide. Integer and float keys remain distinct YAML tags. Even ignored duplicate values are validated and charged to the same budgets. Emission always rejects duplicate keys.

Serde uses the standard JSON data conventions: struct fields are string mapping keys, `Option::None` is null, sequences/tuples are YAML sequences, and enum encoding follows `std::json`. Serde and JSON conversion reject non-string mapping keys and non-finite floats. No implicit conversion of YAML booleans or numeric keys into strings occurs. Existing standard Serde policies remain available through derives. Typed mismatch errors retain the standard JSON field path in the message, but do not map that path back to a YAML source location.

## Schema and compatibility

Plain `true`/`false`, null spellings, decimal integers, `0o` octal and `0x` hexadecimal values follow the YAML 1.2 core schema. `yes`, `no`, `on`, `off`, dates, `0b` binary integers, and underscored numbers remain strings. Decimal integers with leading zeroes are decimal, not YAML 1.1 octal. Explicit `!!str` and the non-specific `!` scalar tag force strings. `!!null`, `!!bool`, `!!int`, and `!!float` validate their contents. Explicit `!!seq`/`!!map` collection tags are supported. Both accepted version-directive spellings (`1.1` and `1.2`) use this library's core 1.2 scalar schema.

Explicit `!!float` accepts decimal integer spellings such as `012` and preserves
the floating-point sign of `-0`, including quoted scalars. Hexadecimal and octal
spellings are integer syntax; they are not accepted as explicit
floats or manually constructed `Value::Float` values during emission.

Unknown/custom tags, `!!binary`, timestamps as typed tags, YAML 1.1 merge keys (`<<`), complex mapping keys, recursive aliases, and references to anchors in other documents fail explicitly. Quoted `"<<"`, `!!str <<`, and `! <<` remain ordinary string keys and use the usual duplicate-key policy. Alias expansion produces values and does not preserve graph sharing or anchor names. Comments, quoting style, tags, and exact number spelling are not round-tripped.

Literal NEL (`U+0085`), LS (`U+2028`), and PS (`U+2029`) characters in source remain rejected; use double-quoted Unicode escapes for those string values. Emission always escapes these characters, including in strings that also contain ordinary newlines, so emitted values round-trip. UTF-8 input, LF/CRLF/CR line endings, and a UTF-8 stream BOM are supported. The checked-in reference subset and regression tests document the supported application grammar; the library does not claim complete YAML conformance.

Version directives and double-quoted `\/` escapes are parsed directly. Quoted directive text, plain/single-quoted/block scalar contents, and escaped backslashes retain their literal contents. Node positions count Unicode scalar values in the original source line.

Slash escapes also work after complete verbatim core tags such as
`!<tag:yaml.org,2002:str>`, including with anchors and intervening comments.
Commas inside these tag URIs do not end the tag; the surrounding scalar keeps
the same validation and decoded-byte limits.

## Bounds and diagnostics

`Options::standard()` sets indentation to two spaces and rejects duplicate keys. Indentation accepts 1–8 spaces. `Limits::standard()` supplies:

| Budget | Default | Maximum configurable value |
| --- | ---: | ---: |
| Input bytes | 1 MiB | 16 MiB |
| Expanded nesting depth | 128 | 512 |
| Source/expanded node visits | 100,000 | 1,000,000 |
| Alias dereferences | 1,000 | 100,000 |
| Expanded scalar bytes | 4 MiB | 64 MiB |
| Documents | 64 | 10,000 |
| Emitted bytes | 4 MiB | 64 MiB |

Depth starts at one for the document root. Keys and values count as nodes. Each alias dereference and each expanded target visit is charged. Scalar bytes charge the larger of the raw decoded scalar and its normalized representation, including repeated aliases and discarded duplicate values. Emission applies this per-scalar maximum across the entire document stream; shorter normalized scalars do not release another scalar's budget, and emitted `null` costs four scalar bytes. Numeric scalar spellings have a separate fixed 4096-byte ceiling before expensive integer conversion. Invalid options return `ErrorKind::Argument`.

Input length is checked before scanning. The parser constructs an unexpanded node graph, enforcing the source-node budget and a fixed construction-depth ceiling of 512. User depth and expanded-node limits apply during bounded resolution; alias targets are never copied during parsing. Alias cycles and cross-document references fail recoverably. Scalar and alias budgets accumulate across the document stream, including ignored duplicate values. Parsing handles directives and escapes in one pass without normalization retries. Emission validates values and budgets before writing through an output-size check. Serde's intermediate JSON operations have bounds derived from these limits; user-supplied serializer code itself remains the caller's responsibility.

Errors expose `kind`, `message`, and one-based `line`/`column`. An unavailable coordinate is zero. Semantic errors such as duplicate keys, unsupported tags, and alias violations retain node positions. Grammar errors retain zero structured coordinates; scalar-validation and semantic failures retain the relevant node location. All failures return `Result` values.

## Validation

From the library root, run the isolated-registry verification:

```sh
(cd ../workflows && just ecosystem-test yaml)
```

For individual checks, provision the ecosystem registry first:

```sh
export GOML_HOME="$(cd ../workflows && just ecosystem-test --registry-only yaml)"
goml fmt --check
goml check
goml test
(cd testdata/downstream/native && goml fmt --check && goml test)
goml run --example basic
```

Use an isolated ecosystem registry containing `ecosystem::config` for consumer tests. The ecosystem verification runner provisions it. GoML tests compare all 36 pinned official YAML test-suite fixtures against their independent JSON results and retain 20 additional official syntax regressions; see [fixture provenance and license](tests/data/reference/README.md). Additional tests cover core-schema differences, explicit/non-specific tags, alias bombs and cycles, every configurable budget, duplicate-key policy, source positions, Unicode/BOM/line endings, normalization, invalid dynamic values, bounded emission, and typed configuration integration.
