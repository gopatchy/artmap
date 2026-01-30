package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gopatchy/artnet"
	"github.com/gopatchy/artmap/config"
	"github.com/gopatchy/artmap/remap"
	"github.com/gopatchy/sacn"
)

type App struct {
	cfg           *config.Config
	artReceiver   *artnet.Receiver
	sacnReceivers []*sacn.Receiver
	artSender     *artnet.Sender
	sacnSender    *sacn.Sender
	discovery     *artnet.Discovery
	engine        *remap.Engine
	artTargets    map[uint16]*net.UDPAddr
	sacnTargets   map[uint16][]*net.UDPAddr
	debug         bool

	inputMu       sync.Mutex
	inputBySrc    map[string]uint64
	inputByUniv   map[string]uint64
}

func main() {
	configPath := flag.String("config", "config.toml", "path to config file")
	artnetListen := flag.String("artnet-listen", ":6454", "artnet listen address (empty to disable)")
	artnetBroadcast := flag.String("artnet-broadcast", "auto", "artnet broadcast addresses (comma-separated, or 'auto')")
	sacnInterface := flag.String("sacn-interface", "", "network interface for sACN multicast")
	apiListen := flag.String("api-listen", ":8080", "HTTP API listen address (empty to disable)")
	debug := flag.Bool("debug", false, "log incoming/outgoing dmx packets")
	flag.Parse()

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	log.Printf("[config] loaded mappings=%d", len(cfg.Mappings))

	// Create remapping engine
	engine := remap.NewEngine(cfg.Normalize())

	// Log mappings
	for _, m := range cfg.Mappings {
		log.Printf("[config]   %s -> %s", m.From, m.To)
	}

	// Parse targets
	artTargets := make(map[uint16]*net.UDPAddr)
	sacnTargets := make(map[uint16][]*net.UDPAddr)
	pollTargets := make(map[string]*net.UDPAddr)
	for _, t := range cfg.Targets {
		addr, err := parseTargetAddr(t.Address, protocolPort(t.Universe.Protocol))
		if err != nil {
			log.Fatalf("target error: address=%q err=%v", t.Address, err)
		}
		switch t.Universe.Protocol {
		case config.ProtocolArtNet:
			artTargets[t.Universe.Number] = addr
			pollTargets[addr.String()] = addr
		case config.ProtocolSACN:
			sacnTargets[t.Universe.Number] = append(sacnTargets[t.Universe.Number], addr)
		}
		log.Printf("[config]   target %s -> %s", t.Universe, addr)
	}

	// Parse broadcast addresses
	var broadcasts []*net.UDPAddr
	if *artnetBroadcast != "" {
		if *artnetBroadcast == "auto" {
			broadcasts = detectBroadcastAddrs()
		} else {
			for _, addrStr := range strings.Split(*artnetBroadcast, ",") {
				addrStr = strings.TrimSpace(addrStr)
				addr, err := parseTargetAddr(addrStr, artnet.Port)
				if err != nil {
					log.Fatalf("broadcast error: address=%q err=%v", addrStr, err)
				}
				broadcasts = append(broadcasts, addr)
			}
		}
		for _, addr := range broadcasts {
			pollTargets[addr.String()] = addr
			log.Printf("[config]   broadcast %s", addr)
		}
	}

	// Create ArtNet sender
	artSender, err := artnet.NewSender()
	if err != nil {
		log.Fatalf("artnet sender error: %v", err)
	}
	defer artSender.Close()

	sacnSender, err := sacn.NewSender("artmap", *sacnInterface)
	if err != nil {
		log.Fatalf("sacn sender error: %v", err)
	}
	defer sacnSender.Close()

	for _, u := range engine.DestSACNUniverses() {
		sacnSender.RegisterUniverse(u)
	}
	sacnSender.StartDiscovery()

	// Create discovery
	destNums := engine.DestArtNetUniverses()
	inputUnivs := make([]artnet.Universe, len(destNums))
	for i, n := range destNums {
		inputUnivs[i] = artnet.Universe(n)
	}
	srcNums := engine.SourceArtNetUniverses()
	outputUnivs := make([]artnet.Universe, len(srcNums))
	for i, n := range srcNums {
		outputUnivs[i] = artnet.Universe(n)
	}

	// Get local interface info for discovery
	var localIP, broadcastIP net.IP
	var localMAC net.HardwareAddr
	if len(broadcasts) > 0 {
		broadcastIP = broadcasts[0].IP
		localIP, localMAC = detectLocalInterface(broadcastIP)
	}
	discovery := artnet.NewDiscovery(artSender, localIP, broadcastIP, localMAC, "artmap", "artmap", inputUnivs, outputUnivs)

	// Create app
	app := &App{
		cfg:          cfg,
		artSender:    artSender,
		sacnSender:   sacnSender,
		discovery:    discovery,
		engine:       engine,
		artTargets:   artTargets,
		sacnTargets:  sacnTargets,
		debug:        *debug,
		inputBySrc:   map[string]uint64{},
		inputByUniv:  map[string]uint64{},
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
		discovery.SetReceiver(artReceiver)
		artReceiver.Start()
		log.Printf("[artnet] listening addr=%s", addr)
	}

	// Create sACN receivers (one per universe)
	sacnUniverses := cfg.SACNSourceUniverses()
	if len(sacnUniverses) > 0 {
		var iface *net.Interface
		if *sacnInterface != "" {
			iface, _ = net.InterfaceByName(*sacnInterface)
		}
		for _, u := range sacnUniverses {
			receiver, err := sacn.NewUniverseReceiver(iface, u)
			if err != nil {
				log.Printf("[sacn] failed to create receiver for universe %d: %v", u, err)
				continue
			}
			receiver.SetHandler(func(src *net.UDPAddr, pkt interface{}) {
				if data, ok := pkt.(*sacn.DataPacket); ok {
					app.HandleSACN(src, data.Universe, data.Data)
				}
			})
			app.sacnReceivers = append(app.sacnReceivers, receiver)
			receiver.Start()
		}
		log.Printf("[sacn] listening universes=%v", sacnUniverses)
	}

	// Start discovery only if we have ArtNet outputs
	if len(destNums) > 0 || len(artTargets) > 0 {
		discovery.Start()
	}

	// Start HTTP API server
	if *apiListen != "" {
		go func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/config", app.handleConfig)
			server := &http.Server{
				Addr:    *apiListen,
				Handler: mux,
			}
			log.Printf("[api] listening addr=%s", *apiListen)
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("[api] server error: %v", err)
			}
		}()
	}

	// Start stats printer
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			app.printStats()
		}
	}()

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("[main] shutting down")
	if app.artReceiver != nil {
		app.artReceiver.Stop()
	}
	for _, r := range app.sacnReceivers {
		r.Stop()
	}
	discovery.Stop()
}

