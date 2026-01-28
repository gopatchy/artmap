package config

import (
	"testing"
)

func FuzzParseUniverse(f *testing.F) {
	f.Add("artnet:0.0.0")
	f.Add("artnet:0.0.1")
	f.Add("artnet:127.15.15")
	f.Add("artnet:0")
	f.Add("artnet:32767")
	f.Add("sacn:1")
	f.Add("sacn:63999")
	f.Add("sacn:100")
	f.Add("")
	f.Add("invalid")
	f.Add("artnet:")
	f.Add("sacn:")
	f.Add("artnet:a.b.c")
	f.Add("artnet:-1")
	f.Add("sacn:0")
	f.Add("sacn:64000")

	f.Fuzz(func(t *testing.T, input string) {
		u, err := ParseUniverse(input)
		if err != nil {
			return
		}
		s := u.String()
		u2, err := ParseUniverse(s)
		if err != nil {
			t.Fatalf("roundtrip failed: parsed %q -> %v -> %q, but re-parse failed: %v", input, u, s, err)
		}
		if u != u2 {
			t.Fatalf("roundtrip mismatch: %v != %v", u, u2)
		}
	})
}

func FuzzFromAddrParse(f *testing.F) {
	f.Add("artnet:0.0.0")
	f.Add("artnet:0.0.1:1-512")
	f.Add("artnet:0.0.1:50-100")
	f.Add("artnet:0.0.1:1")
	f.Add("artnet:0.0.1:512")
	f.Add("artnet:0.0.1:1-")
	f.Add("sacn:1:100-200")
	f.Add("sacn:100")
	f.Add("")
	f.Add("artnet:0.0.0:0")
	f.Add("artnet:0.0.0:513")
	f.Add("artnet:0.0.0:-1")
	f.Add("artnet:0.0.0:abc")

	f.Fuzz(func(t *testing.T, input string) {
		var addr FromAddr
		err := addr.parse(input)
		if err != nil {
			return
		}
		if addr.ChannelStart > addr.ChannelEnd {
			t.Fatalf("ChannelStart > ChannelEnd: %d > %d", addr.ChannelStart, addr.ChannelEnd)
		}
		if addr.ChannelStart < 1 || addr.ChannelEnd > 512 {
			return
		}
		s := addr.String()
		var addr2 FromAddr
		if err := addr2.parse(s); err != nil {
			t.Fatalf("roundtrip failed: parsed %q -> %v -> %q, but re-parse failed: %v", input, addr, s, err)
		}
	})
}

func FuzzToAddrParse(f *testing.F) {
	f.Add("artnet:0.0.0")
	f.Add("artnet:0.0.1:1")
	f.Add("artnet:0.0.1:512")
	f.Add("sacn:1:100")
	f.Add("sacn:100")
	f.Add("")
	f.Add("artnet:0.0.0:0")
	f.Add("artnet:0.0.0:1-100")
	f.Add("artnet:0.0.0:513")

	f.Fuzz(func(t *testing.T, input string) {
		var addr ToAddr
		err := addr.parse(input)
		if err != nil {
			return
		}
		if addr.ChannelStart < 1 || addr.ChannelStart > 512 {
			return
		}
		s := addr.String()
		var addr2 ToAddr
		if err := addr2.parse(s); err != nil {
			t.Fatalf("roundtrip failed: parsed %q -> %v -> %q, but re-parse failed: %v", input, addr, s, err)
		}
	})
}

func FuzzParseUniverseNumber(f *testing.F) {
	f.Add("0.0.0", string(ProtocolArtNet))
	f.Add("127.15.15", string(ProtocolArtNet))
	f.Add("0", string(ProtocolArtNet))
	f.Add("32767", string(ProtocolArtNet))
	f.Add("1", string(ProtocolSACN))
	f.Add("63999", string(ProtocolSACN))
	f.Add("", string(ProtocolArtNet))
	f.Add("invalid", string(ProtocolArtNet))
	f.Add("0.0", string(ProtocolArtNet))
	f.Add("0.0.0.0", string(ProtocolArtNet))
	f.Add("-1", string(ProtocolArtNet))

	f.Fuzz(func(t *testing.T, input string, protoStr string) {
		proto := Protocol(protoStr)
		if proto != ProtocolArtNet && proto != ProtocolSACN {
			return
		}
		_, _ = parseUniverseNumber(input, proto)
	})
}

func FuzzParseChannelRange(f *testing.F) {
	f.Add("1-512")
	f.Add("1")
	f.Add("512")
	f.Add("1-")
	f.Add("100-200")
	f.Add("")
	f.Add("-")
	f.Add("abc")
	f.Add("1-abc")
	f.Add("-1")
	f.Add("0")
	f.Add("513")

	f.Fuzz(func(t *testing.T, input string) {
		var start, end int
		err := parseChannelRange(input, &start, &end)
		if err != nil {
			return
		}
		if start < 0 {
			t.Fatalf("start should not be negative: %d", start)
		}
		if end < 0 {
			t.Fatalf("end should not be negative: %d", end)
		}
	})
}
