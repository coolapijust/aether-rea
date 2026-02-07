package core

import (
	"time"
)

// Event is the base interface for all Core events.
// GUI/CLI receive events through subscription, never poll.
type Event interface {
	EventType() string
	EventTime() int64
}

// baseEvent provides common event fields.
type baseEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"`
}

func (e baseEvent) EventType() string { return e.Type }
func (e baseEvent) EventTime() int64  { return e.Timestamp }

// Event: core.stateChanged
// Fires when the Core FSM transitions between states.
type StateChangedEvent struct {
	baseEvent
	From CoreState `json:"from"`
	To   CoreState `json:"to"`
}

func NewStateChangedEvent(from, to CoreState) Event {
	return StateChangedEvent{
		baseEvent: baseEvent{Type: "core.stateChanged", Timestamp: time.Now().UnixMilli()},
		From:      from,
		To:        to,
	}
}

// Event: session.established
// Fires when WebTransport session is fully established.
type SessionEstablishedEvent struct {
	baseEvent
	SessionID  string `json:"sessionId"`
	LocalAddr  string `json:"localAddr"`
	RemoteAddr string `json:"remoteAddr"`
}

func NewSessionEstablishedEvent(id, local, remote string) Event {
	return SessionEstablishedEvent{
		baseEvent:  baseEvent{Type: "session.established", Timestamp: time.Now().UnixMilli()},
		SessionID:  id,
		LocalAddr:  local,
		RemoteAddr: remote,
	}
}

// Event: session.rotating
// Fires when session rotation starts.
type SessionRotatingEvent struct {
	baseEvent
	OldSessionID string `json:"oldSessionId"`
}

func NewSessionRotatingEvent(oldID string) Event {
	return SessionRotatingEvent{
		baseEvent:    baseEvent{Type: "session.rotating", Timestamp: time.Now().UnixMilli()},
		OldSessionID: oldID,
	}
}

// Event: session.closed
// Fires when session is fully closed.
type SessionClosedEvent struct {
	baseEvent
	SessionID string  `json:"sessionId"`
	Reason    *string `json:"reason,omitempty"` // "user" | "rotation" | "error"
	ErrorCode *string `json:"errorCode,omitempty"`
}

func NewSessionClosedEvent(id string, reason, errCode *string) Event {
	return SessionClosedEvent{
		baseEvent: baseEvent{Type: "session.closed", Timestamp: time.Now().UnixMilli()},
		SessionID: id,
		Reason:    reason,
		ErrorCode: errCode,
	}
}

// Event: stream.opened
// Fires when a new stream is opened to a target.
type StreamOpenedEvent struct {
	baseEvent
	StreamID string         `json:"streamId"`
	Target   TargetAddress  `json:"target"`
}

func NewStreamOpenedEvent(id string, target TargetAddress) Event {
	return StreamOpenedEvent{
		baseEvent: baseEvent{Type: "stream.opened", Timestamp: time.Now().UnixMilli()},
		StreamID:  id,
		Target:    target,
	}
}

// Event: stream.closed
// Fires when a stream is closed.
type StreamClosedEvent struct {
	baseEvent
	StreamID      string `json:"streamId"`
	BytesSent     uint64 `json:"bytesSent"`
	BytesReceived uint64 `json:"bytesReceived"`
}

func NewStreamClosedEvent(id string, sent, received uint64) Event {
	return StreamClosedEvent{
		baseEvent:     baseEvent{Type: "stream.closed", Timestamp: time.Now().UnixMilli()},
		StreamID:      id,
		BytesSent:     sent,
		BytesReceived: received,
	}
}

// Event: stream.error
// Fires when a stream encounters an error.
type StreamErrorEvent struct {
	baseEvent
	StreamID string `json:"streamId"`
	Code     string `json:"code"`
}

func NewStreamErrorEvent(id, code string) Event {
	return StreamErrorEvent{
		baseEvent: baseEvent{Type: "stream.error", Timestamp: time.Now().UnixMilli()},
		StreamID:  id,
		Code:      code,
	}
}

