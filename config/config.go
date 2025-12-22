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
	// Source
	From        UniverseAddr `toml:"from"`
	FromChannel int          `toml:"from_channel"` // 1-512, 0 means all channels

	// Destination
	To        UniverseAddr `toml:"to"`
	ToChannel int          `toml:"to_channel"` // 1-512, 0 means same as from_channel

	// Range
	Count int `toml:"count"` // Number of channels, 0 means all remaining
}

// UniverseAddr handles multiple universe address formats
type UniverseAddr struct {
	Universe artnet.Universe
}

func (u *UniverseAddr) UnmarshalText(text []byte) error {
	s := string(text)
	universe, err := ParseUniverseAddr(s)
	if err != nil {
		return err
	}
	u.Universe = universe
	return nil
}

func (u *UniverseAddr) UnmarshalTOML(data interface{}) error {
	switch v := data.(type) {
	case string:
		universe, err := ParseUniverseAddr(v)
		if err != nil {
			return err
		}
		u.Universe = universe
		return nil
	case int64:
		// Universe number only (0-32767)
		u.Universe = artnet.Universe(v)
		return nil
	case float64:
		// TOML sometimes parses integers as floats
		u.Universe = artnet.Universe(int64(v))
		return nil
	default:
		return fmt.Errorf("unsupported universe address type: %T", data)
	}
}

// ParseUniverseAddr parses universe address formats:
// - "0.0.1" - Net.Subnet.Universe
// - "1" - Universe number only
func ParseUniverseAddr(s string) (artnet.Universe, error) {
	s = strings.TrimSpace(s)

	// Try Net.Subnet.Universe format
	if strings.Contains(s, ".") {
		parts := strings.Split(s, ".")
		if len(parts) != 3 {
			return 0, fmt.Errorf("invalid universe address format: %s (expected net.subnet.universe)", s)
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

	// Plain universe number
	u, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid universe address format: %s", s)
	}
	return artnet.Universe(u), nil
}

// Load loads configuration from a TOML file
func Load(path string) (*Config, error) {
	var cfg Config

	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Validate and normalize mappings
	for i := range cfg.Mappings {
		m := &cfg.Mappings[i]

		// Default from_channel to 1 (start of universe)
		if m.FromChannel == 0 {
			m.FromChannel = 1
		}

		// Default to_channel to same as from_channel
		if m.ToChannel == 0 {
			m.ToChannel = m.FromChannel
		}

		// Default count to all remaining channels
		if m.Count == 0 {
			m.Count = 512 - m.FromChannel + 1
		}

		// Validate ranges
		if m.FromChannel < 1 || m.FromChannel > 512 {
			return nil, fmt.Errorf("mapping %d: from_channel must be 1-512", i)
		}
		if m.ToChannel < 1 || m.ToChannel > 512 {
			return nil, fmt.Errorf("mapping %d: to_channel must be 1-512", i)
		}
		if m.FromChannel+m.Count-1 > 512 {
			return nil, fmt.Errorf("mapping %d: from_channel + count exceeds 512", i)
		}
		if m.ToChannel+m.Count-1 > 512 {
			return nil, fmt.Errorf("mapping %d: to_channel + count exceeds 512", i)
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
			FromChannel:  m.FromChannel - 1, // Convert to 0-indexed
			ToUniverse:   m.To.Universe,
			ToChannel:    m.ToChannel - 1, // Convert to 0-indexed
			Count:        m.Count,
		}
	}
	return result
}
