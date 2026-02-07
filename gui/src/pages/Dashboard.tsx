import {
  Box,
  Card,
  CardContent,
  Typography,
  Button,
  Chip,
  Grid,
} from '@mui/material';
import {
  PowerSettingsNew as PowerIcon,
  Speed as SpeedIcon,
  Refresh as RefreshIcon,
} from '@mui/icons-material';
import {
  LineChart,
  Line,
  ResponsiveContainer,
  XAxis,
  YAxis,
  Area,
  AreaChart,
} from 'recharts';
import { useCoreStore } from '@/store/coreStore';
import { formatDuration, formatBytes } from '@/utils/format';
import LogPanel from '@/components/LogPanel';

export default function Dashboard() {
  const {
    connectionState,
    coreState,
    currentSession,
    metricsHistory,
    activeStreamCount,
    totalUpload,
    totalDownload,
    nextRotation,
    systemProxyEnabled,
    toggleSystemProxy,
  } = useCoreStore();

  const isConnected = connectionState === 'connected' && coreState === 'Active';

  // Prepare chart data
  const chartData = metricsHistory.map((m) => ({
    time: new Date(m.timestamp).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
    upload: m.upload / 1024 / 1024, // MB
    download: m.download / 1024 / 1024,
    latency: m.latencyMs || 0,
  }));

  const getStatusColor = () => {
    if (coreState === 'Active') return 'success';
    if (coreState === 'Error') return 'error';
    if (coreState === 'Starting' || coreState === 'Rotating') return 'warning';
    return 'default';
  };

  const getStatusText = () => {
    switch (coreState) {
      case 'Active': return '已连接';
      case 'Error': return '错误';
      case 'Starting': return '连接中';
      case 'Rotating': return '轮换中';
      case 'Idle': return '未连接';
      default: return coreState;
    }
  };

  return (
    <Box sx={{ p: 3 }}>
      <Typography variant="h5" sx={{ mb: 3, fontWeight: 600 }}>
        首页
      </Typography>

      <Grid container spacing={3}>
        {/* Status Card */}
        <Grid item xs={12} md={4}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="subtitle1" color="text.secondary" gutterBottom>
                连接状态
              </Typography>

              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                <Box
                  sx={{
                    width: 12,
                    height: 12,
                    borderRadius: '50%',
                    bgcolor: getStatusColor() === 'success' ? 'success.main' :
                      getStatusColor() === 'error' ? 'error.main' :
                        getStatusColor() === 'warning' ? 'warning.main' : 'text.disabled',
                    animation: isConnected ? 'pulse 2s infinite' : 'none',
                  }}
                />
                <Chip
                  label={getStatusText()}
                  color={getStatusColor() as any}
                  size="small"
                />
                {currentSession && (
                  <Typography variant="caption" color="text.secondary">
                    #{currentSession.id.slice(-4)}
                  </Typography>
                )}
              </Box>

              <Box sx={{ mt: 2 }}>
                <Typography variant="body2" color="text.secondary">
                  会话时长: {currentSession ? formatDuration(currentSession.uptime) : '-'}
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                  下次轮换: {nextRotation ? formatDuration(nextRotation - Date.now()) : '-'}
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                  节点: {currentSession?.remoteAddr || '-'}
                </Typography>
              </Box>
            </CardContent>
          </Card>
        </Grid>

        {/* Traffic Stats */}
        <Grid item xs={12} md={4}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="subtitle1" color="text.secondary" gutterBottom>
                流量统计
              </Typography>

              <Box sx={{ mb: 2 }}>
                <Typography variant="h4" color="primary.main" sx={{ fontWeight: 600 }}>
                  ↑ {formatBytes(totalUpload)}
                </Typography>
                <Typography variant="h4" color="success.main" sx={{ fontWeight: 600 }}>
                  ↓ {formatBytes(totalDownload)}
                </Typography>
              </Box>

              <Typography variant="body2" color="text.secondary">
                活跃连接: {activeStreamCount}
              </Typography>
            </CardContent>
          </Card>
        </Grid>

        {/* Latency */}
        <Grid item xs={12} md={4}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Typography variant="subtitle1" color="text.secondary" gutterBottom>
                延迟
              </Typography>

              <Typography variant="h3" sx={{ fontWeight: 600, mb: 1 }}>
                {metricsHistory.length > 0
                  ? `${metricsHistory[metricsHistory.length - 1].latencyMs || '-'}ms`
                  : '-'
                }
              </Typography>

              {chartData.length > 0 && (
                <Box sx={{ height: 60 }}>
                  <ResponsiveContainer width="100%" height="100%">
                    <LineChart data={chartData.slice(-10)}>
                      <Line
                        type="monotone"
                        dataKey="latency"
                        stroke="#3b82f6"
                        strokeWidth={2}
                        dot={false}
                      />
                    </LineChart>
                  </ResponsiveContainer>
                </Box>
              )}
            </CardContent>
          </Card>
        </Grid>

        {/* Traffic Chart */}
        <Grid item xs={12} md={8}>
          <Card sx={{ height: '400px' }}>
            <CardContent>
              <Typography variant="subtitle1" color="text.secondary" gutterBottom>
                流量趋势
              </Typography>

              <Box sx={{ height: 300 }}>
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={chartData}>
                    <XAxis
                      dataKey="time"
                      tick={{ fontSize: 12 }}
                      tickLine={false}
                    />
                    <YAxis
                      tick={{ fontSize: 12 }}
                      tickLine={false}
                      tickFormatter={(v) => `${v.toFixed(0)}M`}
                    />
                    <Area
                      type="monotone"
                      dataKey="download"
                      stackId="1"
                      stroke="#10b981"
                      fill="#10b981"
                      fillOpacity={0.3}
                    />
                    <Area
                      type="monotone"
                      dataKey="upload"
                      stackId="1"
                      stroke="#3b82f6"
                      fill="#3b82f6"
                      fillOpacity={0.3}
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </Box>
            </CardContent>
          </Card>
        </Grid>

        {/* Real-time Logs */}
        <Grid item xs={12} md={4}>
          <LogPanel />
        </Grid>

        {/* Quick Actions */}
        <Grid item xs={12}>
          <Card>
            <CardContent sx={{ display: 'flex', gap: 2, flexWrap: 'wrap' }}>
              <Button
                variant="contained"
                size="large"
                startIcon={<PowerIcon />}
                color={systemProxyEnabled ? 'error' : 'primary'}
                onClick={() => toggleSystemProxy(!systemProxyEnabled)}
              >
                {systemProxyEnabled ? '关闭系统代理' : '开启系统代理'}
              </Button>

              <Button
                variant="outlined"
                size="large"
                startIcon={<SpeedIcon />}
              >
                节点测速
              </Button>

              <Button
                variant="outlined"
                size="large"
                startIcon={<RefreshIcon />}
              >
                IP优选
              </Button>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
}
