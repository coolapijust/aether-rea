// HTTP API client for Core
import type {
  CoreConfig,
  CoreState,
  StreamInfo,
  MetricsSnapshotEvent
} from '@/types/core';

const API_BASE = 'http://localhost:9880/api/v1';

export class CoreAPI {
  private baseUrl: string;

  constructor(baseUrl: string = API_BASE) {
    this.baseUrl = baseUrl;
  }

  // Status
  async getStatus(): Promise<{
    state: CoreState;
    config?: CoreConfig;
    uptime?: number;
    active_streams: number;
    proxy_enabled: boolean;
  }> {
    const res = await fetch(`${this.baseUrl}/status`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.json();
  }

  // Config
  async getConfig(): Promise<CoreConfig> {
    const res = await fetch(`${this.baseUrl}/config`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.json();
  }

  async updateConfig(config: CoreConfig): Promise<void> {
    const res = await fetch(`${this.baseUrl}/config`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(config),
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
  }

  // Control
  async start(): Promise<void> {
    const res = await fetch(`${this.baseUrl}/control/start`, { method: 'POST' });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
  }

  async stop(): Promise<void> {
    const res = await fetch(`${this.baseUrl}/control/stop`, { method: 'POST' });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
  }

  async rotate(): Promise<void> {
    const res = await fetch(`${this.baseUrl}/control/rotate`, { method: 'POST' });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
  }

  // Streams
  async getStreams(): Promise<StreamInfo[]> {
    const res = await fetch(`${this.baseUrl}/streams`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.json();
  }

  // Metrics
  async getMetrics(): Promise<MetricsSnapshotEvent> {
    const res = await fetch(`${this.baseUrl}/metrics`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.json();
  }

  // Proxy
  async setSystemProxy(enabled: boolean): Promise<void> {
    const res = await fetch(`${this.baseUrl}/control/proxy`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled }),
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
  }
}

export const api = new CoreAPI();
