import {
  Box,
  Typography,
  Button,
  Grid,
  Paper,
} from '@mui/material';
import {
  PowerSettingsNew as PowerIcon,
  Timeline as LatencyIcon,
  SwapVert as TrafficIcon,
  Dns as ServerIcon,
  Devices as ClientIcon,
  Language as WebIcon,
} from '@mui/icons-material';
import {
  ResponsiveContainer,
  Area,
  AreaChart,
} from 'recharts';
import { useCoreStore } from '@/store/coreStore';
import { formatDuration, formatBytes } from '@/utils/format';

// Mini Component: Sparkline Chart with Gradient
const MiniChart = ({ data, color, dataKey }: { data: any[], color: string, dataKey: string }) => (
  <Box sx={{ height: 60, width: '100%', mt: 1 }}>
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart data={data}>
        <defs>
          <linearGradient id={`gradient${dataKey}`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor={color} stopOpacity={0.3} />
            <stop offset="95%" stopColor={color} stopOpacity={0} />
          </linearGradient>
        </defs>
        <Area
          type="monotone"
          dataKey={dataKey}
          stroke={color}
          fillOpacity={1}
          fill={`url(#gradient${dataKey})`}
          strokeWidth={2}
          isAnimationActive={false}
        />
      </AreaChart>
    </ResponsiveContainer>
  </Box>
);

// Mini Component: Topology Node with Status
const TopologyNode = ({ icon: Icon, label, status, active }: { icon: any, label: string, status?: string, active?: boolean }) => (
  <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 0.5, opacity: active ? 1 : 0.4 }}>
    <Box sx={{
      p: 1.5,
      borderRadius: '50%',
      bgcolor: active ? 'primary.main' : 'rgba(255,255,255,0.05)',
      display: 'flex',
      transition: 'all 0.3s ease',
      boxShadow: active ? '0 0 20px rgba(59, 130, 246, 0.4)' : 'none',
    }}>
      <Icon sx={{ fontSize: 22, color: active ? 'white' : 'inherit' }} />
    </Box>
    <Typography variant="caption" sx={{ fontWeight: 700, fontSize: '0.65rem', mt: 0.5 }}>{label}</Typography>
    {status && <Typography variant="caption" sx={{ fontSize: '0.6rem', opacity: 0.5 }}>{status}</Typography>}
  </Box>
);

