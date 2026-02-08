import { useState, useRef, useEffect } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  List,
  ListItem,
  Chip,
  IconButton,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Tabs,
  Tab,
  Paper,
} from '@mui/material';
import {
  Pause as PauseIcon,
  PlayArrow as PlayIcon,
  Delete as DeleteIcon,
} from '@mui/icons-material';
import { useCoreStore } from '@/store/coreStore';
import { translations } from '@/lib/i18n';
import type { AnyCoreEvent } from '@/types/core';
import { formatBytes } from '@/utils/format';

const eventTypeLabels: Record<string, Record<string, string>> = {
  en: {
    'core.stateChanged': 'State Change',
    'session.established': 'Session Est.',
    'session.rotating': 'Rotating',
    'session.closed': 'Session Closed',
    'stream.opened': 'Stream Opened',
    'stream.closed': 'Stream Closed',
    'stream.error': 'Stream Error',
    'core.error': 'Core Error',
    'rotation.scheduled': 'Scheduled',
  },
  zh: {
    'core.stateChanged': '状态变更',
    'session.established': '会话建立',
    'session.rotating': '会话轮换',
    'session.closed': '会话关闭',
    'stream.opened': '连接建立',
    'stream.closed': '连接关闭',
    'stream.error': '连接错误',
    'core.error': '核心错误',
    'rotation.scheduled': '轮换计划',
  }
};

const getEventColor = (type: string) => {
  if (type.includes('error')) return 'error';
  if (type.includes('closed')) return 'default';
  if (type.includes('established') || type.includes('opened')) return 'success';
  return 'info';
};

const formatEventMessage = (event: AnyCoreEvent, lang: 'en' | 'zh'): string => {
  const isZh = lang === 'zh';
  switch (event.type) {
    case 'core.stateChanged':
      return `${event.from} → ${event.to}`;
    case 'session.established':
      return isZh ? `会话 ${event.sessionId.slice(-4)} 已建立` : `Session ${event.sessionId.slice(-4)} established`;
    case 'session.closed':
      return isZh ? `会话 ${event.sessionId.slice(-4)} 已关闭` : `Session ${event.sessionId.slice(-4)} closed`;
    case 'stream.opened':
      return `${event.target.host}:${event.target.port}`;
    case 'stream.closed':
      return `↑${formatBytes(event.bytesSent)} ↓${formatBytes(event.bytesReceived)}`;
    case 'core.error':
      return `${event.code}: ${event.message}`;
    default:
      return '';
  }
};

export default function Logs() {
  const [activeTab, setActiveTab] = useState(0);
  const [isPaused, setIsPaused] = useState(false);
  const [filter, setFilter] = useState<string>('all');
  const { events, logs, clearEvents, clearLogs, language } = useCoreStore();
  const t = translations[language];
  const scrollRef = useRef<HTMLDivElement>(null);

  // Auto scroll for System Logs (Tab 0)
  useEffect(() => {
    if (activeTab === 0 && scrollRef.current && !isPaused) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs, activeTab, isPaused]);

  const filteredEvents = events
    .filter(e => filter === 'all' || e.type.includes(filter))
    .slice(-200)
    .reverse();

  const getLogLevelColor = (level: string) => {
    switch (level.toLowerCase()) {
      case 'error': return '#ff4d4f';
      case 'warn': return '#faad14';
      default: return '#1890ff';
    }
  };

  return (
    <Box sx={{ p: 3, height: 'calc(100vh - 64px)', display: 'flex', flexDirection: 'column', gap: 2 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <Typography variant="h5" sx={{ fontWeight: 600 }}>
          {t.logs.title}
        </Typography>

        <Box sx={{ display: 'flex', gap: 1 }}>
          {activeTab === 1 && (
            <FormControl size="small" sx={{ minWidth: 120 }}>
              <InputLabel>{language === 'zh' ? '筛选' : 'Filter'}</InputLabel>
              <Select
                value={filter}
                label={language === 'zh' ? '筛选' : 'Filter'}
                onChange={(e) => setFilter(e.target.value)}
              >
                <MenuItem value="all">{language === 'zh' ? '全部事件' : 'All Events'}</MenuItem>
                <MenuItem value="session">{language === 'zh' ? '会话变更' : 'Sessions'}</MenuItem>
                <MenuItem value="stream">{language === 'zh' ? '连接详情' : 'Streams'}</MenuItem>
                <MenuItem value="error">{language === 'zh' ? '错误日志' : 'Errors'}</MenuItem>
              </Select>
            </FormControl>
          )}

          <IconButton size="small" onClick={() => setIsPaused(!isPaused)}>
            {isPaused ? <PlayIcon /> : <PauseIcon />}
          </IconButton>

          <IconButton size="small" onClick={activeTab === 0 ? clearLogs : clearEvents}>
            <DeleteIcon />
          </IconButton>
        </Box>
      </Box>

      <Box sx={{ borderBottom: 1, borderColor: 'divider' }}>
        <Tabs value={activeTab} onChange={(_, v) => setActiveTab(v)}>
          <Tab label={t.logs.tab_system} />
          <Tab label={t.logs.tab_events} />
        </Tabs>
      </Box>

      {/* Tab Panel Content */}
      <Box sx={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
        {activeTab === 0 ? (
          <Paper
            ref={scrollRef}
            elevation={0}
            sx={{
              flex: 1,
              bgcolor: 'rgba(0,0,0,0.05)',
              p: 2,
              overflowY: 'auto',
              fontFamily: "'JetBrains Mono', monospace",
              fontSize: '0.85rem',
            }}
          >
            {logs.length === 0 ? (
              <Box sx={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', opacity: 0.3 }}>
                <Typography variant="body2">{t.logs.placeholder}</Typography>
              </Box>
            ) : (
              logs.map((log, i) => (
                <Box key={i} sx={{ mb: 0.5, display: 'flex' }}>
                  <Typography component="span" sx={{ color: 'text.secondary', mr: 1, userSelect: 'none' }}>
                    [{new Date(log.timestamp).toLocaleTimeString([], { hour12: false })}]
                  </Typography>
                  <Typography component="span" sx={{ color: getLogLevelColor(log.level), fontWeight: 600, mr: 1, minWidth: 45 }}>
                    {log.level.toUpperCase()}
                  </Typography>
                  <Typography component="span" sx={{ flex: 1, wordBreak: 'break-all', opacity: 0.9 }}>
                    {log.message}
                  </Typography>
                </Box>
              ))
            )}
          </Paper>
        ) : (
          <Card sx={{ flex: 1, overflow: 'auto' }}>
            <CardContent sx={{ p: 0 }}>
              <List dense>
                {filteredEvents.map((event, index) => (
                  <ListItem
                    key={`${event.type}-${event.timestamp}-${index}`}
                    sx={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 2,
                      py: 0.8,
                      borderBottom: '1px solid',
                      borderColor: 'divider',
                    }}
                  >
                    <Typography variant="caption" sx={{ minWidth: 70, fontFamily: 'monospace', color: 'text.secondary' }}>
                      {new Date(event.timestamp).toLocaleTimeString(language === 'zh' ? 'zh-CN' : 'en-US')}
                    </Typography>
                    <Chip
                      label={eventTypeLabels[language][event.type] || event.type}
                      color={getEventColor(event.type) as any}
                      size="small"
                      sx={{ minWidth: 100 }}
                    />
                    <Typography variant="body2" sx={{ flex: 1, fontWeight: 500 }}>
                      {formatEventMessage(event, language)}
                    </Typography>
                  </ListItem>
                ))}
              </List>
            </CardContent>
          </Card>
        )}
      </Box>
    </Box>
  );
}
