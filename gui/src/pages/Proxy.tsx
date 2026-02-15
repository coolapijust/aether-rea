import {
  Box,
  Card,
  CardContent,
  Typography,
  List,
  ListItem,
  ListItemButton,
  ListItemText,
  Chip,
  Button,
  Grid,
  TextField,
} from '@mui/material';
import { Speed as SpeedIcon } from '@mui/icons-material';
import React, { useState } from 'react';
import { useCoreStore } from '@/store/coreStore';
import { translations } from '@/lib/i18n';

interface Node {
  id: string;
  name: string;
  address: string;
  latency?: number;
  selected?: boolean;
}

export default function Proxy() {
  const {
    language,
    editingConfig,
    updateEditingConfig,
    applyConfig,
    hasUnsavedChanges
  } = useCoreStore();
  const t = translations[language];
  const [nodes] = useState<Node[]>([]);

  const [selectedNode, setSelectedNode] = useState('1');

  const getLatencyColor = (latency?: number) => {
    if (!latency) return 'default';
    if (latency < 50) return 'success';
    if (latency < 100) return 'warning';
    return 'error';
  };

  const handleSave = async () => {
    try {
      await applyConfig();
    } catch (err) {
      console.error(err);
    }
  };

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h5" sx={{ fontWeight: 600 }}>
          {t.proxy.title}
        </Typography>
        <Button variant="outlined" startIcon={<SpeedIcon />}>
          {t.proxy.btn_speedtest}
        </Button>
      </Box>

      {/* Quick Server Edit */}
      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="overline" sx={{ fontWeight: 800, color: 'primary.main', mb: 2, display: 'block' }}>
            Current Core Endpoint
          </Typography>
          <Grid container spacing={2}>
            <Grid item xs={12} md={6}>
              <TextField
                label="Server Address"
                value={editingConfig.server_addr}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) => updateEditingConfig({ server_addr: e.target.value })}
                fullWidth
                placeholder="dev.softx.eu.org"
              />
            </Grid>
            <Grid item xs={12} md={2}>
              <TextField
                label="Port"
                type="number"
                value={editingConfig.server_port}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) => updateEditingConfig({ server_port: parseInt(e.target.value) || 443 })}
                fullWidth
              />
            </Grid>
            <Grid item xs={12} md={4}>
              <TextField
                label="Path"
                value={editingConfig.server_path}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) => updateEditingConfig({ server_path: e.target.value })}
                fullWidth
                placeholder="/aether"
              />
            </Grid>
            <Grid item xs={12}>
              <TextField
                label={t.rules.label_psk}
                type="password"
                value={editingConfig.psk}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) => updateEditingConfig({ psk: e.target.value })}
                fullWidth
              />
            </Grid>
          </Grid>
          {hasUnsavedChanges && (
            <Button
              variant="contained"
              sx={{ mt: 2 }}
              onClick={handleSave}
            >
              {t.rules.btn_save}
            </Button>
          )}
        </CardContent>
      </Card>

      <Typography variant="overline" sx={{ fontWeight: 800, opacity: 0.5, mb: 1, display: 'block' }}>
        Proxy Nodes (Managed)
      </Typography>
      <Card>
        <CardContent sx={{ p: 0 }}>
          <List>
            {nodes.map((node) => (
              <ListItem key={node.id} disablePadding>
                <ListItemButton
                  selected={selectedNode === node.id}
                  onClick={() => setSelectedNode(node.id)}
                >
                  <ListItemText
                    primary={node.name}
                    secondary={node.address}
                    primaryTypographyProps={{ fontWeight: 500 }}
                  />
                  {node.latency && (
                    <Chip
                      label={`${node.latency}ms`}
                      color={getLatencyColor(node.latency) as any}
                      size="small"
                      sx={{ ml: 2 }}
                    />
                  )}
                </ListItemButton>
              </ListItem>
            ))}
          </List>
        </CardContent>
      </Card>
    </Box>
  );
}
