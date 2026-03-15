package state

import (
	"sync"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type BackendInfo struct {
	Service   string
	Port      int
	Addresses []string
}

type RouteInfo struct {
	PathMatchType gatewayv1.PathMatchType
	PathValue     string
	Backend       BackendInfo
}

type Router struct {
	mu     sync.RWMutex
	Routes map[string][]RouteInfo
}

func NewRouter() *Router {
	return &Router{
		Routes: make(map[string][]RouteInfo),
	}
}

func (r *Router) UpdateRoutes(host string, routes []RouteInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(routes) == 0 {
		delete(r.Routes, host)
		return
	}

	r.Routes[host] = routes
}

func (r *Router) GetRoutesForHost(host string) ([]RouteInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	routes, ok := r.Routes[host]
	if !ok {
		return nil, false
	}

	routesCopy := make([]RouteInfo, len(routes))
	copy(routesCopy, routes)

	return routesCopy, true
}

func (r *Router) DeleteRoute(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Routes, key)
}