export default function Dashboard() {
  const {
    coreState,
    currentSession,
    metricsHistory,
    activeStreamCount,
    totalUpload,
    totalDownload,
    systemProxyEnabled,
    toggleSystemProxy,
    lastError,
    currentConfig,
  } = useCoreStore();

  const isConnected = coreState === 'Active';

  // Real Jitter Calculation (Variation in Latency)
  const jitter = (() => {
    const latencies = metricsHistory
      .map(m => m.latencyMs)
      .filter((l): l is number => l !== undefined && l > 0);
    if (latencies.length < 2) return 0;
    let totalDiff = 0;
    for (let i = 1; i < latencies.length; i++) {
      totalDiff += Math.abs(latencies[i] - latencies[i - 1]);
    }
    return Math.round(totalDiff / (latencies.length - 1));
  })();

  // Extract latest metrics
  const lastMetrics = metricsHistory.length > 0 ? metricsHistory[metricsHistory.length - 1] : null;
  const currentUp = lastMetrics ? lastMetrics.upload / 1024 / 1024 / 5 : 0;
  const currentDown = lastMetrics ? lastMetrics.download / 1024 / 1024 / 5 : 0;

  // Prepare chart data (last 30 snapshots)
  const chartData = metricsHistory.slice(-30).map((m) => ({
    time: m.timestamp,
    upload: m.upload / 1024 / 1024,
    download: m.download / 1024 / 1024,
    latency: m.latencyMs || 0,
  }));

  return (
    <Box sx={{ p: 3, height: '100vh', display: 'flex', flexDirection: 'column', bgcolor: 'background.default', overflow: 'hidden' }}>

      {/* Dynamic Header */}
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
        <Box>
          <Typography variant="h5" sx={{ fontWeight: 800, letterSpacing: -1.5, color: 'primary.main', display: 'flex', alignItems: 'center', gap: 1 }}>
            <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: isConnected ? 'success.main' : 'error.main', boxShadow: isConnected ? '0 0 8px #10b981' : 'none' }} />
            AETHER CONTROL
          </Typography>
          <Typography variant="caption" sx={{ opacity: 0.4, fontWeight: 600, textTransform: 'uppercase', letterSpacing: 2 }}>
            Precision Link Analysis Hub
          </Typography>
        </Box>
        <Button
          variant="contained"
          size="medium"
          startIcon={<PowerIcon />}
          color={systemProxyEnabled ? 'error' : 'primary'}
          onClick={() => toggleSystemProxy(!systemProxyEnabled)}
          sx={{
            borderRadius: 2,
            px: 3,
            textTransform: 'none',
            fontWeight: 700,
            boxShadow: systemProxyEnabled ? '0 4px 14px rgba(239, 68, 68, 0.4)' : '0 4px 14px rgba(59, 130, 246, 0.4)',
            transition: 'all 0.2s'
          }}
        >
          {systemProxyEnabled ? 'Disconnect Proxy' : 'Enable System Proxy'}
        </Button>
      </Box>

      {/* Main Content Area */}
      <Grid container spacing={3} sx={{ flex: 1, minHeight: 0 }}>

        {/* Left Column (Width 4/12) */}
        <Grid item xs={12} md={4} sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>

          {/* Node Topology Widget */}
          <Paper sx={{ p: 3, display: 'flex', flexDirection: 'column', bgcolor: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.06)', borderRadius: 4, position: 'relative' }}>
            <Typography variant="overline" sx={{ opacity: 0.5, fontWeight: 800, mb: 3, display: 'block' }}>Network Topology</Typography>
            <Box sx={{ display: 'flex', justifyContent: 'space-around', alignItems: 'center', position: 'relative' }}>
              <Box sx={{ position: 'absolute', top: '40%', left: '20%', right: '20%', height: 1, background: 'linear-gradient(90deg, transparent, rgba(59, 130, 246, 0.3), transparent)', zIndex: 0 }} />

              <TopologyNode icon={ClientIcon} label="CORE" active={true} status="127.0.0.1" />
              <TopologyNode icon={ServerIcon} label="GATEWAY" active={isConnected} status={isConnected ? 'TRUSTED' : 'OFFLINE'} />
              <TopologyNode icon={WebIcon} label="TARGET" active={isConnected && activeStreamCount > 0} status={`${activeStreamCount} ACTIVE`} />
            </Box>

            {lastError && (
              <Box sx={{ mt: 3, p: 1.5, borderRadius: 2, bgcolor: 'rgba(239, 68, 68, 0.05)', border: '1px solid rgba(239, 68, 68, 0.15)' }}>
                <Typography variant="caption" color="error" sx={{ fontSize: '0.75rem', display: 'block', textAlign: 'center', fontWeight: 500 }}>
                  {lastError.message}
                </Typography>
              </Box>
            )}
          </Paper>

          {/* Metadata Grid */}
          <Paper sx={{ p: 3, bgcolor: 'rgba(255,255,255,0.01)', border: '1px solid rgba(255,255,255,0.04)', borderRadius: 4, flex: 1 }}>
            <Typography variant="overline" sx={{ opacity: 0.5, fontWeight: 800, mb: 2, display: 'block' }}>Session Intelligence</Typography>
            <Grid container spacing={2}>
              <Grid item xs={6}>
                <Typography variant="caption" sx={{ opacity: 0.3, fontWeight: 700, display: 'block' }}>UPLINK PROTOCOL</Typography>
                <Typography variant="body2" sx={{ fontWeight: 600 }}>WebTransport H3</Typography>
              </Grid>
              <Grid item xs={6}>
                <Typography variant="caption" sx={{ opacity: 0.3, fontWeight: 700, display: 'block' }}>UPTIME</Typography>
                <Typography variant="body2" sx={{ fontWeight: 600, fontFamily: 'monospace' }}>{currentSession ? formatDuration(currentSession.uptime) : '00:00:00'}</Typography>
              </Grid>
              <Grid item xs={6}>
                <Typography variant="caption" sx={{ opacity: 0.3, fontWeight: 700, display: 'block' }}>LOCAL PORT</Typography>
                <Typography variant="body2" sx={{ fontWeight: 600 }}>{currentConfig.listen_addr?.split(':')[1] || '1080'}</Typography>
              </Grid>
              <Grid item xs={6}>
                <Typography variant="caption" sx={{ opacity: 0.3, fontWeight: 700, display: 'block' }}>BYPASS CN</Typography>
                <Typography variant="body2" sx={{ fontWeight: 600, color: currentConfig.bypass_cn ? 'success.main' : 'inherit' }}>{currentConfig.bypass_cn ? 'ENABLED' : 'DISABLED'}</Typography>
              </Grid>
            </Grid>
          </Paper>
        </Grid>

        {/* Right Column (Width 8/12) */}
        <Grid item xs={12} md={8}>
          <Grid container spacing={3} sx={{ height: '100%' }}>

            {/* Download Stats Widget */}
            <Grid item xs={6}>
              <Paper sx={{ p: 3, height: '100%', display: 'flex', flexDirection: 'column', borderRadius: 4, border: '1px solid rgba(255,255,255,0.05)' }}>
                <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
                  <Box>
                    <Typography variant="overline" color="success.main" sx={{ fontWeight: 800 }}>Downlink Flow</Typography>
                    <Typography variant="h3" sx={{ fontWeight: 800, letterSpacing: -2 }}>
                      {currentDown.toFixed(2)} <Typography component="span" variant="h6" sx={{ opacity: 0.3, fontWeight: 700 }}>MB/S</Typography>
                    </Typography>
                  </Box>
                  <Box sx={{ p: 1.5, borderRadius: 2, bgcolor: 'rgba(16, 185, 129, 0.1)', color: 'success.main' }}>
                    <TrafficIcon sx={{ fontSize: 24 }} />
                  </Box>
                </Box>
                <MiniChart data={chartData} color="#10b981" dataKey="download" />
                <Box sx={{ mt: 'auto', display: 'flex', justifyContent: 'space-between', pt: 2, borderTop: '1px solid rgba(255,255,255,0.03)' }}>
                  <Typography variant="caption" sx={{ opacity: 0.4 }}>TOTAL RECEIVED</Typography>
                  <Typography variant="caption" sx={{ fontWeight: 700 }}>{formatBytes(totalDownload)}</Typography>
                </Box>
              </Paper>
            </Grid>

            {/* Upload Stats Widget */}
            <Grid item xs={6}>
              <Paper sx={{ p: 3, height: '100%', display: 'flex', flexDirection: 'column', borderRadius: 4, border: '1px solid rgba(255,255,255,0.05)' }}>
                <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
                  <Box>
                    <Typography variant="overline" color="primary.main" sx={{ fontWeight: 800 }}>Uplink Flow</Typography>
                    <Typography variant="h3" sx={{ fontWeight: 800, letterSpacing: -2 }}>
                      {currentUp.toFixed(2)} <Typography component="span" variant="h6" sx={{ opacity: 0.3, fontWeight: 700 }}>MB/S</Typography>
                    </Typography>
                  </Box>
                  <Box sx={{ p: 1.5, borderRadius: 2, bgcolor: 'rgba(59, 130, 246, 0.1)', color: 'primary.main' }}>
                    <TrafficIcon sx={{ fontSize: 24, transform: 'rotate(180deg)' }} />
                  </Box>
                </Box>
                <MiniChart data={chartData} color="#3b82f6" dataKey="upload" />
                <Box sx={{ mt: 'auto', display: 'flex', justifyContent: 'space-between', pt: 2, borderTop: '1px solid rgba(255,255,255,0.03)' }}>
                  <Typography variant="caption" sx={{ opacity: 0.4 }}>TOTAL SENT</Typography>
                  <Typography variant="caption" sx={{ fontWeight: 700 }}>{formatBytes(totalUpload)}</Typography>
                </Box>
              </Paper>
            </Grid>

            {/* Performance Stability Analysis */}
            <Grid item xs={12}>
              <Paper sx={{ p: 3, flex: 1, display: 'flex', flexDirection: 'column', borderRadius: 4, border: '1px solid rgba(255,255,255,0.05)' }}>
                <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 3 }}>
                  <Box>
                    <Typography variant="overline" color="warning.main" sx={{ fontWeight: 800 }}>Latency Performance</Typography>
                    <Box sx={{ display: 'flex', alignItems: 'flex-end', gap: 4 }}>
                      <Typography variant="h2" sx={{ fontWeight: 900, letterSpacing: -4, lineHeight: 1 }}>
                        {lastMetrics?.latencyMs || '-'} <Typography component="span" variant="h5" sx={{ opacity: 0.3, fontWeight: 800, letterSpacing: 0 }}>MS</Typography>
                      </Typography>
                      <Box sx={{ pb: 0.5 }}>
                        <Typography variant="caption" sx={{ display: 'block', opacity: 0.4, fontWeight: 700, mb: -0.5 }}>STABILITY</Typography>
                        <Typography variant="body1" sx={{ fontWeight: 800, color: jitter > 50 ? 'error.main' : 'warning.main' }}>
                          ±{jitter}ms <Typography component="span" variant="caption">JITTER</Typography>
                        </Typography>
                      </Box>
                    </Box>
                  </Box>
                  <Box sx={{ p: 1.5, borderRadius: 2, bgcolor: 'rgba(245, 158, 11, 0.1)', height: 'fit-content' }}>
                    <LatencyIcon sx={{ color: 'warning.main', fontSize: 32 }} />
                  </Box>
                </Box>

                <Box sx={{ flex: 1, minHeight: 120 }}>
                  <ResponsiveContainer width="100%" height="100%">
                    <AreaChart data={chartData}>
                      <defs>
                        <linearGradient id="latencyGrad" x1="0" y1="0" x2="0" y2="1">
                          <stop offset="5%" stopColor="#f59e0b" stopOpacity={0.25} />
                          <stop offset="95%" stopColor="#f59e0b" stopOpacity={0} />
                        </linearGradient>
                      </defs>
                      <Area
                        type="stepAfter"
                        dataKey="latency"
                        stroke="#f59e0b"
                        fill="url(#latencyGrad)"
                        strokeWidth={3}
                        isAnimationActive={false}
                      />
                    </AreaChart>
                  </ResponsiveContainer>
                </Box>
              </Paper>
            </Grid>

          </Grid>
        </Grid>

      </Grid>
    </Box>
  );
}
