package artnet

import (
	"net"
	"sync"
)

// Sender sends ArtNet packets
type Sender struct {
	conn          *net.UDPConn
	broadcastAddr *net.UDPAddr
	sequences     map[Universe]uint8
	seqMu         sync.Mutex
}

// NewSender creates a new ArtNet sender
func NewSender(broadcastAddr string) (*Sender, error) {
	// Create a UDP socket for sending
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}

	// Enable broadcast
	if err := conn.SetWriteBuffer(65536); err != nil {
		conn.Close()
		return nil, err
	}

	broadcast, err := net.ResolveUDPAddr("udp4", broadcastAddr+":"+string(rune(Port)))
	if err != nil {
		// Try parsing as IP:Port
		broadcast, err = net.ResolveUDPAddr("udp4", broadcastAddr)
		if err != nil {
			broadcast = &net.UDPAddr{
				IP:   net.ParseIP(broadcastAddr),
				Port: Port,
			}
		}
	}

	return &Sender{
		conn:          conn,
		broadcastAddr: broadcast,
		sequences:     make(map[Universe]uint8),
	}, nil
}

// SendDMX sends a DMX packet to a specific address
func (s *Sender) SendDMX(addr *net.UDPAddr, universe Universe, data []byte) error {
	s.seqMu.Lock()
	seq := s.sequences[universe]
	seq++
	if seq == 0 {
		seq = 1 // Skip 0
	}
	s.sequences[universe] = seq
	s.seqMu.Unlock()

	pkt := BuildDMXPacket(universe, seq, data)
	_, err := s.conn.WriteToUDP(pkt, addr)
	return err
}

// SendDMXBroadcast sends a DMX packet to the broadcast address
func (s *Sender) SendDMXBroadcast(universe Universe, data []byte) error {
	return s.SendDMX(s.broadcastAddr, universe, data)
}

// SendPoll sends an ArtPoll packet to the broadcast address
func (s *Sender) SendPoll() error {
	pkt := BuildPollPacket()
	_, err := s.conn.WriteToUDP(pkt, s.broadcastAddr)
	return err
}

// SendPollReply sends an ArtPollReply to a specific address
func (s *Sender) SendPollReply(addr *net.UDPAddr, localIP [4]byte, shortName, longName string, universes []Universe) error {
	pkt := BuildPollReplyPacket(localIP, shortName, longName, universes)
	_, err := s.conn.WriteToUDP(pkt, addr)
	return err
}

// Close closes the sender
func (s *Sender) Close() error {
	return s.conn.Close()
}

// BroadcastAddr returns the configured broadcast address
func (s *Sender) BroadcastAddr() *net.UDPAddr {
	return s.broadcastAddr
}
