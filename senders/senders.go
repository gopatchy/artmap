package senders

import (
	"net"
	"sync"
	"time"

	"github.com/gopatchy/artmap/config"
)

type SenderInfo struct {
	Universe config.Universe `json:"universe"`
	IP       string          `json:"ip"`
}

type senderKey struct {
	protocol config.Protocol
	universe uint16
	ip       string
}

type UniverseSenders struct {
	mu      sync.Mutex
	entries map[senderKey]time.Time
}

func New() *UniverseSenders {
	return &UniverseSenders{
		entries: map[senderKey]time.Time{},
	}
}

func (s *UniverseSenders) Record(u config.Universe, ip net.IP) {
	key := senderKey{
		protocol: u.Protocol,
		universe: u.Number,
		ip:       ip.String(),
	}
	s.mu.Lock()
	s.entries[key] = time.Now()
	s.mu.Unlock()
}

func (s *UniverseSenders) Expire(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	s.mu.Lock()
	for k, t := range s.entries {
		if t.Before(cutoff) {
			delete(s.entries, k)
		}
	}
	s.mu.Unlock()
}

func (s *UniverseSenders) GetAll() []SenderInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]SenderInfo, 0, len(s.entries))
	for k := range s.entries {
		result = append(result, SenderInfo{
			Universe: config.Universe{Protocol: k.protocol, Number: k.universe},
			IP:       k.ip,
		})
	}
	return result
}
