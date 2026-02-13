import { useState, useEffect } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  Tabs,
  Tab,
  Switch,
  FormControlLabel,
  TextField,
  Button,
  Alert,
  Divider,
} from '@mui/material';
import { Save as SaveIcon } from '@mui/icons-material';
import { useCoreStore } from '@/store/coreStore';
import { translations } from '@/lib/i18n';
import type { Rule } from '@/types/core';

export default function Rules() {
  const [activeTab, setActiveTab] = useState(0);
  const {
    language,
    editingConfig,
    hasUnsavedChanges,
    updateEditingConfig,
    applyConfig,
    fetchConfig
  } = useCoreStore();

  const t = translations[language];

  useEffect(() => {
    fetchConfig();
  }, [fetchConfig]);

  const handleSave = async () => {
    await applyConfig();
  };

  return (
    <Box sx={{ p: 3 }}>
      <Typography variant="h5" sx={{ mb: 3, fontWeight: 600 }}>
        {t.rules.title}
      </Typography>

      <Card>
        <Tabs
          value={activeTab}
          onChange={(_, v) => setActiveTab(v)}
          sx={{ borderBottom: 1, borderColor: 'divider' }}
        >
          <Tab label={t.rules.tab_splitting} />
          <Tab label={t.rules.tab_session} />
          <Tab label={t.rules.tab_advanced} />
        </Tabs>

        <CardContent>
          {activeTab === 0 && (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              <FormControlLabel
                control={
                  <Switch
                    checked={editingConfig.bypass_cn}
                    onChange={(e) => updateEditingConfig({ bypass_cn: e.target.checked })}
                  />
                }
                label={t.rules.label_bypass_cn}
              />

              <FormControlLabel
                control={
                  <Switch
                    checked={editingConfig.block_ads}
                    onChange={(e) => updateEditingConfig({ block_ads: e.target.checked })}
                  />
                }
                label={t.rules.label_block_ads}
              />

              <Divider />

              <Typography variant="subtitle2" color="text.secondary">
                {t.rules.subtitle_custom}
              </Typography>
              <TextField
                multiline
                rows={6}
                placeholder="DOMAIN_SUFFIX,google.com,proxy&#10;GEOIP,CN,direct&#10;DOMAIN_KEYWORD,ads,block"
                fullWidth
                value={(editingConfig.rules || [])
                  .map(r => `${r.matches[0].type},${r.matches[0].value},${r.action}`)
                  .join('\n')}
                onChange={(e) => {
                  const lines = e.target.value.split('\n').filter(l => l.trim() !== '');
                  const newRules: Rule[] = lines.map((l, i) => {
                    const [type, value, action] = l.split(',').map(s => s.trim().toLowerCase());
                    return {
                      id: `custom-${i}`,
                      name: `Custom Rule ${i}`,
                      priority: 100 - i,
                      enabled: true,
                      action: action as any,
                      matches: [{ type: type as any, value }]
                    };
                  }).filter(r => ['proxy', 'direct', 'block', 'reject'].includes(r.action));
                  updateEditingConfig({ rules: newRules });
                }}
              />
            </Box>
          )}

          {activeTab === 1 && (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              <FormControlLabel
                control={
                  <Switch
                    checked={editingConfig.rotation.enabled}
                    onChange={(e) =>
                      updateEditingConfig({
                        rotation: { ...editingConfig.rotation, enabled: e.target.checked }
                      })
                    }
                  />
                }
                label={t.rules.label_rotation}
              />

              <Box sx={{ display: 'flex', gap: 2 }}>
                <TextField
                  label={t.rules.label_min_interval}
                  type="number"
                  value={editingConfig.rotation.min_interval_ms / 60 / 1000}
                  onChange={(e) =>
                    updateEditingConfig({
                      rotation: {
                        ...editingConfig.rotation,
                        min_interval_ms: parseInt(e.target.value) * 60 * 1000
                      }
                    })
                  }
                  sx={{ flex: 1 }}
                />
                <TextField
                  label={t.rules.label_max_interval}
                  type="number"
                  value={editingConfig.rotation.max_interval_ms / 60 / 1000}
                  onChange={(e) =>
                    updateEditingConfig({
                      rotation: {
                        ...editingConfig.rotation,
                        max_interval_ms: parseInt(e.target.value) * 60 * 1000
                      }
                    })
                  }
                  sx={{ flex: 1 }}
                />
              </Box>

              <TextField
                label={t.rules.label_prewarm}
                type="number"
                value={editingConfig.rotation.pre_warm_ms / 1000}
                onChange={(e) =>
                  updateEditingConfig({
                    rotation: {
                      ...editingConfig.rotation,
                      pre_warm_ms: parseInt(e.target.value) * 1000
                    }
                  })
                }
                helperText={t.rules.helper_prewarm}
                sx={{ maxWidth: 200 }}
              />

              <Divider />

              <Box>
                <Typography variant="subtitle2" sx={{ mb: 1, color: 'text.secondary' }}>
                  {t.rules.label_window_profile}
                </Typography>
                <Box sx={{ display: 'flex', gap: 1 }}>
                  {[
                    { id: 'conservative', label: t.rules.profile_conservative },
                    { id: 'normal', label: t.rules.profile_normal },
                    { id: 'aggressive', label: t.rules.profile_aggressive },
                  ].map((p) => (
                    <Button
                      key={p.id}
                      variant={editingConfig.window_profile === p.id || (!editingConfig.window_profile && p.id === 'normal') ? 'contained' : 'outlined'}
                      size="small"
                      onClick={() => updateEditingConfig({ window_profile: p.id as any })}
                      sx={{ textTransform: 'none', borderRadius: 2 }}
                    >
                      {p.label}
                    </Button>
                  ))}
                </Box>
              </Box>
            </Box>
          )}

          {activeTab === 2 && (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              <TextField
                label={t.rules.label_server_url}
                value={editingConfig.url}
                onChange={(e) => updateEditingConfig({ url: e.target.value })}
                fullWidth
              />

              <TextField
                label={t.rules.label_psk}
                type="password"
                value={editingConfig.psk}
                onChange={(e) => updateEditingConfig({ psk: e.target.value })}
                fullWidth
              />

              <TextField
                label={t.rules.label_socks_port}
                value={editingConfig.listen_addr}
                onChange={(e) => updateEditingConfig({ listen_addr: e.target.value })}
                fullWidth
              />

              <TextField
                label={t.rules.label_http_port}
                value={editingConfig.http_proxy_addr}
                onChange={(e) => updateEditingConfig({ http_proxy_addr: e.target.value })}
                fullWidth
              />

              <TextField
                label={t.rules.label_padding}
                value={editingConfig.max_padding}
                onChange={(e) => updateEditingConfig({ max_padding: parseInt(e.target.value) })}
                sx={{ maxWidth: 200 }}
              />

              <TextField
                label="Record Payload Bytes"
                type="number"
                value={editingConfig.record_payload_bytes ?? 16384}
                onChange={(e) =>
                  updateEditingConfig({
                    record_payload_bytes: Math.max(1024, parseInt(e.target.value || '16384'))
                  })
                }
                helperText="Recommended: 4096 / 8192 / 16384"
                sx={{ maxWidth: 240 }}
              />

              <Box sx={{ display: 'flex', gap: 2 }}>
                <TextField
                  label="Session Pool Min"
                  type="number"
                  value={editingConfig.session_pool_min ?? 4}
                  onChange={(e) =>
                    updateEditingConfig({
                      session_pool_min: Math.max(1, Math.min(8, parseInt(e.target.value || '4')))
                    })
                  }
                  sx={{ maxWidth: 180 }}
                />
                <TextField
                  label="Session Pool Max"
                  type="number"
                  value={editingConfig.session_pool_max ?? 8}
                  onChange={(e) =>
                    updateEditingConfig({
                      session_pool_max: Math.max(1, Math.min(8, parseInt(e.target.value || '8')))
                    })
                  }
                  sx={{ maxWidth: 180 }}
                />
              </Box>

              <FormControlLabel
                control={
                  <Switch
                    checked={editingConfig.perf_capture_enabled || false}
                    onChange={(e) => updateEditingConfig({ perf_capture_enabled: e.target.checked })}
                  />
                }
                label={t.rules.label_perf_capture_enabled}
              />

              <FormControlLabel
                control={
                  <Switch
                    checked={editingConfig.perf_capture_on_connect ?? true}
                    onChange={(e) => updateEditingConfig({ perf_capture_on_connect: e.target.checked })}
                  />
                }
                label={t.rules.label_perf_capture_on_connect}
              />

              <TextField
                label={t.rules.label_perf_log_path}
                value={editingConfig.perf_log_path ?? 'logs/perf/client-perf.log'}
                onChange={(e) => updateEditingConfig({ perf_log_path: e.target.value })}
                fullWidth
              />

              <FormControlLabel
                control={
                  <Switch
                    checked={editingConfig.allow_insecure || false}
                    onChange={(e) => updateEditingConfig({ allow_insecure: e.target.checked })}
                  />
                }
                label={t.rules.label_insecure}
              />
            </Box>
          )}

          {hasUnsavedChanges && (
            <Alert
              severity="info"
              sx={{ mt: 3 }}
              action={
                <Button
                  color="inherit"
                  size="small"
                  startIcon={<SaveIcon />}
                  onClick={handleSave}
                >
                  {t.rules.btn_save}
                </Button>
              }
            >
              {t.rules.alert_unsaved}
            </Alert>
          )}
        </CardContent>
      </Card>
    </Box>
  );
}
