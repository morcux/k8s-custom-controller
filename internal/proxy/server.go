package proxy

import (
	"context"
	"errors"
	"fmt"
	"gateway-proxy/internal/state"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

type Server struct {
	server   *http.Server
	router   *state.Router
	balancer *RoundRobinBalancer
}

func NewServer(addr string, router *state.Router) *Server {
	s := &Server{
		router:   router,
		balancer: NewRoundRobinBalancer(),
	}
	s.server = &http.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(s.handleRequest),
	}
	return s
}

func (s *Server) Start(ctx context.Context) error {
	go func() {
		log.Printf("Starting proxy server on %s", s.server.Addr)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("failed to start proxy server: %v", err)
		}
	}()

	<-ctx.Done()

	log.Println("Shutting down proxy server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.server.Shutdown(shutdownCtx)
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	routes, ok := s.router.GetRoutesForHost(r.Host)
	if !ok {
		http.Error(w, "Host Not Found", http.StatusNotFound)
		return
	}

	var bestMatch *state.RouteInfo
	longestPrefixLen := 0

	for i, route := range routes {
		if route.PathMatchType == "Exact" && route.PathValue == r.URL.Path {
			bestMatch = &routes[i]
			break
		}
		if route.PathMatchType == "PathPrefix" && strings.HasPrefix(r.URL.Path, route.PathValue) {
			if len(route.PathValue) > longestPrefixLen {
				longestPrefixLen = len(route.PathValue)
				bestMatch = &routes[i]
			}
		}
	}

	if bestMatch == nil {
		http.Error(w, "Route Not Found", http.StatusNotFound)
		return
	}

	backend := bestMatch.Backend
	if len(backend.Addresses) == 0 {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	targetIP := s.balancer.GetNextIP(backend.Service, backend.Addresses)
	targetURL, _ := url.Parse(fmt.Sprintf("http://%s:%d", targetIP, backend.Port))

	log.Printf("Forwarding request for %s%s to %s (matched prefix: %s)", r.Host, r.URL.Path, targetURL, bestMatch.PathValue)

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.ServeHTTP(w, r)
}
