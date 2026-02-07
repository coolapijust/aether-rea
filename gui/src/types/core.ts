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
  listenAddr: string;
  dialAddr?: string;
  maxPadding: number;
  rotation: {
    enabled: boolean;
    minIntervalMs: number;
    maxIntervalMs: number;
    preWarmMs: number;
  };
  bypassCN?: boolean;
  blockAds?: boolean;
}

export interface NodeInfo {
  id: string;
  name: string;
  address: string;
  latency?: number;
  selected?: boolean;
}
