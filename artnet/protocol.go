package artnet

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	Port = 6454

	// OpCodes
	OpPoll      = 0x2000
	OpPollReply = 0x2100
	OpDmx       = 0x5000

	// Protocol
	ProtocolVersion = 14
)

var (
	ArtNetID = [8]byte{'A', 'r', 't', '-', 'N', 'e', 't', 0x00}

	ErrInvalidPacket  = errors.New("invalid ArtNet packet")
	ErrInvalidHeader  = errors.New("invalid ArtNet header")
	ErrUnknownOpCode  = errors.New("unknown OpCode")
	ErrPacketTooShort = errors.New("packet too short")
)

// Universe represents an ArtNet universe address (15-bit)
// Bits 14-8: Net (0-127)
// Bits 7-4: SubNet (0-15)
// Bits 3-0: Universe (0-15)
type Universe uint16

func NewUniverse(net, subnet, universe uint8) Universe {
	return Universe((uint16(net&0x7F) << 8) | (uint16(subnet&0x0F) << 4) | uint16(universe&0x0F))
}

func (u Universe) Net() uint8 {
	return uint8((u >> 8) & 0x7F)
}

func (u Universe) SubNet() uint8 {
	return uint8((u >> 4) & 0x0F)
}

func (u Universe) Universe() uint8 {
	return uint8(u & 0x0F)
}

func (u Universe) String() string {
	return fmt.Sprintf("%d.%d.%d", u.Net(), u.SubNet(), u.Universe())
}

// Header is the common ArtNet packet header
type Header struct {
	ID            [8]byte
	OpCode        uint16
}

// DMXPacket represents an ArtDmx packet (OpCode 0x5000)
type DMXPacket struct {
	ProtocolVersion uint16    // High byte first
	Sequence        uint8     // 0x00 to disable, 0x01-0xFF sequence
	Physical        uint8     // Physical input port
	Universe        Universe  // Universe address (low byte first in wire format)
	Length          uint16    // Data length (high byte first), 2-512, even
	Data            [512]byte // DMX data
}

// PollPacket represents an ArtPoll packet (OpCode 0x2000)
type PollPacket struct {
	ProtocolVersion uint16
	Flags           uint8
	DiagPriority    uint8
}

// PollReplyPacket represents an ArtPollReply packet (OpCode 0x2100)
type PollReplyPacket struct {
	IPAddress     [4]byte
	Port          uint16
	VersionInfo   uint16
	NetSwitch     uint8
	SubSwitch     uint8
	OemHi         uint8
	Oem           uint8
	UbeaVersion   uint8
	Status1       uint8
	EstaMan       uint16
	ShortName     [18]byte
	LongName      [64]byte
	NodeReport    [64]byte
	NumPortsHi    uint8
	NumPortsLo    uint8
	PortTypes     [4]byte
	GoodInput     [4]byte
	GoodOutput    [4]byte
	SwIn          [4]byte
	SwOut         [4]byte
	SwVideo       uint8
	SwMacro       uint8
	SwRemote      uint8
	Spare         [3]byte
	Style         uint8
	MAC           [6]byte
	BindIP        [4]byte
	BindIndex     uint8
	Status2       uint8
	Filler        [26]byte
}

// ParsePacket parses a raw ArtNet packet and returns the OpCode and parsed data
func ParsePacket(data []byte) (uint16, interface{}, error) {
	if len(data) < 10 {
		return 0, nil, ErrPacketTooShort
	}

	// Check header
	if !bytes.Equal(data[:8], ArtNetID[:]) {
		return 0, nil, ErrInvalidHeader
	}

	opCode := binary.LittleEndian.Uint16(data[8:10])

	switch opCode {
	case OpDmx:
		pkt, err := parseDMXPacket(data)
		return opCode, pkt, err
	case OpPoll:
		pkt, err := parsePollPacket(data)
		return opCode, pkt, err
	case OpPollReply:
		pkt, err := parsePollReplyPacket(data)
		return opCode, pkt, err
	default:
		return opCode, nil, nil // Unknown but valid packet
	}
}

func parseDMXPacket(data []byte) (*DMXPacket, error) {
	if len(data) < 18 {
		return nil, ErrPacketTooShort
	}

	pkt := &DMXPacket{
		ProtocolVersion: binary.BigEndian.Uint16(data[10:12]),
		Sequence:        data[12],
		Physical:        data[13],
		Universe:        Universe(binary.LittleEndian.Uint16(data[14:16])),
		Length:          binary.BigEndian.Uint16(data[16:18]),
	}

	dataLen := int(pkt.Length)
	if dataLen > 512 {
		dataLen = 512
	}
	if len(data) >= 18+dataLen {
		copy(pkt.Data[:], data[18:18+dataLen])
	}

	return pkt, nil
}

