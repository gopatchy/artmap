package sacn

import (
	"crypto/rand"
	"net"
	"sync"
)

// Sender sends sACN (E1.31) packets
type Sender struct {
	conn       *net.UDPConn
	sourceName string
	cid        [16]byte
	sequences  map[uint16]uint8
	seqMu      sync.Mutex
}

// NewSender creates a new sACN sender
func NewSender(sourceName string) (*Sender, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}

	// Generate random CID
	var cid [16]byte
	rand.Read(cid[:])

	return &Sender{
		conn:       conn,
		sourceName: sourceName,
		cid:        cid,
		sequences:  make(map[uint16]uint8),
	}, nil
}

// SendDMX sends DMX data to a universe via multicast
func (s *Sender) SendDMX(universe uint16, data []byte) error {
	s.seqMu.Lock()
	seq := s.sequences[universe]
	s.sequences[universe] = seq + 1
	s.seqMu.Unlock()

	pkt := BuildDataPacket(universe, seq, s.sourceName, s.cid, data)
	addr := MulticastAddr(universe)

	_, err := s.conn.WriteToUDP(pkt, addr)
	return err
}

// SendDMXUnicast sends DMX data to a specific address
func (s *Sender) SendDMXUnicast(addr *net.UDPAddr, universe uint16, data []byte) error {
	s.seqMu.Lock()
	seq := s.sequences[universe]
	s.sequences[universe] = seq + 1
	s.seqMu.Unlock()

	pkt := BuildDataPacket(universe, seq, s.sourceName, s.cid, data)

	_, err := s.conn.WriteToUDP(pkt, addr)
	return err
}

// Close closes the sender
func (s *Sender) Close() error {
	return s.conn.Close()
}
