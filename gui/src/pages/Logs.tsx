import { useState } from 'react';
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
  Button,
} from '@mui/material';
import { 
  Pause as PauseIcon,
  PlayArrow as PlayIcon,
  Delete as DeleteIcon,
} from '@mui/icons-material';
import { useCoreStore } from '@/store/coreStore';
import type { AnyCoreEvent } from '@/types/core';

const eventTypeLabels: Record<string, string> = {
  'core.stateChanged': '状态变更',
  'session.established': '会话建立',
  'session.rotating': '会话轮换',
  'session.closed': '会话关闭',
  'stream.opened': '连接建立',
  'stream.closed': '连接关闭',
  'stream.error': '连接错误',
  'core.error': '核心错误',
  'metrics.snapshot': '指标',
  'rotation.scheduled': '轮换计划',
};

const getEventColor = (type: string) => {
  if (type.includes('error')) return 'error';
  if (type.includes('closed')) return 'default';
  if (type.includes('established') || type.includes('opened')) return 'success';
  return 'info';
};

const formatEventMessage = (event: AnyCoreEvent): string => {
  switch (event.type) {
    case 'core.stateChanged':
      return `${event.from} → ${event.to}`;
    case 'session.established':
      return `Session ${event.sessionId.slice(-4)} 已建立`;
    case 'session.closed':
      return `Session ${event.sessionId.slice(-4)} 已关闭 (${event.reason || 'unknown'})`;
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

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

export default function Logs() {
  const [isPaused, setIsPaused] = useState(false);
  const [filter, setFilter] = useState<string>('all');
  const { events, clearEvents } = useCoreStore();

  const filteredEvents = events
    .filter(e => filter === 'all' || e.type.includes(filter))
    .slice(-200)
    .reverse();

  return (
    <Box sx={{ p: 3, height: 'calc(100vh - 64px)', display: 'flex', flexDirection: 'column' }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
        <Typography variant="h5" sx={{ fontWeight: 600 }}>
          事件日志
        </Typography>
        
        <Box sx={{ display: 'flex', gap: 1 }}>
          <FormControl size="small" sx={{ minWidth: 120 }}>
            <InputLabel>筛选</InputLabel>
            <Select
              value={filter}
              label="筛选"
              onChange={(e) => setFilter(e.target.value)}
            >
              <MenuItem value="all">全部</MenuItem>
              <MenuItem value="session">会话</MenuItem>
              <MenuItem value="stream">连接</MenuItem>
              <MenuItem value="error">错误</MenuItem>
            </Select>
          </FormControl>
          
          <IconButton onClick={() => setIsPaused(!isPaused)}>
            {isPaused ? <PlayIcon /> : <PauseIcon />}
          </IconButton>
          
          <IconButton onClick={clearEvents}>
            <DeleteIcon />
          </IconButton>
        </Box>
      </Box>

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
                  py: 0.5,
                  borderBottom: '1px solid',
                  borderColor: 'divider',
                }}
              >
                <Typography
                  variant="caption"
                  sx={{ minWidth: 60, fontFamily: 'monospace', color: 'text.secondary' }}
                >
                  {new Date(event.timestamp).toLocaleTimeString('zh-CN')}
                </Typography>
                
                <Chip
                  label={eventTypeLabels[event.type] || event.type}
                  color={getEventColor(event.type) as any}
                  size="small"
                  sx={{ minWidth: 100, justifyContent: 'center' }}
                />
                
                <Typography variant="body2" sx={{ flex: 1 }}>
                  {formatEventMessage(event)}
                </Typography>
              </ListItem>
            ))}
          </List>
        </CardContent>
      </Card>
    </Box>
  );
}