func parsePollPacket(data []byte) (*PollPacket, error) {
	if len(data) < 14 {
		return nil, ErrPacketTooShort
	}

	return &PollPacket{
		ProtocolVersion: binary.BigEndian.Uint16(data[10:12]),
		Flags:           data[12],
		DiagPriority:    data[13],
	}, nil
}

func parsePollReplyPacket(data []byte) (*PollReplyPacket, error) {
	if len(data) < 207 {
		return nil, ErrPacketTooShort
	}

	pkt := &PollReplyPacket{
		Port:        binary.LittleEndian.Uint16(data[14:16]),
		VersionInfo: binary.BigEndian.Uint16(data[16:18]),
		NetSwitch:   data[18],
		SubSwitch:   data[19],
		OemHi:       data[20],
		Oem:         data[21],
		UbeaVersion: data[22],
		Status1:     data[23],
		EstaMan:     binary.LittleEndian.Uint16(data[24:26]),
		NumPortsHi:  data[172],
		NumPortsLo:  data[173],
		Style:       data[200],
		BindIndex:   data[212],
		Status2:     data[213],
	}

	copy(pkt.IPAddress[:], data[10:14])
	copy(pkt.ShortName[:], data[26:44])
	copy(pkt.LongName[:], data[44:108])
	copy(pkt.NodeReport[:], data[108:172])
	copy(pkt.PortTypes[:], data[174:178])
	copy(pkt.GoodInput[:], data[178:182])
	copy(pkt.GoodOutput[:], data[182:186])
	copy(pkt.SwIn[:], data[186:190])
	copy(pkt.SwOut[:], data[190:194])
	copy(pkt.MAC[:], data[201:207])
	copy(pkt.BindIP[:], data[207:211])

	return pkt, nil
}

// BuildDMXPacket creates a raw ArtDmx packet
func BuildDMXPacket(universe Universe, sequence uint8, data []byte) []byte {
	dataLen := len(data)
	if dataLen > 512 {
		dataLen = 512
	}
	// Length must be even
	if dataLen%2 != 0 {
		dataLen++
	}

	buf := make([]byte, 18+dataLen)

	// Header
	copy(buf[0:8], ArtNetID[:])
	binary.LittleEndian.PutUint16(buf[8:10], OpDmx)

	// DMX packet fields
	binary.BigEndian.PutUint16(buf[10:12], ProtocolVersion)
	buf[12] = sequence
	buf[13] = 0 // Physical
	binary.LittleEndian.PutUint16(buf[14:16], uint16(universe))
	binary.BigEndian.PutUint16(buf[16:18], uint16(dataLen))
	copy(buf[18:], data[:dataLen])

	return buf
}

// BuildPollPacket creates an ArtPoll packet
func BuildPollPacket() []byte {
	buf := make([]byte, 14)

	copy(buf[0:8], ArtNetID[:])
	binary.LittleEndian.PutUint16(buf[8:10], OpPoll)
	binary.BigEndian.PutUint16(buf[10:12], ProtocolVersion)
	buf[12] = 0x00 // Flags
	buf[13] = 0x00 // DiagPriority

	return buf
}

// BuildPollReplyPacket creates an ArtPollReply packet
func BuildPollReplyPacket(ip [4]byte, shortName, longName string, universes []Universe) []byte {
	buf := make([]byte, 239)

	copy(buf[0:8], ArtNetID[:])
	binary.LittleEndian.PutUint16(buf[8:10], OpPollReply)
	copy(buf[10:14], ip[:])
	binary.LittleEndian.PutUint16(buf[14:16], Port)
	binary.BigEndian.PutUint16(buf[16:18], ProtocolVersion)

	// Net/Subnet from first universe if available
	if len(universes) > 0 {
		buf[18] = universes[0].Net()
		buf[19] = universes[0].SubNet()
	}

	// Names
	copy(buf[26:44], shortName)
	copy(buf[44:108], longName)

	// Ports
	numPorts := len(universes)
	if numPorts > 4 {
		numPorts = 4
	}
	buf[173] = byte(numPorts)

	for i := 0; i < numPorts; i++ {
		buf[174+i] = 0xC0 // Output, can output DMX
		buf[182+i] = 0x80 // Data transmitted
		buf[190+i] = universes[i].Universe()
	}

	buf[200] = 0x00 // StNode

	return buf
}