// Event: core.error
// Fires when Core encounters a fatal or non-fatal error.
type CoreErrorEvent struct {
	baseEvent
	Code    string `json:"code"`    // ERR_* from protocol
	Message string `json:"message"`
	Fatal   bool   `json:"fatal"`
}

func NewCoreErrorEvent(code, message string, fatal bool) Event {
	return CoreErrorEvent{
		baseEvent: baseEvent{Type: "core.error", Timestamp: time.Now().UnixMilli()},
		Code:      code,
		Message:   message,
		Fatal:     fatal,
	}
}

// Event: metrics.snapshot
// Periodic metrics snapshot (not for polling, for display updates).
type MetricsSnapshotEvent struct {
	baseEvent
	SessionUptime   int64  `json:"sessionUptime"`   // milliseconds
	ActiveStreams   int    `json:"activeStreams"`
	TotalStreams    int64  `json:"totalStreams"`
	BytesSent       uint64 `json:"bytesSent"`
	BytesReceived   uint64 `json:"bytesReceived"`
	LatencyMs       *int64 `json:"latencyMs,omitempty"`
}

func NewMetricsSnapshotEvent(uptime int64, active, total int64, sent, recv uint64, latency *int64) Event {
	return MetricsSnapshotEvent{
		baseEvent:     baseEvent{Type: "metrics.snapshot", Timestamp: time.Now().UnixMilli()},
		SessionUptime: uptime,
		ActiveStreams: int(active),
		TotalStreams:  total,
		BytesSent:     sent,
		BytesReceived: recv,
		LatencyMs:     latency,
	}
}

// Event: rotation.scheduled
// Fires when next rotation is scheduled (for countdown display).
type RotationScheduledEvent struct {
	baseEvent
	NextRotation int64 `json:"nextRotation"` // Unix timestamp (milliseconds)
	MinInterval  int64 `json:"minInterval"`  // milliseconds
	MaxInterval  int64 `json:"maxInterval"`  // milliseconds
}

func NewRotationScheduledEvent(nextRotation time.Time, minInterval, maxInterval time.Duration) Event {
	return RotationScheduledEvent{
		baseEvent:    baseEvent{Type: "rotation.scheduled", Timestamp: time.Now().UnixMilli()},
		NextRotation: nextRotation.UnixMilli(),
		MinInterval:  minInterval.Milliseconds(),
		MaxInterval:  maxInterval.Milliseconds(),
	}
}

// Event: rotation.prewarm.started
// Fires when pre-warming of new session begins.
type RotationPreWarmStartedEvent struct {
	baseEvent
	NewSessionID string `json:"newSessionId"`
}

func NewRotationPreWarmStartedEvent(newSessionID string) Event {
	return RotationPreWarmStartedEvent{
		baseEvent:    baseEvent{Type: "rotation.prewarm.started", Timestamp: time.Now().UnixMilli()},
		NewSessionID: newSessionID,
	}
}

// Event: rotation.completed
// Fires when rotation to new session is complete.
type RotationCompletedEvent struct {
	baseEvent
	OldSessionID string `json:"oldSessionId"`
	NewSessionID string `json:"newSessionId"`
	DrainingTime int64  `json:"drainingTime"` // milliseconds, time before old session closes
}

func NewRotationCompletedEvent(oldID, newID string, drainingTime time.Duration) Event {
	return RotationCompletedEvent{
		baseEvent:    baseEvent{Type: "rotation.completed", Timestamp: time.Now().UnixMilli()},
		OldSessionID: oldID,
		NewSessionID: newID,
		DrainingTime: drainingTime.Milliseconds(),
	}
}

// Error codes from Aether-Realist Protocol V3 Section 7.2
const (
	ErrBadRecord      = "ERR_BAD_RECORD"
	ErrMetadataDecrypt = "ERR_METADATA_DECRYPT"
	ErrUnsupported    = "ERR_UNSUPPORTED"
	ErrTargetConnect  = "ERR_TARGET_CONNECT"
	ErrStreamAbort    = "ERR_STREAM_ABORT"
	ErrResourceLimit  = "ERR_RESOURCE_LIMIT"
	ErrTimeout        = "ERR_TIMEOUT"
	ErrNetwork        = "ERR_NETWORK" // Transport layer aggregation
)
