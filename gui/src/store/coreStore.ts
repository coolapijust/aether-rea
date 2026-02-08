import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import { immer } from 'zustand/middleware/immer';
import { api } from '@/lib/api';
import { EventStream } from '@/lib/websocket';
import type {
  CoreState,
  AnyCoreEvent,
  StreamInfo,
  CoreConfig,
  AppLogEvent,
} from '@/types/core';

interface CoreStore {
  // Connection
  connectionState: 'disconnected' | 'connecting' | 'connected';
  coreState: CoreState;
  lastError?: { code: string; message: string; fatal: boolean };

  // Session
  currentSession?: {
    id: string;
    establishedAt: number;
    localAddr: string;
    remoteAddr: string;
    uptime: number;
  };
  nextRotation?: number;

  // Streams
  streams: Map<string, StreamInfo>;
  activeStreamCount: number;

  // Proxy
  systemProxyEnabled: boolean;

  // Logs
  logs: AppLogEvent[];
  maxLogs: number;

  // Events
  events: AnyCoreEvent[];
  maxEvents: number;

  // Config
  currentConfig: CoreConfig;
  editingConfig: CoreConfig;
  hasUnsavedChanges: boolean;

  // Metrics for charts
  metricsHistory: {
    timestamp: number;
    upload: number;
    download: number;
    activeStreams: number;
    latencyMs?: number;
  }[];

  // Traffic stats
  totalUpload: number;
  totalDownload: number;

  // API Actions
  connect: () => void;
  disconnect: () => void;
  fetchStatus: () => Promise<void>;
  fetchConfig: () => Promise<void>;
  applyConfig: () => Promise<void>;
  startCore: () => Promise<void>;
  stopCore: () => Promise<void>;
  rotateSession: () => Promise<void>;
  closeStream: (streamId: string) => void;
  toggleSystemProxy: (enabled: boolean) => Promise<void>;

  // i18n
  language: 'zh' | 'en';
  setLanguage: (lang: 'zh' | 'en') => void;

  // Event handling
  applyEvent: (event: AnyCoreEvent) => void;
  updateEditingConfig: (config: Partial<CoreConfig>) => void;
  clearEvents: () => void;
  clearLogs: () => void;
}

const defaultConfig: CoreConfig = {
  url: '',
  psk: '',
  listen_addr: '127.0.0.1:1080',
  http_proxy_addr: '127.0.0.1:1081',
  max_padding: 128,
  rotation: {
    enabled: true,
    min_interval_ms: 5 * 60 * 1000,
    max_interval_ms: 10 * 60 * 1000,
    pre_warm_ms: 10 * 1000,
  },
  bypass_cn: true,
  block_ads: true,
};

let eventStream: EventStream | null = null;

