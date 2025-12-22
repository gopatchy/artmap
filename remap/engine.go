package remap

import (
	"github.com/gopatchy/artmap/artnet"
	"github.com/gopatchy/artmap/config"
)

// Output represents a remapped DMX output
type Output struct {
	Universe artnet.Universe
	Data     [512]byte
}

// Engine handles DMX channel remapping
type Engine struct {
	mappings []config.NormalizedMapping
	// Index mappings by source universe for faster lookup
	bySource map[artnet.Universe][]config.NormalizedMapping
}

// NewEngine creates a new remapping engine
func NewEngine(mappings []config.NormalizedMapping) *Engine {
	bySource := make(map[artnet.Universe][]config.NormalizedMapping)
	for _, m := range mappings {
		bySource[m.FromUniverse] = append(bySource[m.FromUniverse], m)
	}

	return &Engine{
		mappings: mappings,
		bySource: bySource,
	}
}

// Remap applies mappings to incoming DMX data and returns outputs
func (e *Engine) Remap(srcUniverse artnet.Universe, srcData [512]byte) []Output {
	mappings, ok := e.bySource[srcUniverse]
	if !ok {
		return nil
	}

	// Group outputs by destination universe
	outputs := make(map[artnet.Universe]*Output)

	for _, m := range mappings {
		// Get or create output for this destination universe
		out, ok := outputs[m.ToUniverse]
		if !ok {
			out = &Output{
				Universe: m.ToUniverse,
			}
			outputs[m.ToUniverse] = out
		}

		// Copy channels
		for i := 0; i < m.Count; i++ {
			srcChan := m.FromChannel + i
			dstChan := m.ToChannel + i
			if srcChan < 512 && dstChan < 512 {
				out.Data[dstChan] = srcData[srcChan]
			}
		}
	}

	// Convert map to slice
	result := make([]Output, 0, len(outputs))
	for _, out := range outputs {
		result = append(result, *out)
	}

	return result
}

// SourceUniverses returns all universes that have mappings
func (e *Engine) SourceUniverses() []artnet.Universe {
	result := make([]artnet.Universe, 0, len(e.bySource))
	for u := range e.bySource {
		result = append(result, u)
	}
	return result
}

// DestUniverses returns all destination universes
func (e *Engine) DestUniverses() []artnet.Universe {
	seen := make(map[artnet.Universe]bool)
	for _, m := range e.mappings {
		seen[m.ToUniverse] = true
	}

	result := make([]artnet.Universe, 0, len(seen))
	for u := range seen {
		result = append(result, u)
	}
	return result
}
