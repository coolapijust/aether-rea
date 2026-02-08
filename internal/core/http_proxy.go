package core

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
)

// HttpProxyServer wraps the HTTP proxy server.
type HttpProxyServer struct {
	addr     string
	core     *Core
	server   *http.Server
	listener net.Listener
}

// newHttpProxyServer creates a new HTTP proxy server.
func newHttpProxyServer(addr string, core *Core) *HttpProxyServer {
	return &HttpProxyServer{
		addr: addr,
		core: core,
	}
}

// Start starts the HTTP proxy server.
func (s *HttpProxyServer) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.addr, err)
	}
	s.listener = listener

	s.server = &http.Server{
		Handler: http.HandlerFunc(s.handleRequest),
	}

	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.core.emit(NewCoreErrorEvent(ErrNetwork, fmt.Sprintf("HTTP proxy error: %v", err), false))
		}
	}()

	return nil
}

// Stop stops the HTTP proxy server.
func (s *HttpProxyServer) Stop() error {
	if s.server != nil {
		return s.server.Shutdown(context.Background())
	}
	return nil
}

func (s *HttpProxyServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		s.handleConnect(w, r)
	} else {
		s.handleHTTP(w, r)
	}
}

// handleConnect handles HTTPS tunneling (CONNECT method).
func (s *HttpProxyServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	host, portStr, err := net.SplitHostPort(r.Host)
	if err != nil {
		// Try adding default port if missing, though CONNECT usually has it
		host = r.Host
		portStr = "443"
	}
	
	port, err := parsePort(portStr)
	if err != nil {
		http.Error(w, "Invalid port", http.StatusBadRequest)
		return
	}

	target := TargetAddress{Host: host, Port: int(port)}

	// Match rules
	action := ActionProxy
	if s.core.ruleEngine != nil {
		req := &MatchRequest{Domain: host, Port: int(port)}
		if ip := net.ParseIP(host); ip != nil {
			req.IP = ip
		}
		res, err := s.core.ruleEngine.Match(req)
		if err == nil {
			action = res.Action
		}
	}

	log.Printf("[HTTP-CONNECT] %s -> %s:%d (action=%s)", r.Host, target.Host, target.Port, action)

	if action == ActionBlock || action == ActionReject {
		http.Error(w, "Blocked by rule", http.StatusForbidden)
		return
	}

	// Connect to target
	var destConn io.ReadWriteCloser
	if action == ActionDirect {
		d, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, fmt.Sprintf("Dial failed: %v", err), http.StatusServiceUnavailable)
			return
		}
		destConn = d
	} else {
		handle, err := s.core.OpenStream(target, nil)
		if err != nil {
			http.Error(w, fmt.Sprintf("Upstream failed: %v", err), http.StatusBadGateway)
			return
		}
		
		// Wrap stream as net.Conn
		destConn = &streamConn{
			handle: handle,
			core:   s.core,
			local:  dummyAddr("http-local"),
			remote: dummyAddr(r.Host),
		}
	}
	defer destConn.Close()

	w.WriteHeader(http.StatusOK)
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer clientConn.Close()

	// Transfer data
	go io.Copy(destConn, clientConn)
	io.Copy(clientConn, destConn)
}

// handleHTTP handles plain HTTP requests.
func (s *HttpProxyServer) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if !r.URL.IsAbs() {
		http.Error(w, "Request must be absolute", http.StatusBadRequest)
		return
	}

	host := r.URL.Hostname()
	portStr := r.URL.Port()
	if portStr == "" {
		portStr = "80"
	}
	port, _ := parsePort(portStr)
	target := TargetAddress{Host: host, Port: int(port)}

	// Rule matching
	action := ActionProxy
	if s.core.ruleEngine != nil {
		req := &MatchRequest{Domain: host, Port: int(port)}
		if ip := net.ParseIP(host); ip != nil {
			req.IP = ip
		}
		res, err := s.core.ruleEngine.Match(req)
		if err == nil {
			action = res.Action
		}
	}

	log.Printf("[HTTP] %s -> %s (action=%s)", r.URL.String(), target, action)

	if action == ActionBlock || action == ActionReject {
		http.Error(w, "Blocked by rule", http.StatusForbidden)
		return
	}

	var transport http.RoundTripper

	if action == ActionDirect {
		transport = http.DefaultTransport
	} else {
		// Use custom transport carrying traffic over Streams
		transport = &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				handle, err := s.core.OpenStream(target, nil)
				if err != nil {
					return nil, err
				}
				return &streamConn{
					handle: handle,
					core:   s.core,
					local:  dummyAddr("http-local"),
					remote: dummyAddr(addr),
				}, nil
			},
		}
	}

	// Remove hop-by-hop headers
	r.RequestURI = ""
	resp, err := transport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
