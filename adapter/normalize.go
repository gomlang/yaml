package adapter

import (
	"io"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

type parseWork struct {
	bytes    int
	attempts int
}

func (work *parseWork) charge(bytes int) error {
	if work.attempts >= 128 || bytes > (64<<20)-work.bytes {
		return fail("limit", "YAML normalization exceeds parse-work budget", nil)
	}
	work.bytes += bytes
	work.attempts++
	return nil
}

type slashCandidate struct {
	original int
	probe    int
}

type columnEdit struct {
	line     int
	original int
	modified int
}

func probeNodes(input string, limits Limits) ([]*yaml.Node, error) {
	decoder := yaml.NewDecoder(strings.NewReader(input))
	nodes := []*yaml.Node{}
	documents := 0
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if err == io.EOF {
			return nodes, nil
		}
		if err != nil {
			return nil, err
		}
		documents++
		if documents > limits.Documents {
			return nil, fail("limit", "YAML stream exceeds max_documents", &document)
		}
		pending := document.Content
		for len(pending) > 0 {
			node := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			nodes = append(nodes, node)
			if len(nodes) > limits.Nodes {
				return nil, fail("limit", "YAML source nodes exceed max_nodes", node)
			}
			pending = append(pending, node.Content...)
		}
	}
}

func quotedStart(input string, position int) int {
	for position < len(input) {
		switch input[position] {
		case ' ', '\t', '\r', '\n':
			position++
			continue
		case '#':
			for position < len(input) && input[position] != '\r' && input[position] != '\n' {
				position++
			}
			continue
		case '&', '!':
			for position < len(input) && !strings.ContainsRune(" \t\r\n,[]{}", rune(input[position])) {
				position++
			}
			continue
		case '"':
			return position
		default:
			return -1
		}
	}
	return -1
}

func normalizeSlashes(input string, limits Limits, work *parseWork) (string, []columnEdit, error) {
	candidates := []slashCandidate{}
	var probe strings.Builder
	start := 0
	for {
		next := strings.Index(input[start:], `\/`)
		if next < 0 {
			probe.WriteString(input[start:])
			break
		}
		next += start
		probe.WriteString(input[start:next])
		candidates = append(candidates, slashCandidate{original: next, probe: probe.Len()})
		probe.WriteString(`\u002f`)
		start = next + 2
	}
	if len(candidates) == 0 {
		return input, nil, nil
	}
	changed := probe.String()
	var nodes []*yaml.Node
	for {
		if err := work.charge(len(changed)); err != nil {
			return "", nil, err
		}
		var err error
		nodes, err = probeNodes(changed, limits)
		if err == nil {
			break
		}
		normalized, ok := normalizeVersion(changed, err)
		if !ok {
			return "", nil, err
		}
		changed = normalized
	}
	source := sourceLookup{input: changed, lines: sourceLines(changed), offsets: map[int][]int{}}
	lookup := map[int]int{}
	for _, candidate := range candidates {
		lookup[candidate.probe] = candidate.original
	}
	selected := map[int]bool{}
	for _, node := range nodes {
		if node.Kind != yaml.ScalarNode || node.Style&yaml.DoubleQuotedStyle == 0 {
			continue
		}
		position := source.position(node)
		if position < 0 {
			continue
		}
		position = quotedStart(changed, position)
		if position < 0 {
			continue
		}
		for position++; position < len(changed); position++ {
			if changed[position] == '"' {
				break
			}
			if changed[position] == '\\' {
				if original, exists := lookup[position]; exists {
					selected[original] = true
				}
				position++
			}
		}
	}
	if len(selected) == 0 {
		return input, nil, nil
	}
	lines := sourceLines(input)
	columns := map[int][]int{}
	var output strings.Builder
	edits := []columnEdit{}
	start = 0
	previousLine, delta := -1, 0
	for _, candidate := range candidates {
		if !selected[candidate.original] {
			continue
		}
		output.WriteString(input[start:candidate.original])
		output.WriteString(`\u002f`)
		start = candidate.original + 2
		line := sort.Search(len(lines), func(index int) bool { return lines[index].start > candidate.original }) - 1
		if line < 0 {
			return "", nil, fail("bridge", "invalid YAML source position", nil)
		}
		if line != previousLine {
			previousLine, delta = line, 0
		}
		offsets, exists := columns[line]
		if !exists {
			for offset := range lines[line].text {
				offsets = append(offsets, offset)
			}
			columns[line] = offsets
		}
		column := sort.SearchInts(offsets, candidate.original-lines[line].start) + 1
		edits = append(edits, columnEdit{line: line + 1, original: column, modified: column + delta})
		delta += 4
	}
	output.WriteString(input[start:])
	return output.String(), edits, nil
}

func originalColumn(column int, edits []columnEdit) int {
	index := sort.Search(len(edits), func(index int) bool { return edits[index].modified+6 > column })
	if index < len(edits) && column >= edits[index].modified {
		return edits[index].original + min(column-edits[index].modified, 1)
	}
	return column - index*4
}

func originalResponse(result Response, err error, edits []columnEdit) string {
	lines := map[int][]columnEdit{}
	for _, edit := range edits {
		lines[edit.line] = append(lines[edit.line], edit)
	}
	for index := range result.Tokens {
		token := &result.Tokens[index]
		token.Column = originalColumn(token.Column, lines[token.Line])
	}
	if problem, ok := err.(*failure); ok {
		copy := *problem
		copy.column = originalColumn(copy.column, lines[copy.line])
		err = &copy
	}
	return response(result, err)
}
