package artnet

import (
	"net"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

// PcapReceiver listens for ArtNet packets using packet capture
type PcapReceiver struct {
	handle  *pcap.Handle
	handler PacketHandler
	done    chan struct{}
}

// NewPcapReceiver creates a new ArtNet receiver using packet capture
// This requires root/admin privileges but avoids port conflicts
func NewPcapReceiver(iface string, handler PacketHandler) (*PcapReceiver, error) {
	// Open device for capturing
	handle, err := pcap.OpenLive(iface, 1600, true, pcap.BlockForever)
	if err != nil {
		return nil, err
	}

	// Filter for UDP port 6454 (ArtNet) - captures both directions
	if err := handle.SetBPFFilter("udp port 6454"); err != nil {
		handle.Close()
		return nil, err
	}

	return &PcapReceiver{
		handle:  handle,
		handler: handler,
		done:    make(chan struct{}),
	}, nil
}

// Start begins receiving packets
func (r *PcapReceiver) Start() {
	go r.receiveLoop()
}

// Stop stops the receiver
func (r *PcapReceiver) Stop() {
	close(r.done)
	r.handle.Close()
}

func (r *PcapReceiver) receiveLoop() {
	packetSource := gopacket.NewPacketSource(r.handle, r.handle.LinkType())

	for {
		select {
		case <-r.done:
			return
		case packet, ok := <-packetSource.Packets():
			if !ok {
				return
			}
			r.handlePacket(packet)
		}
	}
}

func (r *PcapReceiver) handlePacket(packet gopacket.Packet) {
	// Extract UDP layer
	udpLayer := packet.Layer(layers.LayerTypeUDP)
	if udpLayer == nil {
		return
	}

	udp, _ := udpLayer.(*layers.UDP)
	if udp == nil {
		return
	}

	// Extract IP layer for source address
	var srcIP, dstIP [4]byte
	if ipLayer := packet.Layer(layers.LayerTypeIPv4); ipLayer != nil {
		ip, _ := ipLayer.(*layers.IPv4)
		if ip != nil {
			copy(srcIP[:], ip.SrcIP.To4())
			copy(dstIP[:], ip.DstIP.To4())
		}
	}

	// Get payload
	data := udp.Payload
	if len(data) < 12 {
		return
	}

	// Parse the ArtNet packet
	opCode, pkt, err := ParsePacket(data)
	if err != nil {
		return
	}

	src := &net.UDPAddr{
		IP:   net.IP(srcIP[:]),
		Port: int(udp.SrcPort),
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
