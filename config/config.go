package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Protocol specifies the output protocol
type Protocol string

const (
	ProtocolArtNet Protocol = "artnet"
	ProtocolSACN   Protocol = "sacn"
)

// Universe represents a DMX universe with its protocol
type Universe struct {
	Protocol Protocol `json:"protocol"`
	Number   uint16   `json:"number"`
}

func NewUniverse(proto Protocol, num any) (Universe, error) {
	n, err := toUint16(num, proto)
	if err != nil {
		return Universe{}, err
	}
	return makeUniverse(proto, n)
}

func ParseUniverse(s string) (Universe, error) {
	proto, rest, err := splitProtoPrefix(s)
	if err != nil {
		return Universe{}, err
	}
	return NewUniverse(proto, rest)
}

func (u Universe) String() string {
	if u.Protocol == ProtocolSACN {
		return fmt.Sprintf("sacn:%d", u.Number)
	}
	net := (u.Number >> 8) & 0x7F
	subnet := (u.Number >> 4) & 0x0F
	universe := u.Number & 0x0F
	return fmt.Sprintf("artnet:%d.%d.%d", net, subnet, universe)
}

func (u *Universe) UnmarshalTOML(data any) error {
	switch v := data.(type) {
	case string:
		parsed, err := ParseUniverse(v)
		if err != nil {
			return err
		}
		*u = parsed
		return nil
	default:
		parsed, err := NewUniverse(ProtocolArtNet, data)
		if err != nil {
			return err
		}
		*u = parsed
		return nil
	}
}

func toUint16(v any, proto Protocol) (uint16, error) {
	switch n := v.(type) {
	case int:
		return uint16(n), nil
	case int64:
		return uint16(n), nil
	case uint16:
		return n, nil
	case uint:
		return uint16(n), nil
	case float64:
		return uint16(n), nil
	case string:
		return parseUniverseNumber(n, proto)
	default:
		return 0, fmt.Errorf("unsupported universe type: %T", v)
	}
}

func makeUniverse(proto Protocol, n uint16) (Universe, error) {
	switch proto {
	case ProtocolArtNet:
		if n > 0x7FFF {
			return Universe{}, fmt.Errorf("artnet universe %d out of range (max 32767)", n)
		}
	case ProtocolSACN:
		if n < 1 || n > 63999 {
			return Universe{}, fmt.Errorf("sacn universe %d out of range (1-63999)", n)
		}
	default:
		return Universe{}, fmt.Errorf("unknown protocol: %s", proto)
	}
	return Universe{Protocol: proto, Number: n}, nil
}

// Config represents the application configuration
type Config struct {
	Targets  []Target  `toml:"target" json:"targets"`
	Mappings []Mapping `toml:"mapping" json:"mappings"`
}

// Target represents a target address for an output universe
type Target struct {
	Universe Universe `toml:"universe" json:"universe"`
	Address  string   `toml:"address" json:"address"`
}

// Mapping represents a single channel mapping rule
type Mapping struct {
	From FromAddr `toml:"from" json:"from"`
	To   ToAddr   `toml:"to" json:"to"`
}

// FromAddr represents a source universe address with channel range
type FromAddr struct {
	Universe     Universe `json:"universe"`
	ChannelStart int      `json:"channel_start"` // 1-indexed
	ChannelEnd   int      `json:"channel_end"`   // 1-indexed
}

func (a *FromAddr) UnmarshalTOML(data any) error {
	if s, ok := data.(string); ok {
		return a.parse(s)
	}
	u, err := NewUniverse(ProtocolArtNet, data)
	if err != nil {
		return err
	}
	a.Universe = u
	a.ChannelStart = 1
	a.ChannelEnd = 512
	return nil
}

func (a *FromAddr) parse(s string) error {
	proto, rest, err := splitProtoPrefix(strings.TrimSpace(s))
	if err != nil {
		return err
	}

	universeStr, channelSpec := splitAddr(rest)
	u, err := NewUniverse(proto, universeStr)
	if err != nil {
		return err
	}
	a.Universe = u

	if channelSpec == "" {
		a.ChannelStart = 1
		a.ChannelEnd = 512
		return nil
	}

	return parseChannelRange(channelSpec, &a.ChannelStart, &a.ChannelEnd)
}

