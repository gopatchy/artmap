package artnet

import (
	"log"
	"net"
)

// PacketHandler is called when a packet is received
type PacketHandler interface {
	HandleDMX(src *net.UDPAddr, pkt *DMXPacket)
	HandlePoll(src *net.UDPAddr, pkt *PollPacket)
	HandlePollReply(src *net.UDPAddr, pkt *PollReplyPacket)
}

// Receiver listens for ArtNet packets
type Receiver struct {
	conn    *net.UDPConn
	handler PacketHandler
	done    chan struct{}
}

// NewReceiver creates a new ArtNet receiver
func NewReceiver(addr *net.UDPAddr, handler PacketHandler) (*Receiver, error) {
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, err
	}

	return &Receiver{
		conn:    conn,
		handler: handler,
		done:    make(chan struct{}),
	}, nil
}

// Start begins receiving packets
func (r *Receiver) Start() {
	go r.receiveLoop()
}

// Stop stops the receiver
func (r *Receiver) Stop() {
	close(r.done)
	r.conn.Close()
}

func (r *Receiver) receiveLoop() {
	buf := make([]byte, 1024)

	for {
		select {
		case <-r.done:
			return
		default:
		}

		n, src, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-r.done:
				return
			default:
				log.Printf("read error: %v", err)
				continue
			}
		}

		r.handlePacket(src, buf[:n])
	}
}

func (r *Receiver) handlePacket(src *net.UDPAddr, data []byte) {
	opCode, pkt, err := ParsePacket(data)
	if err != nil {
		// Silently ignore invalid packets
		return
	}

	switch opCode {
	case OpDmx:
		if dmx, ok := pkt.(*DMXPacket); ok {
			r.handler.HandleDMX(src, dmx)
		}
	case OpPoll:
		if poll, ok := pkt.(*PollPacket); ok {
			r.handler.HandlePoll(src, poll)
		}
	case OpPollReply:
		if reply, ok := pkt.(*PollReplyPacket); ok {
			r.handler.HandlePollReply(src, reply)
		}
	}
}

// LocalAddr returns the local address the receiver is bound to
func (r *Receiver) LocalAddr() net.Addr {
	return r.conn.LocalAddr()
}

// SendTo sends a raw packet through the receiver's socket (port 6454)
func (r *Receiver) SendTo(data []byte, addr *net.UDPAddr) error {
	_, err := r.conn.WriteToUDP(data, addr)
	return err
}
