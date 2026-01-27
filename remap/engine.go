package remap

import (
	"sync"

	"github.com/gopatchy/artmap/config"
)

// Output represents a remapped DMX output
type Output struct {
	Universe config.Universe
	Data     [512]byte
}

// Engine handles DMX channel remapping
type Engine struct {
	mappings []config.NormalizedMapping
	bySource map[config.Universe][]config.NormalizedMapping
	state    map[config.Universe]*[512]byte
	stateMu  sync.Mutex
}

// NewEngine creates a new remapping engine
func NewEngine(mappings []config.NormalizedMapping) *Engine {
	bySource := make(map[config.Universe][]config.NormalizedMapping)
	for _, m := range mappings {
		bySource[m.From] = append(bySource[m.From], m)
	}

	state := make(map[config.Universe]*[512]byte)
	for _, m := range mappings {
		if _, ok := state[m.To]; !ok {
			state[m.To] = &[512]byte{}
		}
	}

	return &Engine{
		mappings: mappings,
		bySource: bySource,
		state:    state,
	}
}

// Remap applies mappings to incoming DMX data and returns outputs
func (e *Engine) Remap(src config.Universe, srcData [512]byte) []Output {
	mappings, ok := e.bySource[src]
	if !ok {
		return nil
	}

	e.stateMu.Lock()
	defer e.stateMu.Unlock()

	affected := make(map[config.Universe]bool)

	for _, m := range mappings {
		affected[m.To] = true
		outState := e.state[m.To]

		for i := 0; i < m.Count; i++ {
			srcChan := m.FromChan + i
			dstChan := m.ToChan + i
			if srcChan < 512 && dstChan < 512 {
				outState[dstChan] = srcData[srcChan]
			}
		}
	}

	result := make([]Output, 0, len(affected))
	for u := range affected {
		result = append(result, Output{
			Universe: u,
			Data:     *e.state[u],
		})
	}

	return result
}

// DestArtNetUniverses returns destination ArtNet universe numbers (for discovery)
func (e *Engine) DestArtNetUniverses() []uint16 {
	seen := make(map[uint16]bool)
	for _, m := range e.mappings {
		if m.To.Protocol == config.ProtocolArtNet {
			seen[m.To.Number] = true
		}
	}
	result := make([]uint16, 0, len(seen))
	for u := range seen {
		result = append(result, u)
	}
	return result
}