// HandleDMX implements artnet.PacketHandler
func (a *App) HandleDMX(src *net.UDPAddr, pkt *artnet.DMXPacket) {
	if a.debug {
		log.Printf("[<-artnet] src=%s universe=%s seq=%d len=%d",
			src.IP, pkt.Universe, pkt.Sequence, pkt.Length)
	}
	a.inputMu.Lock()
	a.inputBySrc[src.IP.String()]++
	a.inputByUniv[fmt.Sprintf("artnet:%d", pkt.Universe)]++
	a.inputMu.Unlock()

	u := config.Universe{Protocol: config.ProtocolArtNet, Number: uint16(pkt.Universe)}
	a.sendOutputs(a.engine.Remap(u, pkt.Data))
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
func (a *App) HandleSACN(src *net.UDPAddr, universe uint16, data [512]byte) {
	if a.debug {
		log.Printf("[<-sacn] src=%s universe=%d", src.IP, universe)
	}
	a.inputMu.Lock()
	a.inputBySrc[src.IP.String()]++
	a.inputByUniv[fmt.Sprintf("sacn:%d", universe)]++
	a.inputMu.Unlock()

	u := config.Universe{Protocol: config.ProtocolSACN, Number: universe}
	a.sendOutputs(a.engine.Remap(u, data))
}

func (a *App) sendOutputs(outputs []remap.Output) {
	for _, out := range outputs {
		switch out.Universe.Protocol {
		case config.ProtocolSACN:
			u := out.Universe.Number
			if a.debug {
				log.Printf("[->sacn] universe=%d", u)
			}
			if err := a.sacnSender.SendDMX(u, out.Data[:]); err != nil {
				log.Printf("[->sacn] error: universe=%d err=%v", u, err)
			}
			for _, target := range a.sacnTargets[u] {
				if a.debug {
					log.Printf("[->sacn] unicast dst=%s universe=%d", target.IP, u)
				}
				if err := a.sacnSender.SendDMXUnicast(target, u, out.Data[:]); err != nil {
					log.Printf("[->sacn] error: dst=%s err=%v", target.IP, err)
				}
			}

		case config.ProtocolArtNet:
			u := out.Universe.Number
			artU := artnet.Universe(u)
			if target, ok := a.artTargets[u]; ok {
				if a.debug {
					log.Printf("[->artnet] dst=%s universe=%s", target.IP, out.Universe)
				}
				if err := a.artSender.SendDMX(target, artU, out.Data[:]); err != nil {
					log.Printf("[->artnet] error: dst=%s err=%v", target.IP, err)
				}
			} else if nodes := a.discovery.GetNodesForUniverse(artU); len(nodes) > 0 {
				for _, node := range nodes {
					addr := &net.UDPAddr{
						IP:   node.IP,
						Port: int(node.Port),
					}
					if a.debug {
						log.Printf("[->artnet] dst=%s universe=%s", node.IP, out.Universe)
					}
					if err := a.artSender.SendDMX(addr, artU, out.Data[:]); err != nil {
						log.Printf("[->artnet] error: dst=%s err=%v", node.IP, err)
					}
				}
			}
		}
	}
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "artmap")
	json.NewEncoder(w).Encode(a.cfg)
}

