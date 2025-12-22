package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
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
}

func main() {
	configPath := flag.String("config", "config.toml", "path to config file")
	listenPort := flag.Int("port", artnet.Port, "ArtNet listen port")
	broadcastAddr := flag.String("broadcast", "2.255.255.255", "ArtNet broadcast address")
	flag.Parse()

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
		if m.Count == 512 && m.FromChannel == 1 {
			log.Printf("  %s -> %s (all channels)", m.From.Universe, m.To.Universe)
		} else {
			log.Printf("  %s[%d-%d] -> %s[%d-%d]",
				m.From.Universe, m.FromChannel, m.FromChannel+m.Count-1,
				m.To.Universe, m.ToChannel, m.ToChannel+m.Count-1)
		}
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
	}

	// Create receiver
	receiver, err := artnet.NewReceiver(*listenPort, app)
	if err != nil {
		log.Fatalf("failed to create receiver: %v", err)
	}
	app.receiver = receiver

	// Start everything
	receiver.Start()
	discovery.Start()

	log.Printf("listening on port %d", *listenPort)
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
				if err := a.sender.SendDMX(addr, out.Universe, out.Data[:]); err != nil {
					log.Printf("failed to send to %s: %v", node.IP, err)
				}
			}
		} else {
			// Broadcast if no nodes discovered
			if err := a.sender.SendDMXBroadcast(out.Universe, out.Data[:]); err != nil {
				log.Printf("failed to broadcast: %v", err)
			}
		}
	}
}

// HandlePoll implements artnet.PacketHandler
func (a *App) HandlePoll(src *net.UDPAddr, pkt *artnet.PollPacket) {
	a.discovery.HandlePoll(src)
}

// HandlePollReply implements artnet.PacketHandler
func (a *App) HandlePollReply(src *net.UDPAddr, pkt *artnet.PollReplyPacket) {
	a.discovery.HandlePollReply(src, pkt)
}

func init() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	fmt.Println("artmap - ArtNet Remapping Proxy")
	fmt.Println()
}
