package remap

import (
	"sync"
	"sync/atomic"

	"github.com/gopatchy/artmap/config"
)

// Output represents a remapped DMX output
type Output struct {
	Universe config.Universe
	Data     [512]byte
}

// sourceEntry holds mappings and stats for a source universe
type sourceEntry struct {
	mappings []config.NormalizedMapping
	counter  atomic.Uint64
}

// Engine handles DMX channel remapping
type Engine struct {
	mappings []config.NormalizedMapping
	bySource map[config.Universe]*sourceEntry
	state    map[config.Universe]*[512]byte
	stateMu  sync.Mutex
}

// NewEngine creates a new remapping engine
func NewEngine(mappings []config.NormalizedMapping) *Engine {
	bySource := map[config.Universe]*sourceEntry{}
	for _, m := range mappings {
		entry := bySource[m.From]
		if entry == nil {
			entry = &sourceEntry{}
			bySource[m.From] = entry
		}
		entry.mappings = append(entry.mappings, m)
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
	entry := e.bySource[src]
	if entry == nil {
		return nil
	}
	entry.counter.Add(1)

	e.stateMu.Lock()
	defer e.stateMu.Unlock()

	affected := make(map[config.Universe]bool)

	for _, m := range entry.mappings {
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

// SwapStats returns packet counts per source universe since last call and resets them
func (e *Engine) SwapStats() map[config.Universe]uint64 {
	result := map[config.Universe]uint64{}
	for u, entry := range e.bySource {
		result[u] = entry.counter.Swap(0)
	}
	return result
}

// SourceArtNetUniverses returns source ArtNet universe numbers (for discovery)
func (e *Engine) SourceArtNetUniverses() []uint16 {
	seen := make(map[uint16]bool)
	for _, m := range e.mappings {
		if m.From.Protocol == config.ProtocolArtNet {
			seen[m.From.Number] = true
		}
	}
	result := make([]uint16, 0, len(seen))
	for u := range seen {
		result = append(result, u)
	}
	return result
}

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

func (e *Engine) DestSACNUniverses() []uint16 {
	seen := make(map[uint16]bool)
	for _, m := range e.mappings {
		if m.To.Protocol == config.ProtocolSACN {
			seen[m.To.Number] = true
		}
	}
	result := make([]uint16, 0, len(seen))
	for u := range seen {
		result = append(result, u)
	}
	return result
}
