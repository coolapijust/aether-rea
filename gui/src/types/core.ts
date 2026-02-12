// Core API Types - 与后端 Core API 严格对齐

export type CoreState =
  | 'Idle'
  | 'Starting'
  | 'Active'
  | 'Rotating'
  | 'Closing'
  | 'Closed'
  | 'Error';

export type CoreEventType =
  | 'core.stateChanged'
  | 'session.established'
  | 'session.rotating'
  | 'session.closed'
  | 'stream.opened'
  | 'stream.closed'
  | 'stream.error'
  | 'core.error'
  | 'metrics.snapshot'
  | 'rotation.scheduled'
  | 'app.log';

export interface CoreEvent {
  type: CoreEventType;
  timestamp: number;
}

export interface StateChangedEvent extends CoreEvent {
  type: 'core.stateChanged';
  from: CoreState;
  to: CoreState;
}

export interface SessionEstablishedEvent extends CoreEvent {
  type: 'session.established';
  sessionId: string;
  localAddr: string;
  remoteAddr: string;
}

export interface SessionRotatingEvent extends CoreEvent {
  type: 'session.rotating';
  oldSessionId: string;
}

export interface SessionClosedEvent extends CoreEvent {
  type: 'session.closed';
  sessionId: string;
  reason?: 'user' | 'rotation' | 'error' | 'drained';
  errorCode?: string;
}

export interface StreamOpenedEvent extends CoreEvent {
  type: 'stream.opened';
  streamId: string;
  target: {
    host: string;
    port: number;
  };
}

export interface StreamClosedEvent extends CoreEvent {
  type: 'stream.closed';
  streamId: string;
  bytesSent: number;
  bytesReceived: number;
}

export interface CoreErrorEvent extends CoreEvent {
  type: 'core.error';
  code: string;
  message: string;
  fatal: boolean;
}

export interface MetricsSnapshotEvent extends CoreEvent {
  type: 'metrics.snapshot';
  sessionUptime: number;
  activeStreams: number;
  totalStreams: number;
  bytesSent: number;
  bytesReceived: number;
  latencyMs?: number;
}

export interface RotationScheduledEvent extends CoreEvent {
  type: 'rotation.scheduled';
  nextRotation: number;
  minInterval: number;
  maxInterval: number;
}

export interface AppLogEvent extends CoreEvent {
  type: 'app.log';
  level: 'info' | 'warn' | 'error';
  message: string;
  source?: string;
}

export type AnyCoreEvent =
  | StateChangedEvent
  | SessionEstablishedEvent
  | SessionRotatingEvent
  | SessionClosedEvent
  | StreamOpenedEvent
  | StreamClosedEvent
  | CoreErrorEvent
  | MetricsSnapshotEvent
  | RotationScheduledEvent
  | AppLogEvent;

export interface StreamInfo {
  id: string;
  targetHost: string;
  targetPort: number;
  openedAt: number;
  state: 'opening' | 'active' | 'closing' | 'closed';
  bytesSent: number;
  bytesReceived: number;
}

export interface CoreConfig {
  url: string;
  psk: string;
  listen_addr: string;
  http_proxy_addr: string;
  dial_addr?: string;
  max_padding: number;
  record_payload_bytes?: number;
  allow_insecure?: boolean;
  perf_capture_enabled?: boolean;
  perf_capture_on_connect?: boolean;
  perf_log_path?: string;
  rotation: {
    enabled: boolean;
    min_interval_ms: number;
    max_interval_ms: number;
    pre_warm_ms: number;
  };
  bypass_cn?: boolean;
  block_ads?: boolean;
  window_profile?: 'conservative' | 'normal' | 'aggressive';
  rules?: Rule[];
}

export interface Rule {
  id: string;
  name: string;
  priority: number;
  enabled: boolean;
  action: 'direct' | 'proxy' | 'block' | 'reject';
  matches: {
    type: string;
    value: string;
    not?: boolean;
  }[];
}

export interface NodeInfo {
  id: string;
  name: string;
  address: string;
  latency?: number;
  selected?: boolean;
}
