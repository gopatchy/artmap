package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/gopatchy/artmap/artnet"
	"github.com/gopatchy/artmap/config"
	"github.com/gopatchy/artmap/remap"
	"github.com/gopatchy/artmap/sacn"
)

type App struct {
	cfg              *config.Config
	artReceiver      *artnet.Receiver
	sacnReceiver     *sacn.Receiver
	sacnPcapReceiver *sacn.PcapReceiver
	artSender        *artnet.Sender
	sacnSender       *sacn.Sender
	discovery        *artnet.Discovery
	engine           *remap.Engine
	targets          map[artnet.Universe]*net.UDPAddr
	broadcasts       []*net.UDPAddr
	debug            bool
}

func main() {
	configPath := flag.String("config", "config.toml", "path to config file")
	artnetListen := flag.String("artnet-listen", ":6454", "artnet listen address (empty to disable)")
	artnetBroadcast := flag.String("artnet-broadcast", "", "artnet broadcast addresses (comma-separated, or 'auto')")
	sacnPcap := flag.String("sacn-pcap", "", "use pcap for sacn on interface (e.g. en0, eth0)")
	debug := flag.Bool("debug", false, "log incoming/outgoing dmx packets")
	flag.Parse()

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	log.Printf("loaded mappings=%d", len(cfg.Mappings))

	// Create remapping engine
	engine := remap.NewEngine(cfg.Normalize())

	// Log mappings
	for _, m := range cfg.Mappings {
		log.Printf("  %s -> %s", m.From, m.To)
	}

	// Parse targets
	targets := make(map[artnet.Universe]*net.UDPAddr)
	pollTargets := make(map[string]*net.UDPAddr) // dedupe by address string
	for _, t := range cfg.Targets {
		if t.Universe.Protocol != config.ProtocolArtNet {
			continue // only artnet targets need addresses
		}
		addr, err := parseTargetAddr(t.Address)
		if err != nil {
			log.Fatalf("target error: address=%q err=%v", t.Address, err)
		}
		targets[t.Universe.Universe] = addr
		pollTargets[addr.String()] = addr
		log.Printf("  target %s -> %s", t.Universe, addr)
	}

	// Parse broadcast addresses
	var broadcasts []*net.UDPAddr
	if *artnetBroadcast != "" {
		if *artnetBroadcast == "auto" {
			broadcasts = detectBroadcastAddrs()
		} else {
			for _, addrStr := range strings.Split(*artnetBroadcast, ",") {
				addrStr = strings.TrimSpace(addrStr)
				addr, err := parseTargetAddr(addrStr)
				if err != nil {
					log.Fatalf("broadcast error: address=%q err=%v", addrStr, err)
				}
				broadcasts = append(broadcasts, addr)
			}
		}
		for _, addr := range broadcasts {
			pollTargets[addr.String()] = addr
			log.Printf("  broadcast %s", addr)
		}
	}

	// Convert poll targets to slice
	pollTargetSlice := make([]*net.UDPAddr, 0, len(pollTargets))
	for _, addr := range pollTargets {
		pollTargetSlice = append(pollTargetSlice, addr)
	}

	// Create ArtNet sender
	artSender, err := artnet.NewSender()
	if err != nil {
		log.Fatalf("artnet sender error: %v", err)
	}
	defer artSender.Close()

	// Create sACN sender
	sacnSender, err := sacn.NewSender("artmap")
	if err != nil {
		log.Fatalf("sacn sender error: %v", err)
	}
	defer sacnSender.Close()

	// Create discovery
	destUniverses := engine.DestUniverses()
	discovery := artnet.NewDiscovery(artSender, "artmap", "ArtNet Remapping Proxy", destUniverses, pollTargetSlice)

	// Create app
	app := &App{
		cfg:        cfg,
		artSender:  artSender,
		sacnSender: sacnSender,
		discovery:  discovery,
		engine:     engine,
		targets:    targets,
		broadcasts: broadcasts,
		debug:      *debug,
	}

	// Create ArtNet receiver if enabled
	if *artnetListen != "" {
		addr, err := parseListenAddr(*artnetListen)
		if err != nil {
			log.Fatalf("artnet listen error: %v", err)
		}
		artReceiver, err := artnet.NewReceiver(addr, app)
		if err != nil {
			log.Fatalf("artnet receiver error: %v", err)
		}
		app.artReceiver = artReceiver
		artReceiver.Start()
		log.Printf("artnet listening addr=%s", addr)
	}

	// Create sACN receiver if needed
	sacnUniverses := cfg.SACNSourceUniverses()
	if len(sacnUniverses) > 0 {
		if *sacnPcap != "" {
			// Use pcap-based receiver (requires root, avoids port conflicts)
			iface := *sacnPcap
			if iface == "auto" {
				iface = sacn.DefaultInterface()
			}
			pcapReceiver, err := sacn.NewPcapReceiver(iface, sacnUniverses, app.HandleSACN)
			if err != nil {
				log.Fatalf("sacn pcap error: %v", err)
			}
			app.sacnPcapReceiver = pcapReceiver
			pcapReceiver.Start()
			log.Printf("sacn pcap listening iface=%s universes=%v", iface, sacnUniverses)
		} else {
			// Use standard UDP receiver
			sacnReceiver, err := sacn.NewReceiver(sacnUniverses, app.HandleSACN)
			if err != nil {
				log.Fatalf("sacn receiver error: %v", err)
			}
			app.sacnReceiver = sacnReceiver
			sacnReceiver.Start()
			log.Printf("sacn listening universes=%v", sacnUniverses)
		}
	}

	// Start discovery
	discovery.Start()

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("shutting down")
	if app.artReceiver != nil {
		app.artReceiver.Stop()
	}
	if app.sacnReceiver != nil {
		app.sacnReceiver.Stop()
	}
	if app.sacnPcapReceiver != nil {
		app.sacnPcapReceiver.Stop()
	}
	discovery.Stop()
}

