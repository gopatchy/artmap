package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gopatchy/artmap/artnet"
)

// Config represents the application configuration
type Config struct {
	Mappings []Mapping `toml:"mapping"`
}

// Mapping represents a single channel mapping rule
type Mapping struct {
	From FromAddr `toml:"from"`
	To   ToAddr   `toml:"to"`
}

// FromAddr represents a source universe address with channel range
type FromAddr struct {
	Universe     artnet.Universe
	ChannelStart int // 1-indexed
	ChannelEnd   int // 1-indexed
}

func (a *FromAddr) UnmarshalTOML(data interface{}) error {
	switch v := data.(type) {
	case string:
		return a.parse(v)
	case int64:
		a.Universe = artnet.Universe(v)
		a.ChannelStart = 1
		a.ChannelEnd = 512
		return nil
	case float64:
		a.Universe = artnet.Universe(int64(v))
		a.ChannelStart = 1
		a.ChannelEnd = 512
		return nil
	default:
		return fmt.Errorf("unsupported address type: %T", data)
	}
}

// parse parses address formats:
// - "0.0.1" - all channels
// - "0.0.1:50" - single channel
// - "0.0.1:50-" - channel 50 through end
// - "0.0.1:50-100" - channel range
func (a *FromAddr) parse(s string) error {
	s = strings.TrimSpace(s)

	universeStr, channelSpec := splitAddr(s)

	universe, err := parseUniverse(universeStr)
	if err != nil {
		return err
	}
	a.Universe = universe

	if channelSpec == "" {
		a.ChannelStart = 1
		a.ChannelEnd = 512
		return nil
	}

	if idx := strings.Index(channelSpec, "-"); idx != -1 {
		startStr := channelSpec[:idx]
		endStr := channelSpec[idx+1:]

		start, err := strconv.Atoi(startStr)
		if err != nil {
			return fmt.Errorf("invalid channel start: %w", err)
		}
		a.ChannelStart = start

		if endStr == "" {
			a.ChannelEnd = 512
		} else {
			end, err := strconv.Atoi(endStr)
			if err != nil {
				return fmt.Errorf("invalid channel end: %w", err)
			}
			a.ChannelEnd = end
		}
	} else {
		ch, err := strconv.Atoi(channelSpec)
		if err != nil {
			return fmt.Errorf("invalid channel: %w", err)
		}
		a.ChannelStart = ch
		a.ChannelEnd = ch
	}

	return nil
}

func (a *FromAddr) Count() int {
	return a.ChannelEnd - a.ChannelStart + 1
}

// ToAddr represents a destination universe address with starting channel
type ToAddr struct {
	Universe     artnet.Universe
	ChannelStart int // 1-indexed
}

func (a *ToAddr) UnmarshalTOML(data interface{}) error {
	switch v := data.(type) {
	case string:
		return a.parse(v)
	case int64:
		a.Universe = artnet.Universe(v)
		a.ChannelStart = 1
		return nil
	case float64:
		a.Universe = artnet.Universe(int64(v))
		a.ChannelStart = 1
		return nil
	default:
		return fmt.Errorf("unsupported address type: %T", data)
	}
}

// parse parses address formats:
// - "0.0.1" - starting at channel 1
// - "0.0.1:50" - starting at channel 50
func (a *ToAddr) parse(s string) error {
	s = strings.TrimSpace(s)

	universeStr, channelSpec := splitAddr(s)

	universe, err := parseUniverse(universeStr)
	if err != nil {
		return err
	}
	a.Universe = universe

	if channelSpec == "" {
		a.ChannelStart = 1
		return nil
	}

	if strings.Contains(channelSpec, "-") {
		return fmt.Errorf("to address cannot contain range; use single channel number")
	}

	ch, err := strconv.Atoi(channelSpec)
	if err != nil {
		return fmt.Errorf("invalid channel: %w", err)
	}
	a.ChannelStart = ch

	return nil
}

func splitAddr(s string) (universe, channel string) {
	if idx := strings.LastIndex(s, ":"); idx != -1 {
		return s[:idx], s[idx+1:]
	}
	return s, ""
}

func parseUniverse(s string) (artnet.Universe, error) {
	if strings.Contains(s, ".") {
		parts := strings.Split(s, ".")
		if len(parts) != 3 {
			return 0, fmt.Errorf("invalid universe format: %s (expected net.subnet.universe)", s)
		}
		net, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, fmt.Errorf("invalid net: %w", err)
		}
		subnet, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, fmt.Errorf("invalid subnet: %w", err)
		}
		universe, err := strconv.Atoi(parts[2])
		if err != nil {
			return 0, fmt.Errorf("invalid universe: %w", err)
		}
		return artnet.NewUniverse(uint8(net), uint8(subnet), uint8(universe)), nil
	}

	u, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid universe: %s", s)
	}
	return artnet.Universe(u), nil
}

// Load loads configuration from a TOML file
func Load(path string) (*Config, error) {
	var cfg Config

	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	for i, m := range cfg.Mappings {
		if m.From.ChannelStart < 1 || m.From.ChannelStart > 512 {
			return nil, fmt.Errorf("mapping %d: from channel start must be 1-512", i)
		}
		if m.From.ChannelEnd < 1 || m.From.ChannelEnd > 512 {
			return nil, fmt.Errorf("mapping %d: from channel end must be 1-512", i)
		}
		if m.From.ChannelStart > m.From.ChannelEnd {
			return nil, fmt.Errorf("mapping %d: from channel start > end", i)
		}
		if m.To.ChannelStart < 1 || m.To.ChannelStart > 512 {
			return nil, fmt.Errorf("mapping %d: to channel must be 1-512", i)
		}
		toEnd := m.To.ChannelStart + m.From.Count() - 1
		if toEnd > 512 {
			return nil, fmt.Errorf("mapping %d: to channels exceed 512", i)
		}
	}

	return &cfg, nil
}

// NormalizedMapping is a processed mapping ready for the remapper
type NormalizedMapping struct {
	FromUniverse artnet.Universe
	FromChannel  int // 0-indexed
	ToUniverse   artnet.Universe
	ToChannel    int // 0-indexed
	Count        int
}

// Normalize converts config mappings to normalized form (0-indexed channels)
func (c *Config) Normalize() []NormalizedMapping {
	result := make([]NormalizedMapping, len(c.Mappings))
	for i, m := range c.Mappings {
		result[i] = NormalizedMapping{
			FromUniverse: m.From.Universe,
			FromChannel:  m.From.ChannelStart - 1,
			ToUniverse:   m.To.Universe,
			ToChannel:    m.To.ChannelStart - 1,
			Count:        m.From.Count(),
		}
	}
	return result
}
