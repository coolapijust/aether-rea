import { useState } from 'react';
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

export default function Rules() {
  const [activeTab, setActiveTab] = useState(0);
  const { editingConfig, hasUnsavedChanges, updateEditingConfig, applyConfig } = useCoreStore();

  const handleSave = async () => {
    await applyConfig();
  };

  return (
    <Box sx={{ p: 3 }}>
      <Typography variant="h5" sx={{ mb: 3, fontWeight: 600 }}>
        规则配置
      </Typography>

      <Card>
        <Tabs
          value={activeTab}
          onChange={(_, v) => setActiveTab(v)}
          sx={{ borderBottom: 1, borderColor: 'divider' }}
        >
          <Tab label="分流规则" />
          <Tab label="会话设置" />
          <Tab label="高级选项" />
        </Tabs>

        <CardContent>
          {activeTab === 0 && (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              <FormControlLabel
                control={
                  <Switch
                    checked={editingConfig.bypassCN}
                    onChange={(e) => updateEditingConfig({ bypassCN: e.target.checked })}
                  />
                }
                label="国内网站直连"
              />
              
              <FormControlLabel
                control={
                  <Switch
                    checked={editingConfig.blockAds}
                    onChange={(e) => updateEditingConfig({ blockAds: e.target.checked })}
                  />
                }
                label="拦截广告"
              />

              <Divider />
              
              <Typography variant="subtitle2" color="text.secondary">
                自定义规则 (Geo/Domain/IP)
              </Typography>
              <TextField
                multiline
                rows={6}
                placeholder="DOMAIN-SUFFIX,google.com,Proxy\nGEOIP,CN,Direct\n..."
                fullWidth
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
                label="启用会话自动轮换"
              />

              <Box sx={{ display: 'flex', gap: 2 }}>
                <TextField
                  label="最短间隔 (分钟)"
                  type="number"
                  value={editingConfig.rotation.minIntervalMs / 60 / 1000}
                  onChange={(e) =>
                    updateEditingConfig({
                      rotation: {
                        ...editingConfig.rotation,
                        minIntervalMs: parseInt(e.target.value) * 60 * 1000
                      }
                    })
                  }
                  sx={{ flex: 1 }}
                />
                <TextField
                  label="最长间隔 (分钟)"
                  type="number"
                  value={editingConfig.rotation.maxIntervalMs / 60 / 1000}
                  onChange={(e) =>
                    updateEditingConfig({
                      rotation: {
                        ...editingConfig.rotation,
                        maxIntervalMs: parseInt(e.target.value) * 60 * 1000
                      }
                    })
                  }
                  sx={{ flex: 1 }}
                />
              </Box>

              <TextField
                label="预热时间 (秒)"
                type="number"
                value={editingConfig.rotation.preWarmMs / 1000}
                onChange={(e) =>
                  updateEditingConfig({
                    rotation: {
                      ...editingConfig.rotation,
                      preWarmMs: parseInt(e.target.value) * 1000
                    }
                  })
                }
                helperText="新会话提前建立的时间"
                sx={{ maxWidth: 200 }}
              />
            </Box>
          )}

          {activeTab === 2 && (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              <TextField
                label="服务器 URL"
                value={editingConfig.url}
                onChange={(e) => updateEditingConfig({ url: e.target.value })}
                fullWidth
              />
              
              <TextField
                label="PSK (预共享密钥)"
                type="password"
                value={editingConfig.psk}
                onChange={(e) => updateEditingConfig({ psk: e.target.value })}
                fullWidth
              />
              
              <TextField
                label="监听地址"
                value={editingConfig.listenAddr}
                onChange={(e) => updateEditingConfig({ listenAddr: e.target.value })}
                fullWidth
              />
              
              <TextField
                label="最大填充 (bytes)"
                type="number"
                value={editingConfig.maxPadding}
                onChange={(e) => updateEditingConfig({ maxPadding: parseInt(e.target.value) })}
                sx={{ maxWidth: 200 }}
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
                  保存
                </Button>
              }
            >
              配置已修改，记得保存
            </Alert>
          )}
        </CardContent>
      </Card>
    </Box>
  );
}