func (a *App) printStats() {
	a.inputMu.Lock()
	inputBySrc := a.inputBySrc
	inputByUniv := a.inputByUniv
	a.inputBySrc = map[string]uint64{}
	a.inputByUniv = map[string]uint64{}
	a.inputMu.Unlock()

	if len(inputBySrc) > 0 {
		log.Printf("[stats] input by source (last 10s):")
		for src, count := range inputBySrc {
			log.Printf("[stats]   %s: %d packets", src, count)
		}
	}
	if len(inputByUniv) > 0 {
		log.Printf("[stats] input by universe (last 10s):")
		for univ, count := range inputByUniv {
			log.Printf("[stats]   %s: %d packets", univ, count)
		}
	}

	if len(a.cfg.Mappings) == 0 {
		return
	}
	counts := a.engine.SwapStats()
	log.Printf("[stats] mapping traffic (last 10s):")
	for _, m := range a.cfg.Mappings {
		log.Printf("[stats]   %s -> %s: %d packets", m.From, m.To, counts[m.From.Universe])
	}
}

func init() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
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

func protocolPort(p config.Protocol) int {
	if p == config.ProtocolSACN {
		return sacn.Port
	}
	return artnet.Port
}

func parseTargetAddr(s string, defaultPort int) (*net.UDPAddr, error) {
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
		port = defaultPort
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", host)
	}

	return &net.UDPAddr{IP: ip, Port: port}, nil
}

// detectLocalInterface returns local IP and MAC for an interface matching the broadcast address
func detectLocalInterface(broadcast net.IP) (net.IP, net.HardwareAddr) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}

			mask := ipnet.Mask
			if len(mask) != 4 {
				continue
			}

			bcast := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				bcast[i] = ip4[i] | ^mask[i]
			}

			if bcast.Equal(broadcast) {
				return ip4, iface.HardwareAddr
			}
		}
	}
	return nil, nil
}

// detectBroadcastAddrs returns broadcast addresses for all network interfaces
func detectBroadcastAddrs() []*net.UDPAddr {
	var addrs []*net.UDPAddr
	seen := make(map[string]bool)

	ifaces, err := net.Interfaces()
	if err != nil {
		return addrs
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