// HandleDMX implements artnet.PacketHandler
func (a *App) HandleDMX(src *net.UDPAddr, pkt *artnet.DMXPacket) {
	if a.debug {
		log.Printf("[<-artnet] src=%s universe=%s seq=%d len=%d",
			src.IP, pkt.Universe, pkt.Sequence, pkt.Length)
	}

	a.sendOutputs(a.engine.Remap(config.ProtocolArtNet, pkt.Universe, pkt.Data))
}

// HandlePoll implements artnet.PacketHandler
func (a *App) HandlePoll(src *net.UDPAddr, pkt *artnet.PollPacket) {
	if a.debug {
		log.Printf("[<-artnet] poll src=%s", src.IP)
	}
	a.discovery.HandlePoll(src)
}

// HandlePollReply implements artnet.PacketHandler
func (a *App) HandlePollReply(src *net.UDPAddr, pkt *artnet.PollReplyPacket) {
	if a.debug {
		log.Printf("[<-artnet] pollreply src=%s", src.IP)
	}
	a.discovery.HandlePollReply(src, pkt)
}

// HandleSACN handles incoming sACN DMX data
func (a *App) HandleSACN(universe uint16, data [512]byte) {
	if a.debug {
		log.Printf("[<-sacn] universe=%d", universe)
	}

	a.sendOutputs(a.engine.Remap(config.ProtocolSACN, artnet.Universe(universe), data))
}

