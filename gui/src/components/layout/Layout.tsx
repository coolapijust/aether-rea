import { useState } from 'react';
import { 
  Box, 
  Drawer, 
  List, 
  ListItem, 
  ListItemButton,
  ListItemIcon, 
  ListItemText,
  Toolbar,
  Typography,
  IconButton,
  Divider,
} from '@mui/material';
import {
  Home as HomeIcon,
  Language as ProxyIcon,
  Rule as RulesIcon,
  Lan as ConnectionsIcon,
  Article as LogsIcon,
  Settings as SettingsIcon,
  Brightness4 as DarkModeIcon,
  Brightness7 as LightModeIcon,
} from '@mui/icons-material';
import { useTheme } from '@mui/material/styles';

const drawerWidth = 240;

interface LayoutProps {
  children: React.ReactNode;
  currentPage: string;
  onPageChange: (page: string) => void;
  darkMode: boolean;
  onToggleDarkMode: () => void;
}

const menuItems = [
  { id: 'dashboard', label: '首页', icon: HomeIcon },
  { id: 'proxy', label: '代理', icon: ProxyIcon },
  { id: 'rules', label: '规则', icon: RulesIcon },
  { id: 'connections', label: '连接', icon: ConnectionsIcon },
  { id: 'logs', label: '日志', icon: LogsIcon },
  { id: 'settings', label: '设置', icon: SettingsIcon },
];

export default function Layout({ 
  children, 
  currentPage, 
  onPageChange,
  darkMode,
  onToggleDarkMode,
}: LayoutProps) {
  const theme = useTheme();

  return (
    <Box sx={{ display: 'flex', height: '100vh' }}>
      {/* Sidebar */}
      <Drawer
        variant="permanent"
        sx={{
          width: drawerWidth,
          flexShrink: 0,
          '& .MuiDrawer-paper': {
            width: drawerWidth,
            boxSizing: 'border-box',
            borderRight: `1px solid ${theme.palette.divider}`,
          },
        }}
      >
        <Toolbar sx={{ px: 2 }}>
          <Typography variant="h6" sx={{ fontWeight: 700 }}>
            Aether
          </Typography>
          <Typography variant="caption" sx={{ ml: 0.5, opacity: 0.7 }}>
            Realist
          </Typography>
        </Toolbar>
        
        <Divider />
        
        <List sx={{ px: 1, py: 1 }}>
          {menuItems.map((item) => {
            const Icon = item.icon;
            return (
              <ListItem key={item.id} disablePadding>
                <ListItemButton
                  selected={currentPage === item.id}
                  onClick={() => onPageChange(item.id)}
                >
                  <ListItemIcon>
                    <Icon />
                  </ListItemIcon>
                  <ListItemText primary={item.label} />
                </ListItemButton>
              </ListItem>
            );
          })}
        </List>
        
        <Box sx={{ flexGrow: 1 }} />
        
        <Divider />
        
        <Box sx={{ p: 2, display: 'flex', justifyContent: 'center' }}>
          <IconButton onClick={onToggleDarkMode} color="inherit">
            {darkMode ? <LightModeIcon /> : <DarkModeIcon />}
          </IconButton>
        </Box>
      </Drawer>

      {/* Main content */}
      <Box
        component="main"
        sx={{
          flexGrow: 1,
          bgcolor: 'background.default',
          overflow: 'auto',
        }}
      >
        {children}
      </Box>
    </Box>
  );
}