export const useCoreStore = create<CoreStore>()(
  subscribeWithSelector(
    immer((set, get) => ({
      connectionState: 'disconnected',
      coreState: 'Idle',
      streams: new Map(),
      activeStreamCount: 0,
      events: [],
      maxEvents: 1000,
      currentConfig: defaultConfig,
      editingConfig: defaultConfig,
      hasUnsavedChanges: false,
      metricsHistory: [],
      totalUpload: 0,
      totalDownload: 0,
      systemProxyEnabled: false,
      logs: [],
      maxLogs: 500,
      language: 'en',
      setLanguage: (lang) => set({ language: lang }),

      connect: () => {
        if (eventStream) return;

        set({ connectionState: 'connecting' });

        eventStream = new EventStream(
          (event) => get().applyEvent(event),
          () => {
            set({ connectionState: 'connected' });
            get().fetchStatus();
            get().fetchConfig();
          },
          () => set({ connectionState: 'disconnected' })
        );

        eventStream.connect();
      },

      disconnect: () => {
        eventStream?.disconnect();
        eventStream = null;
        set({ connectionState: 'disconnected' });
      },

      fetchStatus: async () => {
        try {
          const status = await api.getStatus();
          set({
            coreState: status.state,
            systemProxyEnabled: status.proxy_enabled,
            activeStreamCount: status.active_streams,
            lastError: status.last_error ? {
              code: 'CORE_ERROR',
              message: status.last_error,
              fatal: false
            } : undefined,
          });
        } catch (err) {
          console.error('Failed to fetch status:', err);
        }
      },

      fetchConfig: async () => {
        try {
          const config = await api.getConfig();
          set({
            currentConfig: config,
            editingConfig: config,
            hasUnsavedChanges: false,
          });
        } catch (err) {
          console.error('Failed to fetch config:', err);
        }
      },

      applyConfig: async () => {
        const { editingConfig } = get();
        try {
          await api.updateConfig(editingConfig);
          set({
            currentConfig: editingConfig,
            hasUnsavedChanges: false,
          });
        } catch (err) {
          console.error('Failed to apply config:', err);
          throw err;
        }
      },

      startCore: async () => {
        try {
          await api.start();
        } catch (err) {
          console.error('Failed to start core:', err);
          throw err;
        }
      },

      stopCore: async () => {
        try {
          await api.stop();
        } catch (err) {
          console.error('Failed to stop core:', err);
          throw err;
        }
      },

      rotateSession: async () => {
        try {
          await api.rotate();
        } catch (err) {
          console.error('Failed to rotate session:', err);
          throw err;
        }
      },

      closeStream: (streamId) => {
        const state = get();
        const newStreams = new Map(state.streams);
        const stream = newStreams.get(streamId);
        if (stream) {
          stream.state = 'closing';
          newStreams.set(streamId, stream);
          set({ streams: newStreams });
        }
        // TODO: Call API to actually close stream
      },

      toggleSystemProxy: async (enabled) => {
        try {
          await api.setSystemProxy(enabled);
          set({ systemProxyEnabled: enabled });
        } catch (err) {
          console.error('Failed to toggle system proxy:', err);
          throw err;
        }
      },

      applyEvent: (event) => {
        const state = get();

        // Add to events (filter out high-frequency noise like metrics)
        if (event.type !== 'metrics.snapshot') {
          const newEvents = [...state.events, event];
          if (newEvents.length > state.maxEvents) {
            newEvents.shift();
          }
          set({ events: newEvents });
        }

        // Process event
        switch (event.type) {
          case 'core.stateChanged':
            set({ coreState: event.to });
            if (event.to === 'Active') {
              set({ lastError: undefined });
            }
            break;

          case 'session.established':
            set({
              currentSession: {
                id: event.sessionId,
                establishedAt: event.timestamp,
                localAddr: event.localAddr,
                remoteAddr: event.remoteAddr,
                uptime: 0,
              },
            });
            break;

          case 'session.closed':
            if (state.currentSession?.id === event.sessionId) {
              set({ currentSession: undefined });
            }
            break;

          case 'stream.opened': {
            const newStreams = new Map(state.streams);
            newStreams.set(event.streamId, {
              id: event.streamId,
              targetHost: event.target.host,
              targetPort: event.target.port,
              openedAt: event.timestamp,
              state: 'active',
              bytesSent: 0,
              bytesReceived: 0,
            });
            set({
              streams: newStreams,
              activeStreamCount: state.activeStreamCount + 1
            });
            break;
          }

          case 'stream.closed': {
            const newStreams = new Map(state.streams);
            const stream = newStreams.get(event.streamId);
            if (stream) {
              stream.state = 'closed';
              stream.bytesSent = event.bytesSent;
              stream.bytesReceived = event.bytesReceived;
              newStreams.set(event.streamId, stream);
            }
            set({
              streams: newStreams,
              activeStreamCount: Math.max(0, state.activeStreamCount - 1),
              totalUpload: state.totalUpload + event.bytesSent,
              totalDownload: state.totalDownload + event.bytesReceived,
            });
            break;
          }

          case 'core.error':
            set({
              lastError: {
                code: event.code,
                message: event.message,
                fatal: event.fatal,
              },
            });
            break;

          case 'metrics.snapshot': {
            const newHistory = [...state.metricsHistory, {
              timestamp: event.timestamp,
              upload: event.bytesSent,
              download: event.bytesReceived,
              activeStreams: event.activeStreams,
              latencyMs: event.latencyMs,
            }];
            if (newHistory.length > 100) {
              newHistory.shift();
            }
            set({
              metricsHistory: newHistory,
              totalUpload: event.bytesSent,
              totalDownload: event.bytesReceived,
            });

            if (state.currentSession) {
              set({
                currentSession: {
                  ...state.currentSession,
                  uptime: event.sessionUptime,
                },
              });
            }
            break;
          }

          case 'rotation.scheduled':
            set({ nextRotation: event.nextRotation });
            break;

          case 'app.log': {
            const newLogs = [...get().logs, event];
            if (newLogs.length > get().maxLogs) {
              newLogs.shift();
            }
            set({ logs: newLogs });
            break;
          }
        }
      },

      updateEditingConfig: (config) => set((state) => ({
        editingConfig: { ...state.editingConfig, ...config },
        hasUnsavedChanges: true,
      })),

      clearEvents: () => set({ events: [] }),
      clearLogs: () => set({ logs: [] }),
    }))
  )
);
