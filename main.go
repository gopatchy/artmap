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
	cfg          *config.Config
	artReceiver  *artnet.Receiver
	sacnReceiver *sacn.Receiver
	artSender    *artnet.Sender
	sacnSender   *sacn.Sender
	discovery    *artnet.Discovery
	engine       *remap.Engine
	targets      map[artnet.Universe]*net.UDPAddr
	debug        bool
}

func main() {
	configPath := flag.String("config", "config.toml", "path to config file")
	listenAddr := flag.String("listen", ":6454", "listen address (host:port, host, or :port)")
	debug := flag.Bool("debug", false, "log ArtNet packets")
	flag.Parse()

	// Parse listen address
	addr, err := parseListenAddr(*listenAddr)
	if err != nil {
		log.Fatalf("invalid listen address: %v", err)
	}

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	log.Printf("loaded %d mappings", len(cfg.Mappings))

	// Create remapping engine
	engine := remap.NewEngine(cfg.Normalize())

	// Log mappings
	for _, m := range cfg.Mappings {
		toEnd := m.To.ChannelStart + m.From.Count() - 1
		log.Printf("  [%s] %s:%d-%d -> [%s] %s:%d-%d",
			m.FromProto, m.From.Universe, m.From.ChannelStart, m.From.ChannelEnd,
			m.Protocol, m.To.Universe, m.To.ChannelStart, toEnd)
	}

	// Parse targets
	targets := make(map[artnet.Universe]*net.UDPAddr)
	pollTargets := make(map[string]*net.UDPAddr) // dedupe by address string
	for _, t := range cfg.Targets {
		addr, err := parseTargetAddr(t.Address)
		if err != nil {
			log.Fatalf("invalid target address %q: %v", t.Address, err)
		}
		targets[t.Universe.Universe] = addr
		pollTargets[addr.String()] = addr
		log.Printf("  target %s -> %s", t.Universe.Universe, addr)
	}

	// Convert poll targets to slice
	pollTargetSlice := make([]*net.UDPAddr, 0, len(pollTargets))
	for _, addr := range pollTargets {
		pollTargetSlice = append(pollTargetSlice, addr)
	}

	// Create ArtNet sender
	artSender, err := artnet.NewSender()
	if err != nil {
		log.Fatalf("failed to create artnet sender: %v", err)
	}
	defer artSender.Close()

	// Create sACN sender
	sacnSender, err := sacn.NewSender("artmap")
	if err != nil {
		log.Fatalf("failed to create sacn sender: %v", err)
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
		debug:      *debug,
	}

	// Create ArtNet receiver
	artReceiver, err := artnet.NewReceiver(addr, app)
	if err != nil {
		log.Fatalf("failed to create artnet receiver: %v", err)
	}
	app.artReceiver = artReceiver

	// Create sACN receiver if needed
	sacnUniverses := cfg.SACNSourceUniverses()
	if len(sacnUniverses) > 0 {
		sacnReceiver, err := sacn.NewReceiver(sacnUniverses, app.HandleSACN)
		if err != nil {
			log.Fatalf("failed to create sacn receiver: %v", err)
		}
		app.sacnReceiver = sacnReceiver
		sacnReceiver.Start()
		log.Printf("listening for sACN on universes %v", sacnUniverses)
	}

	// Start everything
	artReceiver.Start()
	discovery.Start()

	log.Printf("listening for ArtNet on %s", addr)

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("shutting down...")
	artReceiver.Stop()
	if app.sacnReceiver != nil {
		app.sacnReceiver.Stop()
	}
	discovery.Stop()
}

// HandleDMX implements artnet.PacketHandler
func (a *App) HandleDMX(src *net.UDPAddr, pkt *artnet.DMXPacket) {
	if a.debug {
		log.Printf("recv ArtNet from %s: universe=%s seq=%d len=%d",
			src.IP, pkt.Universe, pkt.Sequence, pkt.Length)
	}

	// Apply remapping
	outputs := a.engine.Remap(config.ProtocolArtNet, pkt.Universe, pkt.Data)

	// Send remapped outputs
	for _, out := range outputs {
		switch out.Protocol {
		case config.ProtocolSACN:
			if a.debug {
				log.Printf("send sACN multicast: universe=%d", uint16(out.Universe))
			}
			if err := a.sacnSender.SendDMX(uint16(out.Universe), out.Data[:]); err != nil {
				log.Printf("failed to send sACN: %v", err)
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
						log.Printf("send ArtNet to %s: universe=%s", node.IP, out.Universe)
					}
					if err := a.artSender.SendDMX(addr, out.Universe, out.Data[:]); err != nil {
						log.Printf("failed to send to %s: %v", node.IP, err)
					}
				}
			} else if target, ok := a.targets[out.Universe]; ok {
				if a.debug {
					log.Printf("send ArtNet to %s: universe=%s", target.IP, out.Universe)
				}
				if err := a.artSender.SendDMX(target, out.Universe, out.Data[:]); err != nil {
					log.Printf("failed to send to %s: %v", target.IP, err)
				}
			} else {
				log.Printf("no target configured for universe %s", out.Universe)
			}
		}
	}
}

// HandlePoll implements artnet.PacketHandler
func (a *App) HandlePoll(src *net.UDPAddr, pkt *artnet.PollPacket) {
	if a.debug {
		log.Printf("recv Poll from %s", src.IP)
	}
	a.discovery.HandlePoll(src)
}

// HandlePollReply implements artnet.PacketHandler
func (a *App) HandlePollReply(src *net.UDPAddr, pkt *artnet.PollReplyPacket) {
	if a.debug {
		log.Printf("recv PollReply from %s", src.IP)
	}
	a.discovery.HandlePollReply(src, pkt)
}

// HandleSACN handles incoming sACN DMX data
func (a *App) HandleSACN(universe uint16, data [512]byte) {
	if a.debug {
		log.Printf("recv sACN: universe=%d", universe)
	}

	// Apply remapping
	outputs := a.engine.Remap(config.ProtocolSACN, artnet.Universe(universe), data)

	// Send remapped outputs
	for _, out := range outputs {
		switch out.Protocol {
		case config.ProtocolSACN:
			if a.debug {
				log.Printf("send sACN multicast: universe=%d", uint16(out.Universe))
			}
			if err := a.sacnSender.SendDMX(uint16(out.Universe), out.Data[:]); err != nil {
				log.Printf("failed to send sACN: %v", err)
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
						log.Printf("send ArtNet to %s: universe=%s", node.IP, out.Universe)
					}
					if err := a.artSender.SendDMX(addr, out.Universe, out.Data[:]); err != nil {
						log.Printf("failed to send to %s: %v", node.IP, err)
					}
				}
			} else if target, ok := a.targets[out.Universe]; ok {
				if a.debug {
					log.Printf("send ArtNet to %s: universe=%s", target.IP, out.Universe)
				}
				if err := a.artSender.SendDMX(target, out.Universe, out.Data[:]); err != nil {
					log.Printf("failed to send to %s: %v", target.IP, err)
				}
			} else {
				log.Printf("no target configured for universe %s", out.Universe)
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
