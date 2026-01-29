package sacn

import (
	"encoding/binary"
	"log"
	"net"

	"golang.org/x/net/ipv4"
)

// DMXHandler is called when DMX data is received
type DMXHandler func(universe uint16, data [512]byte)

// Receiver listens for sACN packets
type Receiver struct {
	conn      *ipv4.PacketConn
	universes []uint16
	handler   DMXHandler
	done      chan struct{}
}

// NewReceiver creates a new sACN receiver for the given universes
func NewReceiver(universes []uint16, ifaceName string, handler DMXHandler) (*Receiver, error) {
	c, err := net.ListenPacket("udp4", ":5568")
	if err != nil {
		return nil, err
	}

	var iface *net.Interface
	if ifaceName != "" {
		iface, err = net.InterfaceByName(ifaceName)
		if err != nil {
			c.Close()
			return nil, err
		}
	}

	p := ipv4.NewPacketConn(c)

	for _, u := range universes {
		group := net.IPv4(239, 255, byte(u>>8), byte(u&0xff))
		if err := p.JoinGroup(iface, &net.UDPAddr{IP: group}); err != nil {
			c.Close()
			return nil, err
		}
	}

	return &Receiver{
		conn:      p,
		universes: universes,
		handler:   handler,
		done:      make(chan struct{}),
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
	buf := make([]byte, 638) // Max sACN packet size

	for {
		select {
		case <-r.done:
			return
		default:
		}

		n, _, _, err := r.conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-r.done:
				return
			default:
				log.Printf("sacn read error: %v", err)
				continue
			}
		}

		r.handlePacket(buf[:n])
	}
}

func (r *Receiver) handlePacket(data []byte) {
	// Minimum packet size check
	if len(data) < 126 {
		return
	}

	// Check ACN packet identifier
	if data[4] != 0x41 || data[5] != 0x53 || data[6] != 0x43 {
		return
	}

	// Check root vector (E1.31 data)
	rootVector := binary.BigEndian.Uint32(data[18:22])
	if rootVector != VectorRootE131Data {
		return
	}

	// Check framing vector (DMP data)
	framingVector := binary.BigEndian.Uint32(data[40:44])
	if framingVector != VectorE131DataPacket {
		return
	}

	// Get universe
	universe := binary.BigEndian.Uint16(data[113:115])

	// Check DMP vector
	if data[117] != VectorDMPSetProperty {
		return
	}

	// Get property count (includes START code)
	propCount := binary.BigEndian.Uint16(data[123:125])
	if propCount < 1 {
		return
	}

	// Skip START code at data[125]
	dmxLen := int(propCount) - 1
	if dmxLen > 512 {
		dmxLen = 512
	}

	if len(data) < 126+dmxLen {
		return
	}

	var dmxData [512]byte
	copy(dmxData[:], data[126:126+dmxLen])

	r.handler(universe, dmxData)
}
