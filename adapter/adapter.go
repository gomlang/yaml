package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

type Token struct {
	Kind   string `json:"kind"`
	Text   string `json:"text"`
	Size   int    `json:"size"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type Response struct {
	Kind      string  `json:"kind"`
	Message   string  `json:"message"`
	Line      int     `json:"line"`
	Column    int     `json:"column"`
	Documents int     `json:"documents"`
	Tokens    []Token `json:"tokens"`
	Output    string  `json:"output"`
}

type Limits struct {
	Input     int `json:"max_input_bytes"`
	Depth     int `json:"max_depth"`
	Nodes     int `json:"max_nodes"`
	Aliases   int `json:"max_alias_expansions"`
	Decoded   int `json:"max_decoded_bytes"`
	Documents int `json:"max_documents"`
	Output    int `json:"max_output_bytes"`
}

type nodeValue struct {
	token    Token
	children []*nodeValue
}

type failure struct {
	kind    string
	message string
	line    int
	column  int
}

func (f *failure) Error() string { return f.message }

func fail(kind, message string, node *yaml.Node) error {
	f := &failure{kind: kind, message: message}
	if node != nil {
		f.line, f.column = node.Line, node.Column
	}
	return f
}

func response(result Response, err error) string {
	if result.Tokens == nil {
		result.Tokens = []Token{}
	}
	if err != nil {
		result.Tokens = []Token{}
		result.Documents = 0
		result.Output = ""
		if f, ok := err.(*failure); ok {
			result.Kind, result.Message, result.Line, result.Column = f.kind, f.message, f.line, f.column
		} else {
			result.Kind, result.Message = "syntax", err.Error()
		}
	}
	data, err := json.Marshal(result)
	if err != nil {
		return `{"kind":"bridge","message":"cannot encode YAML bridge result","line":0,"column":0,"documents":0,"tokens":[],"output":""}`
	}
	return string(data)
}

var decimalInteger = regexp.MustCompile(`^[+-]?[0-9]+$`)
var octalInteger = regexp.MustCompile(`^0o[0-7]+$`)
var hexInteger = regexp.MustCompile(`^0x[0-9a-fA-F]+$`)
var decimalFloat = regexp.MustCompile(`^[+-]?(?:\.[0-9]+|[0-9]+(?:\.[0-9]*)?)(?:[eE][+-]?[0-9]+)?$`)

func validateLimits(raw string) (Limits, error) {
	var l Limits
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		return l, fail("argument", "invalid YAML limits", nil)
	}
	if l.Input < 0 || l.Input > 16<<20 || l.Depth < 1 || l.Depth > 512 || l.Nodes < 1 || l.Nodes > 1000000 || l.Aliases < 0 || l.Aliases > 100000 || l.Decoded < 0 || l.Decoded > 64<<20 || l.Documents < 1 || l.Documents > 10000 || l.Output < 0 || l.Output > 64<<20 {
		return l, fail("argument", "YAML limits outside supported bounds", nil)
	}
	return l, nil
}

func coreScalar(node *yaml.Node) (Token, error) {
	token := Token{Line: node.Line, Column: node.Column, Text: node.Value}
	explicit := node.Style&yaml.TaggedStyle != 0
	tag := node.Tag
	if !explicit && node.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		token.Kind = "string"
		return token, nil
	}
	value := node.Value
	if explicit && (tag == "!!str" || tag == "tag:yaml.org,2002:str") {
		token.Kind = "string"
		return token, nil
	}
	kind := "string"
	text := value
	switch value {
	case "", "~", "null", "Null", "NULL":
		kind, text = "null", ""
	case "true", "True", "TRUE":
		kind, text = "bool", "true"
	case "false", "False", "FALSE":
		kind, text = "bool", "false"
	case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF":
		kind, text = "float", ".inf"
	case "-.inf", "-.Inf", "-.INF":
		kind, text = "float", "-.inf"
	case ".nan", ".NaN", ".NAN":
		kind, text = "float", ".nan"
	default:
		base, digits := 10, value
		integer := decimalInteger.MatchString(value)
		if octalInteger.MatchString(value) {
			base, digits, integer = 8, value[2:], true
		}
		if hexInteger.MatchString(value) {
			base, digits, integer = 16, value[2:], true
		}
		if integer {
			if len(value) > 4096 {
				return token, fail("limit", "YAML numeric scalar exceeds 4096 bytes", node)
			}
			number, ok := new(big.Int).SetString(digits, base)
			if !ok {
				return token, fail("syntax", "invalid YAML integer", node)
			}
			kind, text = "integer", number.String()
		} else if decimalFloat.MatchString(value) {
			if len(value) > 4096 {
				return token, fail("limit", "YAML numeric scalar exceeds 4096 bytes", node)
			}
			number, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsInf(number, 0) {
				return token, fail("unsupported", "YAML float exceeds finite float64 range", node)
			}
			kind, text = "float", strconv.FormatFloat(number, 'g', -1, 64)
			if !strings.ContainsAny(text, ".eE") {
				text += ".0"
			}
		}
	}
	if explicit {
		switch tag {
		case "!!str", "tag:yaml.org,2002:str":
			kind, text = "string", value
		case "!!null", "tag:yaml.org,2002:null":
			if kind != "null" {
				return token, fail("syntax", "invalid explicit null scalar", node)
			}
		case "!!bool", "tag:yaml.org,2002:bool":
			if kind != "bool" {
				return token, fail("syntax", "invalid explicit boolean scalar", node)
			}
		case "!!int", "tag:yaml.org,2002:int":
			if kind != "integer" {
				return token, fail("syntax", "invalid explicit integer scalar", node)
			}
		case "!!float", "tag:yaml.org,2002:float":
			if kind == "integer" {
				if !decimalInteger.MatchString(value) {
					return token, fail("syntax", "invalid explicit float scalar", node)
				}
				number, err := strconv.ParseFloat(value, 64)
				if err != nil {
					return token, fail("unsupported", "YAML float exceeds finite float64 range", node)
				}
				kind, text = "float", strconv.FormatFloat(number, 'g', -1, 64)
				if !strings.ContainsAny(text, ".eE") {
					text += ".0"
				}
			}
			if kind != "float" {
				return token, fail("syntax", "invalid explicit float scalar", node)
			}
		default:
			return token, fail("unsupported", "unsupported YAML tag: "+tag, node)
		}
	}
	token.Kind, token.Text = kind, text
	return token, nil
}

type sourceLine struct {
	text  string
	start int
}

func sourceLines(input string) []sourceLine {
	lines := []sourceLine{}
	for start := 0; ; {
		end := start
		for end < len(input) && input[end] != '\r' && input[end] != '\n' {
			end++
		}
		content := start
		if start == 0 && strings.HasPrefix(input[start:end], "\ufeff") {
			content += len("\ufeff")
		}
		lines = append(lines, sourceLine{text: input[content:end], start: content})
		if end == len(input) {
			break
		}
		start = end + 1
		if input[end] == '\r' && start < len(input) && input[start] == '\n' {
			start++
		}
	}
	return lines
}

type sourceLookup struct {
	input     string
	lines     []sourceLine
	offsets   map[int][]int
	tags      map[*yaml.Node]bool
	positions []int
}

func (source *sourceLookup) position(node *yaml.Node) int {
	if node.Line < 1 || node.Line > len(source.lines) || node.Column < 1 {
		return -1
	}
	line := node.Line - 1
	offsets, exists := source.offsets[line]
	if !exists {
		for offset := range source.lines[line].text {
			offsets = append(offsets, offset)
		}
		offsets = append(offsets, len(source.lines[line].text))
		source.offsets[line] = offsets
	}
	if node.Column > len(offsets) {
		return -1
	}
	return source.lines[line].start + offsets[node.Column-1]
}

func (source *sourceLookup) setPositions(members map[*yaml.Node]bool) {
	source.positions = source.positions[:0]
	for node := range members {
		if position := source.position(node); position >= 0 {
			source.positions = append(source.positions, position)
		}
	}
	sort.Ints(source.positions)
}

func (source *sourceLookup) nonSpecificTag(node *yaml.Node) bool {
	if value, exists := source.tags[node]; exists {
		return value
	}
	value := source.findNonSpecificTag(node)
	source.tags[node] = value
	return value
}

func (source *sourceLookup) findNonSpecificTag(node *yaml.Node) bool {
	position := source.position(node)
	if position < 0 {
		return false
	}
	next := sort.Search(len(source.positions), func(index int) bool { return source.positions[index] > position })
	end := len(source.input)
	if next < len(source.positions) {
		end = source.positions[next]
	}
	text := source.input[position:end]
	for {
		text = strings.TrimLeft(text, " \t\r\n")
		if text == "" {
			return false
		}
		if text[0] == '#' {
			next := strings.IndexAny(text, "\r\n")
			if next < 0 {
				return false
			}
			text = text[next:]
			continue
		}
		if text[0] != '&' && text[0] != '!' {
			return false
		}
		end := strings.IndexAny(text, " \t\r\n,[]{}")
		if end < 0 {
			end = len(text)
		}
		if end == 0 {
			return false
		}
		property := text[:end]
		if property == "!" {
			return true
		}
		text = text[end:]
	}
}

type resolver struct {
	limits     Limits
	duplicates string
	nodes      int
	aliases    int
	decoded    int
	active     map[*yaml.Node]bool
	members    map[*yaml.Node]bool
	source     *sourceLookup
}

func (r *resolver) resolve(node *yaml.Node, depth int) (*nodeValue, error) {
	if depth > r.limits.Depth {
		return nil, fail("limit", "YAML nesting exceeds max_depth", node)
	}
	r.nodes++
	if r.nodes > r.limits.Nodes {
		return nil, fail("limit", "YAML expanded nodes exceed max_nodes", node)
	}
	if r.active[node] {
		return nil, fail("alias", "recursive YAML alias", node)
	}
	r.active[node] = true
	defer delete(r.active, node)
	if node.Kind == yaml.AliasNode {
		r.aliases++
		if r.aliases > r.limits.Aliases {
			return nil, fail("limit", "YAML aliases exceed max_alias_expansions", node)
		}
		if node.Alias == nil || !r.members[node.Alias] {
			return nil, fail("alias", "YAML alias must reference an anchor in the same document", node)
		}
		return r.resolve(node.Alias, depth)
	}
	value := &nodeValue{token: Token{Line: node.Line, Column: node.Column}}
	switch node.Kind {
	case yaml.ScalarNode:
		if len(node.Value) > r.limits.Decoded-r.decoded {
			return nil, fail("limit", "YAML expanded scalar bytes exceed max_decoded_bytes", node)
		}
		scalar := node
		if r.source != nil && r.source.nonSpecificTag(node) {
			copy := *node
			copy.Tag, copy.Style = "!!str", yaml.TaggedStyle
			scalar = &copy
		}
		token, err := coreScalar(scalar)
		if err != nil {
			return nil, err
		}
		r.decoded += max(len(token.Text), len(node.Value))
		if r.decoded > r.limits.Decoded {
			return nil, fail("limit", "YAML expanded scalar bytes exceed max_decoded_bytes", node)
		}
		value.token = token
	case yaml.SequenceNode:
		if node.Tag != "!!seq" {
			return nil, fail("unsupported", "unsupported YAML sequence tag: "+node.Tag, node)
		}
		value.token.Kind = "sequence"
		for _, child := range node.Content {
			item, err := r.resolve(child, depth+1)
			if err != nil {
				return nil, err
			}
			value.children = append(value.children, item)
		}
		value.token.Size = len(value.children)
	case yaml.MappingNode:
		if node.Tag != "!!map" {
			return nil, fail("unsupported", "unsupported YAML mapping tag: "+node.Tag, node)
		}
		value.token.Kind = "mapping"
		seen := map[string]int{}
		for index := 0; index < len(node.Content); index += 2 {
			keyNode := node.Content[index]
			if keyNode.Tag == "!!merge" && !(r.source != nil && r.source.nonSpecificTag(keyNode)) {
				return nil, fail("unsupported", "YAML merge keys are not supported", keyNode)
			}
			key, err := r.resolve(keyNode, depth+1)
			if err != nil {
				return nil, err
			}
			if key.token.Kind == "mapping" || key.token.Kind == "sequence" {
				return nil, fail("unsupported", "YAML mapping keys must be scalars", keyNode)
			}
			identity := key.token.Kind + ":" + key.token.Text
			if key.token.Kind == "float" && key.token.Text == "-0.0" {
				identity = "float:0.0"
			}
			prior, exists := seen[identity]
			if exists && r.duplicates == "reject" {
				return nil, fail("duplicate", "duplicate YAML mapping key", keyNode)
			}
			item, err := r.resolve(node.Content[index+1], depth+1)
			if err != nil {
				return nil, err
			}
			if exists {
				if r.duplicates == "last" {
					value.children[prior+1] = item
				}
			} else {
				seen[identity] = len(value.children)
				value.children = append(value.children, key, item)
			}
		}
		value.token.Size = len(value.children) / 2
	default:
		return nil, fail("unsupported", "unsupported YAML node kind", node)
	}
	return value, nil
}

func flatten(value *nodeValue, tokens *[]Token) {
	*tokens = append(*tokens, value.token)
	for _, child := range value.children {
		flatten(child, tokens)
	}
}

func Parse(input, rawLimits, duplicates string) string {
	result := Response{}
	limits, err := validateLimits(rawLimits)
	if err != nil {
		return response(result, err)
	}
	if duplicates != "reject" && duplicates != "first" && duplicates != "last" {
		return response(result, fail("argument", "unknown duplicate key policy", nil))
	}
	if len(input) > limits.Input {
		return response(result, fail("limit", "YAML input exceeds max_input_bytes", nil))
	}
	if strings.ContainsAny(input, "\u0085\u2028\u2029") {
		return response(result, fail("unsupported", "literal legacy Unicode line separators require escaped scalar notation", nil))
	}
	if !utf8.ValidString(input) {
		return response(result, fail("syntax", "YAML input must be UTF-8", nil))
	}
	modified := input
	work := &parseWork{}
	slashes := false
	edits := []columnEdit{}
	for {
		if err := work.charge(len(modified)); err != nil {
			return response(Response{}, err)
		}
		result, err = parseDocuments(modified, limits, duplicates)
		if err == nil {
			return originalResponse(result, nil, edits)
		}
		if next, ok := normalizeVersion(modified, err); ok {
			modified = next
			continue
		}
		if !slashes && strings.Contains(err.Error(), "found unknown escape character") {
			slashes = true
			next, mapping, problem := normalizeSlashes(modified, limits, work)
			if problem != nil {
				return response(Response{}, problem)
			}
			if next != modified {
				modified, edits = next, mapping
				continue
			}
		}
		return originalResponse(result, err, edits)
	}
}

var incompatibleVersion = regexp.MustCompile(`^yaml: (?:line ([0-9]+): )?found incompatible YAML document$`)
var version12 = regexp.MustCompile(`^%YAML[ \t]+1\.2(?:[ \t]*(?:#.*)?\r?)$`)

func normalizeVersion(input string, err error) (string, bool) {
	match := incompatibleVersion.FindStringSubmatch(err.Error())
	if match == nil {
		return "", false
	}
	line := 0
	if match[1] != "" {
		line, _ = strconv.Atoi(match[1])
	}
	lines := sourceLines(input)
	if line >= len(lines) || !version12.MatchString(lines[line].text) {
		return "", false
	}
	position := lines[line].start + strings.Index(lines[line].text, "1.2") + 2
	return input[:position] + "1" + input[position+1:], true
}

func parseDocuments(input string, limits Limits, duplicates string) (Response, error) {
	result := Response{}
	decoder := yaml.NewDecoder(strings.NewReader(input))
	resolver := resolver{limits: limits, duplicates: duplicates, active: map[*yaml.Node]bool{}, source: &sourceLookup{input: input, lines: sourceLines(input), offsets: map[int][]int{}, tags: map[*yaml.Node]bool{}}}
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, err
		}
		result.Documents++
		if result.Documents > limits.Documents {
			return result, fail("limit", "YAML stream exceeds max_documents", &document)
		}
		if len(document.Content) != 1 {
			return result, fail("syntax", "invalid YAML document", &document)
		}
		resolver.members = map[*yaml.Node]bool{}
		pending := []*yaml.Node{document.Content[0]}
		for len(pending) > 0 {
			node := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			resolver.members[node] = true
			if len(resolver.members) > limits.Nodes {
				return result, fail("limit", "YAML source nodes exceed max_nodes", node)
			}
			pending = append(pending, node.Content...)
		}
		resolver.source.setPositions(resolver.members)
		value, err := resolver.resolve(document.Content[0], 1)
		if err != nil {
			return result, err
		}
		flatten(value, &result.Tokens)
	}
	return result, nil
}

type boundedWriter struct {
	buffer bytes.Buffer
	limit  int
}

func (w *boundedWriter) Write(data []byte) (int, error) {
	if len(data) > w.limit-w.buffer.Len() {
		return 0, fail("limit", "YAML output exceeds max_output_bytes", nil)
	}
	return w.buffer.Write(data)
}

func makeNode(tokens []Token, index *int, depth int, limits Limits) (*yaml.Node, error) {
	if depth > limits.Depth {
		return nil, fail("limit", "YAML nesting exceeds max_depth", nil)
	}
	if *index >= len(tokens) {
		return nil, fail("bridge", "truncated YAML token stream", nil)
	}
	token := tokens[*index]
	*index++
	node := &yaml.Node{Value: token.Text}
	switch token.Kind {
	case "null":
		node.Kind, node.Tag, node.Value = yaml.ScalarNode, "!!null", "null"
	case "bool":
		if token.Text != "true" && token.Text != "false" {
			return nil, fail("argument", "invalid boolean value", nil)
		}
		node.Kind, node.Tag = yaml.ScalarNode, "!!bool"
	case "integer", "float":
		tag := "!!int"
		if token.Kind == "float" {
			tag = "!!float"
		}
		scalar := &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Style: yaml.TaggedStyle, Value: token.Text}
		normalized, err := coreScalar(scalar)
		if err != nil {
			return nil, err
		}
		node.Kind, node.Tag, node.Value = yaml.ScalarNode, tag, normalized.Text
	case "string":
		if !utf8.ValidString(token.Text) {
			return nil, fail("argument", "YAML strings must be UTF-8", nil)
		}
		node.Kind, node.Tag, node.Style = yaml.ScalarNode, "!!str", yaml.DoubleQuotedStyle
		if strings.Contains(token.Text, "\n") && !strings.ContainsAny(token.Text, "\u0085\u2028\u2029") {
			node.Style = yaml.LiteralStyle
		}
	case "sequence", "mapping":
		if token.Size < 0 || token.Size > len(tokens) {
			return nil, fail("bridge", "invalid YAML child count", nil)
		}
		count := token.Size
		node.Kind, node.Tag = yaml.SequenceNode, "!!seq"
		if token.Kind == "mapping" {
			node.Kind, node.Tag, count = yaml.MappingNode, "!!map", count*2
		}
		for child := 0; child < count; child++ {
			value, err := makeNode(tokens, index, depth+1, limits)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, value)
		}
	default:
		return nil, fail("bridge", "invalid YAML token kind", nil)
	}
	return node, nil
}

