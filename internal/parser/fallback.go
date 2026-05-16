package parser

const maxFallbackBytes = 8 * 1024

type FallbackParser struct{}

func (f *FallbackParser) Parse(source []byte, language string) ([]Chunk, error) {
	if len(source) > maxFallbackBytes {
		return nil, nil
	}
	return []Chunk{{
		NodeType:  "file",
		Language:  language,
		StartLine: 1,
		EndLine:   countLines(source),
		StartByte: 0,
		Text:      string(source),
	}}, nil
}

func countLines(b []byte) int {
	n := 1
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}
