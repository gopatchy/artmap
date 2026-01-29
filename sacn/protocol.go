package sacn

import (
	"encoding/binary"
	"net"
)

const (
	Port = 5568

	ACNPacketIdentifier = 0x41534300

	VectorRootE131Data      = 0x00000004
	VectorRootE131Extended  = 0x00000008
	VectorE131DataPacket    = 0x00000002
	VectorE131Discovery     = 0x00000002
	VectorDMPSetProperty    = 0x02
	VectorUniverseDiscovery = 0x00000001
)

var (
	// ACN packet identifier (12 bytes)
	packetIdentifier = [12]byte{
		0x41, 0x53, 0x43, 0x2d, 0x45, 0x31, 0x2e, 0x31, 0x37, 0x00, 0x00, 0x00,
	}
)

// BuildDataPacket creates an E1.31 (sACN) data packet
func BuildDataPacket(universe uint16, sequence uint8, sourceName string, cid [16]byte, data []byte) []byte {
	dataLen := len(data)
	if dataLen > 512 {
		dataLen = 512
	}

	// Total packet size: Root Layer (38) + Framing Layer (77) + DMP Layer (11 + data)
	// = 126 + dataLen
	pktLen := 126 + dataLen
	buf := make([]byte, pktLen)

	// Root Layer (38 bytes)
	// Preamble Size (2 bytes)
	binary.BigEndian.PutUint16(buf[0:2], 0x0010)
	// Post-amble Size (2 bytes)
	binary.BigEndian.PutUint16(buf[2:4], 0x0000)
	// ACN Packet Identifier (12 bytes)
	copy(buf[4:16], packetIdentifier[:])
	// Flags and Length (2 bytes) - high 4 bits are flags (0x7), low 12 bits are length
	rootLen := pktLen - 16 // Length from after ACN Packet Identifier
	binary.BigEndian.PutUint16(buf[16:18], 0x7000|uint16(rootLen))
	// Vector (4 bytes)
	binary.BigEndian.PutUint32(buf[18:22], VectorRootE131Data)
	// CID (16 bytes)
	copy(buf[22:38], cid[:])

	// Framing Layer (77 bytes, starting at offset 38)
	// Flags and Length (2 bytes)
	framingLen := pktLen - 38
	binary.BigEndian.PutUint16(buf[38:40], 0x7000|uint16(framingLen))
	// Vector (4 bytes)
	binary.BigEndian.PutUint32(buf[40:44], VectorE131DataPacket)
	// Source Name (64 bytes, null-terminated)
	copy(buf[44:108], sourceName)
	// Priority (1 byte)
	buf[108] = 100
	// Synchronization Address (2 bytes)
	binary.BigEndian.PutUint16(buf[109:111], 0)
	// Sequence Number (1 byte)
	buf[111] = sequence
	// Options (1 byte)
	buf[112] = 0
	// Universe (2 bytes)
	binary.BigEndian.PutUint16(buf[113:115], universe)

	// DMP Layer (11 + dataLen bytes, starting at offset 115)
	// Flags and Length (2 bytes)
	dmpLen := 11 + dataLen
	binary.BigEndian.PutUint16(buf[115:117], 0x7000|uint16(dmpLen))
	// Vector (1 byte)
	buf[117] = VectorDMPSetProperty
	// Address Type & Data Type (1 byte)
	buf[118] = 0xa1
	// First Property Address (2 bytes)
	binary.BigEndian.PutUint16(buf[119:121], 0)
	// Address Increment (2 bytes)
	binary.BigEndian.PutUint16(buf[121:123], 1)
	// Property Value Count (2 bytes) - includes START code
	binary.BigEndian.PutUint16(buf[123:125], uint16(dataLen+1))
	// START Code (1 byte)
	buf[125] = 0
	// Property Values (DMX data)
	copy(buf[126:], data[:dataLen])

	return buf
}

func MulticastAddr(universe uint16) *net.UDPAddr {
	return &net.UDPAddr{
		IP:   net.IPv4(239, 255, byte(universe>>8), byte(universe&0xff)),
		Port: Port,
	}
}

var DiscoveryAddr = &net.UDPAddr{
	IP:   net.IPv4(239, 255, 250, 214),
	Port: Port,
}

func BuildDiscoveryPacket(sourceName string, cid [16]byte, page, lastPage uint8, universes []uint16) []byte {
	universeCount := len(universes)
	if universeCount > 512 {
		universeCount = 512
	}

	pktLen := 120 + universeCount*2
	buf := make([]byte, pktLen)

	binary.BigEndian.PutUint16(buf[0:2], 0x0010)
	binary.BigEndian.PutUint16(buf[2:4], 0x0000)
	copy(buf[4:16], packetIdentifier[:])
	rootLen := pktLen - 16
	binary.BigEndian.PutUint16(buf[16:18], 0x7000|uint16(rootLen))
	binary.BigEndian.PutUint32(buf[18:22], VectorRootE131Extended)
	copy(buf[22:38], cid[:])

	framingLen := pktLen - 38
	binary.BigEndian.PutUint16(buf[38:40], 0x7000|uint16(framingLen))
	binary.BigEndian.PutUint32(buf[40:44], VectorE131Discovery)
	copy(buf[44:108], sourceName)
	binary.BigEndian.PutUint32(buf[108:112], 0)

	discoveryLen := pktLen - 112
	binary.BigEndian.PutUint16(buf[112:114], 0x7000|uint16(discoveryLen))
	binary.BigEndian.PutUint32(buf[114:118], VectorUniverseDiscovery)
	buf[118] = page
	buf[119] = lastPage
	for i := 0; i < universeCount; i++ {
		binary.BigEndian.PutUint16(buf[120+i*2:122+i*2], universes[i])
	}

	return buf
}