func parseChannelRange(spec string, start, end *int) error {
	if idx := strings.Index(spec, "-"); idx != -1 {
		s, err := strconv.Atoi(spec[:idx])
		if err != nil {
			return fmt.Errorf("invalid channel start: %w", err)
		}
		*start = s

		if spec[idx+1:] == "" {
			*end = 512
		} else {
			e, err := strconv.Atoi(spec[idx+1:])
			if err != nil {
				return fmt.Errorf("invalid channel end: %w", err)
			}
			*end = e
		}
	} else {
		ch, err := strconv.Atoi(spec)
		if err != nil {
			return fmt.Errorf("invalid channel: %w", err)
		}
		*start = ch
		*end = ch
	}
	if *start > *end {
		return fmt.Errorf("channel start %d > end %d", *start, *end)
	}
	return nil
}

func (a FromAddr) String() string {
	if a.ChannelStart == 1 && a.ChannelEnd == 512 {
		return a.Universe.String()
	}
	if a.ChannelStart == a.ChannelEnd {
		return fmt.Sprintf("%s:%d", a.Universe, a.ChannelStart)
	}
	return fmt.Sprintf("%s:%d-%d", a.Universe, a.ChannelStart, a.ChannelEnd)
}

func (a *FromAddr) Count() int {
	return a.ChannelEnd - a.ChannelStart + 1
}

// ToAddr represents a destination universe address with starting channel
type ToAddr struct {
	Universe     Universe `json:"universe"`
	ChannelStart int      `json:"channel_start"` // 1-indexed
}

func (a *ToAddr) UnmarshalTOML(data any) error {
	if s, ok := data.(string); ok {
		return a.parse(s)
	}
	u, err := NewUniverse(ProtocolArtNet, data)
	if err != nil {
		return err
	}
	a.Universe = u
	a.ChannelStart = 1
	return nil
}

func (a *ToAddr) parse(s string) error {
	proto, rest, err := splitProtoPrefix(strings.TrimSpace(s))
	if err != nil {
		return err
	}

	universeStr, channelSpec := splitAddr(rest)
	u, err := NewUniverse(proto, universeStr)
	if err != nil {
		return err
	}
	a.Universe = u

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

func (a ToAddr) String() string {
	if a.ChannelStart == 1 {
		return a.Universe.String()
	}
	return fmt.Sprintf("%s:%d", a.Universe, a.ChannelStart)
}

func splitProtoPrefix(s string) (Protocol, string, error) {
	if strings.HasPrefix(s, "artnet:") {
		return ProtocolArtNet, s[7:], nil
	}
	if strings.HasPrefix(s, "sacn:") {
		return ProtocolSACN, s[5:], nil
	}
	return "", "", fmt.Errorf("address %q must start with 'artnet:' or 'sacn:' prefix", s)
}

func splitAddr(s string) (universe, channel string) {
	if idx := strings.LastIndex(s, ":"); idx != -1 {
		return s[:idx], s[idx+1:]
	}
	return s, ""
}

func parseUniverseNumber(s string, proto Protocol) (uint16, error) {
	if strings.Contains(s, ".") {
		if proto == ProtocolSACN {
			return 0, fmt.Errorf("sACN universes cannot use net.subnet.universe format")
		}
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
		return uint16(net&0x7F)<<8 | uint16(subnet&0x0F)<<4 | uint16(universe&0x0F), nil
	}

	u, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid universe: %s", s)
	}
	return uint16(u), nil
}

// Load loads configuration from a TOML file
func Load(path string) (*Config, error) {
	var cfg Config

	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Validate targets
	for i, t := range cfg.Targets {
		if t.Address == "" {
			return nil, fmt.Errorf("target %d: address is required", i)
		}
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
	From       Universe
	FromChan   int // 0-indexed
	To         Universe
	ToChan     int // 0-indexed
	Count      int
}

// Normalize converts config mappings to normalized form (0-indexed channels)
func (c *Config) Normalize() []NormalizedMapping {
	result := make([]NormalizedMapping, len(c.Mappings))
	for i, m := range c.Mappings {
		result[i] = NormalizedMapping{
			From:     m.From.Universe,
			FromChan: m.From.ChannelStart - 1,
			To:       m.To.Universe,
			ToChan:   m.To.ChannelStart - 1,
			Count:    m.From.Count(),
		}
	}
	return result
}

// SACNSourceUniverses returns sACN universe numbers that need input
func (c *Config) SACNSourceUniverses() []uint16 {
	seen := make(map[uint16]bool)
	for _, m := range c.Mappings {
		if m.From.Universe.Protocol == ProtocolSACN {
			seen[m.From.Universe.Number] = true
		}
	}
	result := make([]uint16, 0, len(seen))
	for u := range seen {
		result = append(result, u)
	}
	return result
}
