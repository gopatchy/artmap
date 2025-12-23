package sacn

import (
	"encoding/binary"
	"fmt"
	"log"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

// PcapReceiver listens for sACN packets using packet capture
type PcapReceiver struct {
	handle    *pcap.Handle
	universes map[uint16]bool
	handler   DMXHandler
	done      chan struct{}
}

// NewPcapReceiver creates a new sACN receiver using packet capture
// This requires root/admin privileges but avoids port conflicts
func NewPcapReceiver(iface string, universes []uint16, handler DMXHandler) (*PcapReceiver, error) {
	// Open device for capturing
	handle, err := pcap.OpenLive(iface, 1600, true, pcap.BlockForever)
	if err != nil {
		return nil, fmt.Errorf("pcap open: %w", err)
	}

	// Filter for UDP port 5568 (sACN) - captures both directions
	if err := handle.SetBPFFilter("udp port 5568"); err != nil {
		handle.Close()
		return nil, fmt.Errorf("pcap filter: %w", err)
	}

	universeMap := make(map[uint16]bool)
	for _, u := range universes {
		universeMap[u] = true
	}

	return &PcapReceiver{
		handle:    handle,
		universes: universeMap,
		handler:   handler,
		done:      make(chan struct{}),
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

	// Get payload
	data := udp.Payload
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

	// Check if we care about this universe
	if !r.universes[universe] {
		return
	}

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

// ListInterfaces returns available network interfaces for packet capture
func ListInterfaces() ([]string, error) {
	devices, err := pcap.FindAllDevs()
	if err != nil {
		return nil, err
	}

	var names []string
	for _, dev := range devices {
		names = append(names, dev.Name)
	}
	return names, nil
}

// DefaultInterface returns a reasonable default interface for capture
func DefaultInterface() string {
	devices, err := pcap.FindAllDevs()
	if err != nil {
		return "en0"
	}

	// Prefer interfaces with addresses
	for _, dev := range devices {
		if len(dev.Addresses) > 0 && dev.Name != "lo0" && dev.Name != "lo" {
			log.Printf("sacn pcap using interface: %s", dev.Name)
			return dev.Name
		}
	}

	if len(devices) > 0 {
		return devices[0].Name
	}
	return "en0"
}
