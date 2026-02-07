import { create } from 'zustand';
import type { 
  CoreState, 
  AnyCoreEvent, 
  StreamInfo, 
  CoreConfig,
  MetricsSnapshotEvent 
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
  
  // Actions
  applyEvent: (event: AnyCoreEvent) => void;
  setConnectionState: (state: 'disconnected' | 'connecting' | 'connected') => void;
  updateEditingConfig: (config: Partial<CoreConfig>) => void;
  markChangesSaved: () => void;
  clearEvents: () => void;
  closeStream: (streamId: string) => void;
}

const defaultConfig: CoreConfig = {
  url: 'https://example.com/v1/api/sync',
  psk: '',
  listenAddr: '127.0.0.1:1080',
  maxPadding: 128,
  rotation: {
    enabled: true,
    minIntervalMs: 15 * 60 * 1000,
    maxIntervalMs: 40 * 60 * 1000,
    preWarmMs: 30 * 1000,
  },
  bypassCN: true,
  blockAds: false,
};

export const useCoreStore = create<CoreStore>((set, get) => ({
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

  applyEvent: (event) => {
    const state = get();
    
    // Add to events
    const newEvents = [...state.events, event];
    if (newEvents.length > state.maxEvents) {
      newEvents.shift();
    }
    
    set({ events: newEvents });
    
    // Process event
    switch (event.type) {
      case 'core.stateChanged':
        set({ coreState: event.to });
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
    }
  },

  setConnectionState: (connectionState) => set({ connectionState }),
  
  updateEditingConfig: (config) => set((state) => ({
    editingConfig: { ...state.editingConfig, ...config },
    hasUnsavedChanges: true,
  })),
  
  markChangesSaved: () => set((state) => ({
    currentConfig: { ...state.editingConfig },
    hasUnsavedChanges: false,
  })),
  
  clearEvents: () => set({ events: [] }),
  
  closeStream: (streamId) => {
    // TODO: Call Core API to close stream
    const state = get();
    const newStreams = new Map(state.streams);
    const stream = newStreams.get(streamId);
    if (stream) {
      stream.state = 'closing';
      newStreams.set(streamId, stream);
      set({ streams: newStreams });
    }
  },
}));
