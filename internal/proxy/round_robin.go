// internal/proxy/round_robin.go
package proxy

import (
	"sync"
	"sync/atomic"
)

type RoundRobinBalancer struct {
	counters sync.Map
}

func NewRoundRobinBalancer() *RoundRobinBalancer {
	return &RoundRobinBalancer{}
}

func (b *RoundRobinBalancer) GetNextIP(serviceName string, ips []string) string {
	if len(ips) == 0 {
		return ""
	}
	if len(ips) == 1 {
		return ips[0]
	}

	val, _ := b.counters.LoadOrStore(serviceName, new(uint64))
	counter := val.(*uint64)

	currentCount := atomic.AddUint64(counter, 1)

	index := currentCount % uint64(len(ips))

	return ips[index]
}
