package remap

import (
	"sync"

	"github.com/gopatchy/artmap/artnet"
	"github.com/gopatchy/artmap/config"
)

// Output represents a remapped DMX output
type Output struct {
	Universe artnet.Universe
	Protocol config.Protocol
	Data     [512]byte
}

// outputKey uniquely identifies an output destination
type outputKey struct {
	Universe artnet.Universe
	Protocol config.Protocol
}

// sourceKey uniquely identifies an input source
type sourceKey struct {
	Universe artnet.Universe
	Protocol config.Protocol
}

// Engine handles DMX channel remapping
type Engine struct {
	mappings []config.NormalizedMapping
	// Index mappings by source universe and protocol for faster lookup
	bySource map[sourceKey][]config.NormalizedMapping
	// Persistent state for each output universe (merged from all sources)
	state   map[outputKey]*[512]byte
	stateMu sync.Mutex
}

// NewEngine creates a new remapping engine
func NewEngine(mappings []config.NormalizedMapping) *Engine {
	bySource := make(map[sourceKey][]config.NormalizedMapping)
	for _, m := range mappings {
		key := sourceKey{Universe: m.FromUniverse, Protocol: m.FromProto}
		bySource[key] = append(bySource[key], m)
	}

	// Initialize state for all output universes
	state := make(map[outputKey]*[512]byte)
	for _, m := range mappings {
		key := outputKey{Universe: m.ToUniverse, Protocol: m.Protocol}
		if _, ok := state[key]; !ok {
			state[key] = &[512]byte{}
		}
	}

	return &Engine{
		mappings: mappings,
		bySource: bySource,
		state:    state,
	}
}

// Remap applies mappings to incoming DMX data and returns outputs
func (e *Engine) Remap(srcProto config.Protocol, srcUniverse artnet.Universe, srcData [512]byte) []Output {
	key := sourceKey{Universe: srcUniverse, Protocol: srcProto}
	mappings, ok := e.bySource[key]
	if !ok {
		return nil
	}

	e.stateMu.Lock()
	defer e.stateMu.Unlock()

	// Track which outputs are affected by this input
	affected := make(map[outputKey]bool)

	for _, m := range mappings {
		outKey := outputKey{Universe: m.ToUniverse, Protocol: m.Protocol}
		affected[outKey] = true

		// Update state for this output
		outState := e.state[outKey]

		// Copy channels into persistent state
		for i := 0; i < m.Count; i++ {
			srcChan := m.FromChannel + i
			dstChan := m.ToChannel + i
			if srcChan < 512 && dstChan < 512 {
				outState[dstChan] = srcData[srcChan]
			}
		}
	}

	// Return outputs for all affected universes
	result := make([]Output, 0, len(affected))
	for outKey := range affected {
		result = append(result, Output{
			Universe: outKey.Universe,
			Protocol: outKey.Protocol,
			Data:     *e.state[outKey],
		})
	}

	return result
}

// SourceUniverses returns all universes that have mappings
func (e *Engine) SourceUniverses() []artnet.Universe {
	seen := make(map[artnet.Universe]bool)
	for key := range e.bySource {
		seen[key.Universe] = true
	}
	result := make([]artnet.Universe, 0, len(seen))
	for u := range seen {
		result = append(result, u)
	}
	return result
}

// DestUniverses returns all destination universes (for ArtNet discovery)
func (e *Engine) DestUniverses() []artnet.Universe {
	seen := make(map[artnet.Universe]bool)
	for _, m := range e.mappings {
		if m.Protocol == config.ProtocolArtNet {
			seen[m.ToUniverse] = true
		}
	}

	result := make([]artnet.Universe, 0, len(seen))
	for u := range seen {
		result = append(result, u)
	}
	return result
}
