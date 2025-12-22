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
)

type App struct {
	cfg       *config.Config
	receiver  *artnet.Receiver
	sender    *artnet.Sender
	discovery *artnet.Discovery
	engine    *remap.Engine
	debug     bool
}

func main() {
	configPath := flag.String("config", "config.toml", "path to config file")
	listenAddr := flag.String("listen", ":6454", "listen address (host:port, host, or :port)")
	broadcastAddr := flag.String("broadcast", "2.255.255.255", "ArtNet broadcast address")
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
		log.Printf("  %s:%d-%d -> %s:%d-%d",
			m.From.Universe, m.From.ChannelStart, m.From.ChannelEnd,
			m.To.Universe, m.To.ChannelStart, toEnd)
	}

	// Create sender
	sender, err := artnet.NewSender(*broadcastAddr)
	if err != nil {
		log.Fatalf("failed to create sender: %v", err)
	}
	defer sender.Close()

	// Create discovery
	destUniverses := engine.DestUniverses()
	discovery := artnet.NewDiscovery(sender, "artmap", "ArtNet Remapping Proxy", destUniverses)

	// Create app
	app := &App{
		cfg:       cfg,
		sender:    sender,
		discovery: discovery,
		engine:    engine,
		debug:     *debug,
	}

	// Create receiver
	receiver, err := artnet.NewReceiver(addr, app)
	if err != nil {
		log.Fatalf("failed to create receiver: %v", err)
	}
	app.receiver = receiver

	// Start everything
	receiver.Start()
	discovery.Start()

	log.Printf("listening on %s", addr)
	log.Printf("broadcasting to %s", *broadcastAddr)

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("shutting down...")
	receiver.Stop()
	discovery.Stop()
}

// HandleDMX implements artnet.PacketHandler
func (a *App) HandleDMX(src *net.UDPAddr, pkt *artnet.DMXPacket) {
	if a.debug {
		log.Printf("recv DMX from %s: universe=%s seq=%d len=%d",
			src.IP, pkt.Universe, pkt.Sequence, pkt.Length)
	}

	// Apply remapping
	outputs := a.engine.Remap(pkt.Universe, pkt.Data)

	// Send remapped outputs
	for _, out := range outputs {
		// Find nodes for this universe
		nodes := a.discovery.GetNodesForUniverse(out.Universe)

		if len(nodes) > 0 {
			// Send to discovered nodes
			for _, node := range nodes {
				addr := &net.UDPAddr{
					IP:   node.IP,
					Port: int(node.Port),
				}
				if a.debug {
					log.Printf("send DMX to %s: universe=%s", node.IP, out.Universe)
				}
				if err := a.sender.SendDMX(addr, out.Universe, out.Data[:]); err != nil {
					log.Printf("failed to send to %s: %v", node.IP, err)
				}
			}
		} else {
			// Broadcast if no nodes discovered
			if a.debug {
				log.Printf("send DMX broadcast: universe=%s", out.Universe)
			}
			if err := a.sender.SendDMXBroadcast(out.Universe, out.Data[:]); err != nil {
				log.Printf("failed to broadcast: %v", err)
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
