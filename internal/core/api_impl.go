package core

import (
	"context"
	"fmt"
	"sync"
	"time"
	
	webtransport "github.com/quic-go/webtransport-go"
)

// initialize sets up internal components.
func (c *Core) initialize() error {
	c.metrics = NewMetrics()
	
	// Initialize session manager
	c.sessionMgr = newSessionManager(c.config, c.emit, c.metrics)
	if err := c.sessionMgr.initialize(); err != nil {
		return err
	}
	
	// Connect initial session
	if err := c.sessionMgr.connect(); err != nil {
		return err
	}
	
	// Initialize SOCKS5 server
	c.socksServer = newSocks5Server(c.config.ListenAddr, c)
	if err := c.socksServer.start(); err != nil {
		return err
	}
	
	// Start metrics collector (every 5 seconds)
	c.metricsCollector = NewMetricsCollector(c.metrics, 5*time.Second, c.emit)
	c.metricsCollector.Start()
	
	// Start event loop
	go c.runEventLoop()
	
	return nil
}

// cleanup releases all resources.
func (c *Core) cleanup() {
	if c.metricsCollector != nil {
		c.metricsCollector.Stop()
	}
	
	if c.socksServer != nil {
		c.socksServer.stop()
	}
	
	if c.sessionMgr != nil {
		c.sessionMgr.close("shutdown")
	}
}

// performRotation handles session rotation.
func (c *Core) performRotation() error {
	if c.sessionMgr == nil {
		return fmt.Errorf("session manager not initialized")
	}
	return c.sessionMgr.rotate()
}

// openStreamInternal creates a stream (protocol internal).
func (c *Core) openStreamInternal(target TargetAddress, options map[string]interface{}) (StreamHandle, error) {
	if c.sessionMgr == nil {
		return StreamHandle{}, fmt.Errorf("session manager not initialized")
	}
	
	ctx := context.Background()
	session, streamID, err := c.sessionMgr.getSession(ctx)
	if err != nil {
		return StreamHandle{}, err
	}
	
	stream, err := session.OpenStreamSync(ctx)
	if err != nil {
		c.emit(NewStreamErrorEvent(fmt.Sprintf("stream-%d", streamID), ErrStreamAbort))
		return StreamHandle{}, err
	}
	
	// Build metadata and send
	maxPadding := uint16(0)
	if c.config != nil {
		maxPadding = uint16(c.config.MaxPadding)
	}
	
	metadata, err := buildMetadataRecord(target.Host, uint16(target.Port), maxPadding, c.config.PSK, streamID)
	if err != nil {
		stream.Close()
		return StreamHandle{}, err
	}
	
	if _, err := stream.Write(metadata); err != nil {
		stream.Close()
		c.emit(NewStreamErrorEvent(fmt.Sprintf("stream-%d", streamID), ErrBadRecord))
		return StreamHandle{}, err
	}
	
	handle := StreamHandle{ID: fmt.Sprintf("stream-%d", streamID)}
	c.metrics.StreamOpened()
	c.emit(NewStreamOpenedEvent(handle.ID, target))
	
	// Store stream for later reference
	c.registerStream(handle.ID, stream, target)
	
	return handle, nil
}

// closeStreamInternal closes a stream.
func (c *Core) closeStreamInternal(handle StreamHandle) error {
	return c.unregisterStream(handle.ID)
}

// streamRegistry tracks active streams.
type streamRegistry struct {
	streams map[string]*streamEntry
	mu      sync.RWMutex
}

type streamEntry struct {
	stream webtransport.Stream
	target TargetAddress
}

var globalRegistry = &streamRegistry{
	streams: make(map[string]*streamEntry),
}

func (c *Core) registerStream(id string, stream webtransport.Stream, target TargetAddress) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.streams[id] = &streamEntry{stream: stream, target: target}
}

func (c *Core) unregisterStream(id string) error {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	
	entry, ok := globalRegistry.streams[id]
	if !ok {
		return fmt.Errorf("stream not found: %s", id)
	}
	
	err := entry.stream.Close()
	delete(globalRegistry.streams, id)
	
	c.metrics.StreamClosed()
	
	// Get stats for event
	sent := c.metrics.BytesSent()
	received := c.metrics.BytesReceived()
	c.emit(NewStreamClosedEvent(id, sent, received))
	
	return err
}