func Emit(rawTokens, rawLimits string, documents, indent int) string {
	result := Response{}
	limits, err := validateLimits(rawLimits)
	if err != nil {
		return response(result, err)
	}
	if indent < 1 || indent > 8 {
		return response(result, fail("argument", "YAML indentation must be between 1 and 8", nil))
	}
	if documents < 0 || documents > limits.Documents {
		return response(result, fail("limit", "YAML stream exceeds max_documents", nil))
	}
	if len(rawTokens) > 256<<20 {
		return response(result, fail("limit", "YAML bridge input exceeds hard limit", nil))
	}
	var tokens []Token
	if err := json.Unmarshal([]byte(rawTokens), &tokens); err != nil {
		return response(result, fail("bridge", "invalid YAML token stream", nil))
	}
	if len(tokens) > limits.Nodes {
		return response(result, fail("limit", "YAML expanded nodes exceed max_nodes", nil))
	}
	total := 0
	for _, token := range tokens {
		total += len(token.Text)
		if total > limits.Decoded {
			return response(result, fail("limit", "YAML scalar bytes exceed max_decoded_bytes", nil))
		}
	}
	if documents == 0 && len(tokens) == 0 {
		return response(result, nil)
	}
	writer := &boundedWriter{limit: limits.Output}
	encoder := yaml.NewEncoder(writer)
	encoder.SetIndent(indent)
	index := 0
	for document := 0; document < documents; document++ {
		node, err := makeNode(tokens, &index, 1, limits)
		if err != nil {
			return response(result, err)
		}
		validator := resolver{limits: limits, duplicates: "reject", active: map[*yaml.Node]bool{}}
		if _, err := validator.resolve(node, 1); err != nil {
			return response(result, err)
		}
		if err := encoder.Encode(node); err != nil {
			if strings.Contains(err.Error(), "max_output_bytes") {
				return response(result, fail("limit", "YAML output exceeds max_output_bytes", nil))
			}
			return response(result, err)
		}
	}
	if index != len(tokens) {
		return response(result, fail("bridge", "trailing YAML tokens", nil))
	}
	if err := encoder.Close(); err != nil {
		return response(result, fmt.Errorf("YAML encoder close: %w", err))
	}
	result.Output = writer.buffer.String()
	return response(result, nil)
}
