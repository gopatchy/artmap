package artnet

import (
	"log"
	"net"
	"sync"
	"time"
)

// Node represents a discovered ArtNet node
type Node struct {
	IP          net.IP
	Port        uint16
	ShortName   string
	LongName    string
	Universes   []Universe
	LastSeen    time.Time
	CanTransmit bool
}

// Discovery handles ArtNet node discovery
type Discovery struct {
	sender      *Sender
	nodes       map[string]*Node // keyed by IP string
	nodesMu     sync.RWMutex
	localIP     [4]byte
	shortName   string
	longName    string
	universes   []Universe
	pollTargets []*net.UDPAddr
	done        chan struct{}
}

// NewDiscovery creates a new discovery handler
func NewDiscovery(sender *Sender, shortName, longName string, universes []Universe, pollTargets []*net.UDPAddr) *Discovery {
	return &Discovery{
		sender:      sender,
		nodes:       make(map[string]*Node),
		shortName:   shortName,
		longName:    longName,
		universes:   universes,
		pollTargets: pollTargets,
		done:        make(chan struct{}),
	}
}

// Start begins periodic discovery
func (d *Discovery) Start() {
	// Get local IP
	d.localIP = d.getLocalIP()

	// Start periodic poll
	go d.pollLoop()
}

// Stop stops discovery
func (d *Discovery) Stop() {
	close(d.done)
}

func (d *Discovery) pollLoop() {
	// Send initial poll to all targets
	d.sendPolls()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	cleanupTicker := time.NewTicker(30 * time.Second)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-d.done:
			return
		case <-ticker.C:
			d.sendPolls()
		case <-cleanupTicker.C:
			d.cleanup()
		}
	}
}

func (d *Discovery) sendPolls() {
	for _, target := range d.pollTargets {
		if err := d.sender.SendPoll(target); err != nil {
			log.Printf("[->artnet] poll error: dst=%s err=%v", target.IP, err)
		}
	}
}

func (d *Discovery) cleanup() {
	d.nodesMu.Lock()
	defer d.nodesMu.Unlock()

	cutoff := time.Now().Add(-60 * time.Second)
	for ip, node := range d.nodes {
		if node.LastSeen.Before(cutoff) {
			log.Printf("[artnet] node timeout ip=%s name=%s", ip, node.ShortName)
			delete(d.nodes, ip)
		}
	}
}

// HandlePollReply processes an incoming ArtPollReply
func (d *Discovery) HandlePollReply(src *net.UDPAddr, pkt *PollReplyPacket) {
	d.nodesMu.Lock()
	defer d.nodesMu.Unlock()

	ip := src.IP.String()

	// Skip our own replies
	localIP := net.IP(d.localIP[:])
	if src.IP.Equal(localIP) {
		return
	}

	// Parse universes from SwOut
	var universes []Universe
	numPorts := int(pkt.NumPortsLo)
	if numPorts > 4 {
		numPorts = 4
	}

	for i := 0; i < numPorts; i++ {
		// Check if port can output DMX
		if pkt.PortTypes[i]&0x80 != 0 {
			u := NewUniverse(pkt.NetSwitch, pkt.SubSwitch, pkt.SwOut[i])
			universes = append(universes, u)
		}
	}

	shortName := string(pkt.ShortName[:])
	// Trim null bytes
	for i, b := range pkt.ShortName {
		if b == 0 {
			shortName = string(pkt.ShortName[:i])
			break
		}
	}

	longName := string(pkt.LongName[:])
	for i, b := range pkt.LongName {
		if b == 0 {
			longName = string(pkt.LongName[:i])
			break
		}
	}

	node, exists := d.nodes[ip]
	if !exists {
		node = &Node{
			IP:   src.IP,
			Port: pkt.Port, // Use port from packet, not UDP source port
		}
		d.nodes[ip] = node
	}

	node.ShortName = shortName
	node.LongName = longName
	node.LastSeen = time.Now()
	node.CanTransmit = true

	// Accumulate universes from multiple ArtPollReply packets
	// (multi-port devices send separate replies for each group of 4 ports)
	prevLen := len(node.Universes)
	for _, u := range universes {
		found := false
		for _, existing := range node.Universes {
			if existing == u {
				found = true
				break
			}
		}
		if !found {
			node.Universes = append(node.Universes, u)
		}
	}

	if !exists {
		log.Printf("[artnet] discovered ip=%s name=%s universes=%v", ip, shortName, node.Universes)
	} else if len(node.Universes) != prevLen {
		log.Printf("[artnet] updated ip=%s name=%s universes=%v", ip, shortName, node.Universes)
	}
}

// HandlePoll processes an incoming ArtPoll and responds
func (d *Discovery) HandlePoll(src *net.UDPAddr) {
	// Respond with our info
	err := d.sender.SendPollReply(src, d.localIP, d.shortName, d.longName, d.universes)
	if err != nil {
		log.Printf("[->artnet] pollreply error: dst=%s err=%v", src.IP, err)
	}
}

// GetNodesForUniverse returns nodes that support a given universe
func (d *Discovery) GetNodesForUniverse(universe Universe) []*Node {
	d.nodesMu.RLock()
	defer d.nodesMu.RUnlock()

	var result []*Node
	for _, node := range d.nodes {
		for _, u := range node.Universes {
			if u == universe {
				result = append(result, node)
				break
			}
		}
	}

	if len(result) == 0 && len(d.nodes) > 0 {
		log.Printf("[artnet] no nodes for universe=%s, have %d nodes", universe, len(d.nodes))
		for ip, node := range d.nodes {
			log.Printf("[artnet]   node ip=%s universes=%v", ip, node.Universes)
		}
	}

	return result
}

// GetAllNodes returns all discovered nodes
func (d *Discovery) GetAllNodes() []*Node {
	d.nodesMu.RLock()
	defer d.nodesMu.RUnlock()

	result := make([]*Node, 0, len(d.nodes))
	for _, node := range d.nodes {
		result = append(result, node)
	}
	return result
}

func (d *Discovery) getLocalIP() [4]byte {
	var result [4]byte

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return result
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				copy(result[:], ip4)
				return result
			}
		}
	}

	return result
}

// SetLocalIP sets the local IP for PollReply responses
func (d *Discovery) SetLocalIP(ip net.IP) {
	if ip4 := ip.To4(); ip4 != nil {
		copy(d.localIP[:], ip4)
	}
}
