package sacn

import (
	"bytes"
	"testing"
)

func FuzzParsePacket(f *testing.F) {
	cid := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	validPacket := BuildDataPacket(1, 0, "test", cid, make([]byte, 512))
	f.Add(validPacket)
	f.Add(BuildDataPacket(1, 0, "test", cid, make([]byte, 100)))
	f.Add(BuildDataPacket(63999, 255, "long source name here", cid, make([]byte, 512)))
	f.Add([]byte{})
	f.Add(make([]byte, 125))
	f.Add(make([]byte, 126))
	f.Add(make([]byte, 638))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, dmxData, ok := ParsePacket(data)
		if !ok {
			return
		}
		if len(dmxData) != 512 {
			t.Fatalf("dmx data should be 512 bytes, got %d", len(dmxData))
		}
	})
}

func FuzzBuildParseRoundtrip(f *testing.F) {
	f.Add(uint16(1), uint8(0), "test", make([]byte, 512))
	f.Add(uint16(63999), uint8(255), "source", make([]byte, 100))
	f.Add(uint16(100), uint8(128), "", make([]byte, 0))
	f.Add(uint16(1), uint8(0), "a]very long source name that exceeds normal limits", make([]byte, 512))

	f.Fuzz(func(t *testing.T, universe uint16, seq uint8, sourceName string, dmxInput []byte) {
		if universe < 1 || universe > 63999 {
			return
		}
		cid := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		packet := BuildDataPacket(universe, seq, sourceName, cid, dmxInput)
		parsedUniverse, parsedData, ok := ParsePacket(packet)
		if !ok {
			t.Fatalf("failed to parse packet we just built")
		}
		if parsedUniverse != universe {
			t.Fatalf("universe mismatch: sent %d, got %d", universe, parsedUniverse)
		}
		expectedLen := len(dmxInput)
		if expectedLen > 512 {
			expectedLen = 512
		}
		if !bytes.Equal(parsedData[:expectedLen], dmxInput[:expectedLen]) {
			t.Fatalf("dmx data mismatch")
		}
	})
}

func ParsePacket(data []byte) (universe uint16, dmxData [512]byte, ok bool) {
	if len(data) < 126 {
		return 0, dmxData, false
	}
	if data[4] != 0x41 || data[5] != 0x53 || data[6] != 0x43 {
		return 0, dmxData, false
	}
	rootVector := uint32(data[18])<<24 | uint32(data[19])<<16 | uint32(data[20])<<8 | uint32(data[21])
	if rootVector != VectorRootE131Data {
		return 0, dmxData, false
	}
	framingVector := uint32(data[40])<<24 | uint32(data[41])<<16 | uint32(data[42])<<8 | uint32(data[43])
	if framingVector != VectorE131DataPacket {
		return 0, dmxData, false
	}
	universe = uint16(data[113])<<8 | uint16(data[114])
	if data[117] != VectorDMPSetProperty {
		return 0, dmxData, false
	}
	propCount := uint16(data[123])<<8 | uint16(data[124])
	if propCount < 1 {
		return 0, dmxData, false
	}
	dmxLen := int(propCount) - 1
	if dmxLen > 512 {
		dmxLen = 512
	}
	if len(data) < 126+dmxLen {
		return 0, dmxData, false
	}
	copy(dmxData[:], data[126:126+dmxLen])
	return universe, dmxData, true
}
