package core

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/armon/go-socks5"
)

// socks5Server wraps the SOCKS5 server for Core integration.
type socks5Server struct {
	addr     string
	core     *Core
	server   *socks5.Server
	listener net.Listener
	cancel   context.CancelFunc
}

// newSocks5Server creates a SOCKS5 server.
func newSocks5Server(addr string, core *Core) *socks5Server {
	return &socks5Server{
		addr: addr,
		core: core,
	}
}

// start starts the SOCKS5 server.
func (s *socks5Server) start() error {
	conf := &socks5.Config{
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, portStr, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			
			port, err := parsePort(portStr)
			if err != nil {
				return nil, err
			}
			
			// Open stream through Core
			target := TargetAddress{Host: host, Port: int(port)}
			handle, err := s.core.OpenStream(target, nil)
			if err != nil {
				s.core.emit(NewCoreErrorEvent(ErrTargetConnect, err.Error(), false))
				return nil, err
			}
			
			// Get stream from registry
			conn := &streamConn{
				handle:  handle,
				core:    s.core,
				local:   dummyAddr("socks-local"),
				remote:  dummyAddr(fmt.Sprintf("%s:%d", host, port)),
			}
			
			return conn, nil
		},
	}

	server, err := socks5.New(conf)
	if err != nil {
		return err
	}

	s.server = server

	// Start listening in background
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.addr, err)
	}
	s.listener = listener

	go func() {
		if err := s.server.Serve(listener); err != nil {
			// Log error but don't crash
			select {
			case <-ctx.Done():
				// Expected shutdown
			default:
				s.core.emit(NewCoreErrorEvent(ErrNetwork, err.Error(), false))
			}
		}
	}()

	return nil
}

// stop stops the SOCKS5 server.
func (s *socks5Server) stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// parsePort parses a port string to uint16.
func parsePort(portStr string) (uint16, error) {
	port, err := net.LookupPort("tcp", portStr)
	if err == nil {
		return uint16(port), nil
	}
	
	var value uint64
	_, err = fmt.Sscanf(portStr, "%d", &value)
	if err != nil || value > 65535 {
		return 0, fmt.Errorf("invalid port: %s", portStr)
	}
	return uint16(value), nil
}

// streamConn wraps a Core stream as net.Conn.
type streamConn struct {
	handle StreamHandle
	core   *Core
	local  net.Addr
	remote net.Addr
	reader *recordReader
	closed bool
}

func (c *streamConn) Read(p []byte) (int, error) {
	// TODO: Implement reading from stream through Core
	return 0, fmt.Errorf("not implemented")
}

func (c *streamConn) Write(p []byte) (int, error) {
	// TODO: Implement writing to stream through Core
	return len(p), nil
}

func (c *streamConn) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	return c.core.CloseStream(c.handle)
}

func (c *streamConn) LocalAddr() net.Addr  { return c.local }
func (c *streamConn) RemoteAddr() net.Addr { return c.remote }
func (c *streamConn) SetDeadline(t time.Time) error       { return nil }
func (c *streamConn) SetReadDeadline(t time.Time) error   { return nil }
func (c *streamConn) SetWriteDeadline(t time.Time) error  { return nil }

type dummyAddr string

func (d dummyAddr) Network() string { return string(d) }
func (d dummyAddr) String() string  { return string(d) }