func (a *App) sendOutputs(outputs []remap.Output) {
	for _, out := range outputs {
		switch out.Protocol {
		case config.ProtocolSACN:
			if a.debug {
				log.Printf("[->sacn] universe=%d", uint16(out.Universe))
			}
			if err := a.sacnSender.SendDMX(uint16(out.Universe), out.Data[:]); err != nil {
				log.Printf("[->sacn] error: universe=%d err=%v", uint16(out.Universe), err)
			}

		default: // ArtNet
			nodes := a.discovery.GetNodesForUniverse(out.Universe)

			if len(nodes) > 0 {
				for _, node := range nodes {
					addr := &net.UDPAddr{
						IP:   node.IP,
						Port: int(node.Port),
					}
					if a.debug {
						log.Printf("[->artnet] dst=%s universe=%s", node.IP, out.Universe)
					}
					if err := a.artSender.SendDMX(addr, out.Universe, out.Data[:]); err != nil {
						log.Printf("[->artnet] error: dst=%s err=%v", node.IP, err)
					}
				}
			} else if target, ok := a.targets[out.Universe]; ok {
				if a.debug {
					log.Printf("[->artnet] dst=%s universe=%s", target.IP, out.Universe)
				}
				if err := a.artSender.SendDMX(target, out.Universe, out.Data[:]); err != nil {
					log.Printf("[->artnet] error: dst=%s err=%v", target.IP, err)
				}
			} else if len(a.broadcasts) > 0 {
				for _, bcast := range a.broadcasts {
					if a.debug {
						log.Printf("[->artnet] broadcast dst=%s universe=%s", bcast.IP, out.Universe)
					}
					if err := a.artSender.SendDMX(bcast, out.Universe, out.Data[:]); err != nil {
						log.Printf("[->artnet] error: dst=%s err=%v", bcast.IP, err)
					}
				}
			} else {
				log.Printf("[->artnet] no target: universe=%s", out.Universe)
			}
		}
	}
}

func init() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	fmt.Println("artmap - ArtNet Remapping Proxy")
	fmt.Println()
}

// parseListenAddr parses listen address formats:
// - "host:port" -> bind to specific host and port
// - "host" -> bind to specific host, default port
// - ":port" -> bind to all interfaces, specific port
func parseListenAddr(s string) (*net.UDPAddr, error) {
	var host string
	var port int

	if strings.Contains(s, ":") {
		h, p, err := net.SplitHostPort(s)
		if err != nil {
			return nil, err
		}
		host = h
		if p == "" {
			port = artnet.Port
		} else {
			port, err = strconv.Atoi(p)
			if err != nil {
				return nil, err
			}
		}
	} else {
		host = s
		port = artnet.Port
	}

	var ip net.IP
	if host == "" {
		ip = net.IPv4zero
	} else {
		ip = net.ParseIP(host)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP address: %s", host)
		}
	}

	return &net.UDPAddr{IP: ip, Port: port}, nil
}

// parseTargetAddr parses target address formats:
// - "host:port" -> specific host and port
// - "host" -> specific host, default ArtNet port
func parseTargetAddr(s string) (*net.UDPAddr, error) {
	var host string
	var port int

	if strings.Contains(s, ":") {
		h, p, err := net.SplitHostPort(s)
		if err != nil {
			return nil, err
		}
		host = h
		port, err = strconv.Atoi(p)
		if err != nil {
			return nil, err
		}
	} else {
		host = s
		port = artnet.Port
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", host)
	}

	return &net.UDPAddr{IP: ip, Port: port}, nil
}

// detectBroadcastAddrs returns broadcast addresses for all network interfaces
func detectBroadcastAddrs() []*net.UDPAddr {
	var addrs []*net.UDPAddr
	seen := make(map[string]bool)

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	for _, iface := range ifaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		ifaceAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range ifaceAddrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}

			// Calculate broadcast address: IP | ~mask
			mask := ipnet.Mask
			if len(mask) != 4 {
				continue
			}

			broadcast := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				broadcast[i] = ip4[i] | ^mask[i]
			}

			key := broadcast.String()
			if seen[key] {
				continue
			}
			seen[key] = true

			addrs = append(addrs, &net.UDPAddr{
				IP:   broadcast,
				Port: artnet.Port,
			})
		}
	}

	return addrs
}
