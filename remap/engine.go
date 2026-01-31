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

// universeBuffer holds per-output-universe state with its own lock
type universeBuffer struct {
	mu    sync.Mutex
	data  [512]byte
	dirty bool
}

// Engine handles DMX channel remapping
type Engine struct {
	mappings []config.NormalizedMapping
	bySource map[config.Universe]*sourceEntry
	outputs  map[config.Universe]*universeBuffer
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

	outputs := map[config.Universe]*universeBuffer{}
	for _, m := range mappings {
		if _, ok := outputs[m.To]; !ok {
			outputs[m.To] = &universeBuffer{}
		}
	}

	return &Engine{
		mappings: mappings,
		bySource: bySource,
		outputs:  outputs,
	}
}

// Remap applies mappings to incoming DMX data and marks affected outputs dirty
func (e *Engine) Remap(src config.Universe, srcData [512]byte) {
	entry := e.bySource[src]
	if entry == nil {
		return
	}
	entry.counter.Add(1)

	for _, m := range entry.mappings {
		e.applyMapping(m, srcData)
	}
}

func (e *Engine) applyMapping(m config.NormalizedMapping, srcData [512]byte) {
	buf := e.outputs[m.To]
	buf.mu.Lock()
	defer buf.mu.Unlock()

	for i := 0; i < m.Count; i++ {
		srcChan := m.FromChan + i
		dstChan := m.ToChan + i
		if srcChan < 512 && dstChan < 512 {
			buf.data[dstChan] = srcData[srcChan]
		}
	}
	buf.dirty = true
}

// GetDirtyOutputs returns outputs that have been modified since last call
func (e *Engine) GetDirtyOutputs() []Output {
	var result []Output
	for u, buf := range e.outputs {
		if out, ok := e.getDirtyOutput(u, buf); ok {
			result = append(result, out)
		}
	}
	return result
}

func (e *Engine) getDirtyOutput(u config.Universe, buf *universeBuffer) (Output, bool) {
	buf.mu.Lock()
	defer buf.mu.Unlock()

	if !buf.dirty {
		return Output{}, false
	}
	buf.dirty = false
	return Output{Universe: u, Data: buf.data}, true
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
