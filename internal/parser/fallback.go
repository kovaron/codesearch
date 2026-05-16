package parser

type FallbackParser struct{}

func (f *FallbackParser) Parse(source []byte, language string) ([]Chunk, error) {
	return nil, nil
}
