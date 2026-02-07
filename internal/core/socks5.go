package core

import (
	"context"
	"fmt"
	"io"
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
			
			// Rule matching
			target := TargetAddress{Host: host, Port: int(port)}
			var action ActionType = ActionProxy
			var ruleID string
			
			if s.core.ruleEngine != nil {
				req := &MatchRequest{
					Domain: host,
					Port:   int(port),
				}
				
				// Optional: Resolve IP if needed for IP matching
				if ip := net.ParseIP(host); ip != nil {
					req.IP = ip
				}
				
				res, err := s.core.ruleEngine.Match(req)
				if err == nil {
					action = res.Action
					ruleID = res.RuleID
				}
			}
			
			switch action {
			case ActionDirect:
				return net.Dial(network, addr)
				
			case ActionBlock, ActionReject:
				return nil, fmt.Errorf("blocked by rule: %s", ruleID)
				
			case ActionProxy:
				fallthrough
			default:
				// Open stream through Core
				handle, err := s.core.OpenStream(target, nil)
				if err != nil {
					s.core.emit(NewCoreErrorEvent(ErrTargetConnect, err.Error(), false))
					return nil, err
				}
				
				return &streamConn{
					handle:  handle,
					core:    s.core,
					local:   dummyAddr("socks-local"),
					remote:  dummyAddr(fmt.Sprintf("%s:%d", host, port)),
				}, nil
			}
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
	reader *RecordReader
	closed bool
}

func (c *streamConn) Read(p []byte) (int, error) {
	if c.reader == nil {
		stream, ok := c.core.GetUnderlyingStream(c.handle)
		if !ok {
			return 0, fmt.Errorf("stream not found")
		}
		c.reader = NewRecordReader(stream)
	}
	
	for {
		record, err := c.reader.ReadNextRecord()
		if err != nil {
			return 0, err
		}
		
		if record.Type == TypeError {
			return 0, fmt.Errorf("server error: %s", record.ErrorMessage)
		}
		
		if record.Type == TypeData {
			n := copy(p, record.Payload)
			if c.core.metrics != nil {
				c.core.metrics.RecordBytesReceived(uint64(n))
			}
			return n, nil
		}
		// Ignore other types for now
	}
}

func (c *streamConn) Write(p []byte) (int, error) {
	stream, ok := c.core.GetUnderlyingStream(c.handle)
	if !ok {
		return 0, fmt.Errorf("stream not found")
	}

	maxPadding := uint16(0)
	if c.core.config != nil {
		maxPadding = uint16(c.core.config.MaxPadding)
	}

	record, err := BuildDataRecord(p, maxPadding)
	if err != nil {
		return 0, err
	}
	
	n, err := stream.Write(record)
	if err != nil {
		return 0, err
	}
	
	if c.core.metrics != nil {
		c.core.metrics.RecordBytesSent(uint64(len(p)))
	}

	// Correctly return the number of bytes from the original payload
	if n > 0 {
		return len(p), nil
	}
	return 0, io.EOF
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
