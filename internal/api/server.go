// Package api provides HTTP API and WebSocket event stream for GUI clients.
package api

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"aether-rea/internal/core"
	"github.com/gorilla/websocket"
)

// Server provides HTTP API and WebSocket event stream
type Server struct {
	core       *core.Core
	addr       string
	listener   net.Listener
	server     *http.Server
	upgrader   websocket.Upgrader
	
	// Event broadcasting
	eventSubs  map[string]*eventSubscriber
	subMu      sync.RWMutex
	
	// Control channels
	ctx        context.Context
	cancel     context.CancelFunc
}

type eventSubscriber struct {
	id       string
	conn     *websocket.Conn
	sendCh   chan []byte
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewServer creates a new API server
func NewServer(core *core.Core, addr string) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Server{
		core:      core,
		addr:      addr,
		eventSubs: make(map[string]*eventSubscriber),
		ctx:       ctx,
		cancel:    cancel,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// Allow all origins for local development
				// In production, restrict to localhost
				return true
			},
		},
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()
	
	// REST API endpoints
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/config", s.handleConfig)
	mux.HandleFunc("/api/v1/rules", s.handleRules)
	mux.HandleFunc("/api/v1/streams", s.handleStreams)
	mux.HandleFunc("/api/v1/metrics", s.handleMetrics)
	mux.HandleFunc("/api/v1/control/start", s.handleStart)
	mux.HandleFunc("/api/v1/control/stop", s.handleStop)
	mux.HandleFunc("/api/v1/control/rotate", s.handleRotate)
	
	// WebSocket endpoint for events
	mux.HandleFunc("/api/v1/events", s.handleEvents)
	
	// Static files (for embedded GUI)
	// Serve embedded GUI files from embedded filesystem
	mux.Handle("/", http.FileServer(http.FS(s.staticFS)))
	
	s.server = &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}
	
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	s.listener = listener
	
	// Start event forwarding from Core
	go s.forwardEvents()
	
	log.Printf("API server listening on %s", s.addr)
	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()
	
	return nil
}

// Stop gracefully shuts down the server
func (s *Server) Stop() error {
	s.cancel()
	
	// Close all WebSocket connections
	s.subMu.Lock()
	for _, sub := range s.eventSubs {
		sub.cancel()
		sub.conn.Close()
	}
	s.subMu.Unlock()
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	return s.server.Shutdown(ctx)
}

// Addr returns the actual listening address
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.addr
}

// handleStatus returns current Core status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	state := s.core.GetState()
	config := s.core.GetActiveConfig()
	
	status := struct {
		State       string           `json:"state"`
		Config      *core.SessionConfig `json:"config"`
		Uptime      int64            `json:"uptime_ms,omitempty"`
		StreamCount int              `json:"active_streams"`
	}{
		State:       state,
		Config:      config,
		StreamCount: 0, // TODO: get from core
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleConfig handles config GET/POST
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		config := s.core.GetActiveConfig()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config)
		
	case http.MethodPost:
		var config core.SessionConfig
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		
		// Update core config
		if err := s.core.UpdateConfig(config); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
		
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleRules handles rule configuration
func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rules := s.core.GetRules()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rules)
		
	case http.MethodPost:
		var rules []core.Rule
		if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		
		if err := s.core.UpdateRules(rules); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
		
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleStreams returns active streams
func (s *Server) handleStreams(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	streams := s.core.GetStreams()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(streams)
}

// handleMetrics returns metrics data
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	metrics := s.core.GetMetrics()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// handleStart starts the Core
func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	config := s.core.GetActiveConfig()
	if err := s.core.Start(config); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

// handleStop stops the Core
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	if err := s.core.Close(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

// handleRotate triggers session rotation
func (s *Server) handleRotate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	if err := s.core.Rotate(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "rotating"})
}

// handleEvents handles WebSocket connections for event streaming
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	
	ctx, cancel := context.WithCancel(s.ctx)
	sub := &eventSubscriber{
		id:     generateSubscriberID(),
		conn:   conn,
		sendCh: make(chan []byte, 100),
		ctx:    ctx,
		cancel: cancel,
	}
	
	s.subMu.Lock()
	s.eventSubs[sub.id] = sub
	s.subMu.Unlock()
	
	// Cleanup on disconnect
	defer func() {
		s.subMu.Lock()
		delete(s.eventSubs, sub.id)
		s.subMu.Unlock()
		cancel()
		conn.Close()
	}()
	
	// Start goroutines for reading and writing
	go s.writeEvents(sub)
	s.readEvents(sub)
}

// writeEvents writes events to the WebSocket
func (s *Server) writeEvents(sub *eventSubscriber) {
	defer sub.cancel()
	
	for {
		select {
		case msg := <-sub.sendCh:
			if err := sub.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-sub.ctx.Done():
			return
		}
	}
}

// readEvents reads commands from the WebSocket
func (s *Server) readEvents(sub *eventSubscriber) {
	defer sub.cancel()
	
	for {
		_, msg, err := sub.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			return
		}
		
		// Handle client commands (subscribe, unsubscribe, etc.)
		var cmd struct {
			Action string   `json:"action"`
			Events []string `json:"events"`
		}
		if err := json.Unmarshal(msg, &cmd); err != nil {
			continue
		}
		
		switch cmd.Action {
		case "ping":
			// Send pong
			select {
			case sub.sendCh <- []byte(`{"type":"pong"}`):
			case <-sub.ctx.Done():
				return
			}
		}
	}
}

// forwardEvents forwards Core events to all WebSocket subscribers
func (s *Server) forwardEvents() {
	// Subscribe to Core events
	coreSub := s.core.Subscribe(func(event core.Event) {
		data, err := json.Marshal(event)
		if err != nil {
			return
		}
		
		// Broadcast to all subscribers
		s.subMu.RLock()
		subs := make([]*eventSubscriber, 0, len(s.eventSubs))
		for _, sub := range s.eventSubs {
			subs = append(subs, sub)
		}
		s.subMu.RUnlock()
		
		for _, sub := range subs {
			select {
			case sub.sendCh <- data:
			case <-sub.ctx.Done():
			case <-time.After(100 * time.Millisecond):
				// Drop event if subscriber is slow
			}
		}
	})
	defer coreSub.Cancel()
	
	<-s.ctx.Done()
}

func generateSubscriberID() string {
	return fmt.Sprintf("sub-%d", time.Now().UnixNano())
}
