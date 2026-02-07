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
  Paper,
} from '@mui/material';
import { Speed as SpeedIcon } from '@mui/icons-material';
import { useState } from 'react';

interface Node {
  id: string;
  name: string;
  address: string;
  latency?: number;
  selected?: boolean;
}

export default function Proxy() {
  const [nodes] = useState<Node[]>([
    { id: '1', name: '自动选择', address: 'auto', selected: true },
    { id: '2', name: '香港节点 1', address: 'hk1.example.com:443', latency: 23 },
    { id: '3', name: '香港节点 2', address: 'hk2.example.com:443', latency: 35 },
    { id: '4', name: '日本节点', address: 'jp1.example.com:443', latency: 45 },
    { id: '5', name: '美国节点', address: 'us1.example.com:443', latency: 120 },
  ]);

  const [selectedNode, setSelectedNode] = useState('1');

  const getLatencyColor = (latency?: number) => {
    if (!latency) return 'default';
    if (latency < 50) return 'success';
    if (latency < 100) return 'warning';
    return 'error';
  };

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h5" sx={{ fontWeight: 600 }}>
          代理节点
        </Typography>
        <Button variant="outlined" startIcon={<SpeedIcon />}>
          测速
        </Button>
      </Box>

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
