import { useEffect } from 'react';
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
  Chip,
} from '@mui/material';
import {
  Home as HomeIcon,
  Language as ProxyIcon,
  Shield as RulesIcon,
  SwapHoriz as ConnectionsIcon,
  Article as LogsIcon,
  Settings as SettingsIcon,
  Brightness4 as DarkModeIcon,
  Brightness7 as LightModeIcon,
  Power as PowerIcon,
  Remove as MinimizeIcon,
  Close as CloseIcon,
} from '@mui/icons-material';
import { useTheme } from '@mui/material/styles';
import { useCoreStore } from '@/store/coreStore';
import { translations } from '@/lib/i18n';
import { appWindow } from '@tauri-apps/api/window';

const drawerWidth = 240;

interface LayoutProps {
  children: React.ReactNode;
  currentPage: string;
  onPageChange: (page: string) => void;
  darkMode: boolean;
  onToggleDarkMode: () => void;
}

export default function Layout({
  children,
  currentPage,
  onPageChange,
  darkMode,
  onToggleDarkMode,
}: LayoutProps) {
  const theme = useTheme();
  const { connectionState, connect, disconnect, language } = useCoreStore();
  const t = translations[language];

  const menuItems = [
    { id: 'dashboard', label: t.nav.dashboard, icon: HomeIcon },
    { id: 'proxy', label: t.nav.proxy, icon: ProxyIcon },
    { id: 'rules', label: t.nav.rules, icon: RulesIcon },
    { id: 'connections', label: t.nav.connections, icon: ConnectionsIcon },
    { id: 'logs', label: t.nav.logs, icon: LogsIcon },
    { id: 'settings', label: t.nav.settings, icon: SettingsIcon },
  ];

  useEffect(() => {
    // Auto-connect on mount
    connect();
    return () => disconnect();
  }, []);

  const getConnectionColor = () => {
    if (connectionState === 'connected') return 'success';
    if (connectionState === 'connecting') return 'warning';
    return 'error';
  };

  return (
    <>
      {/* Draggable Title Bar for the whole app (thin strip) */}
      <Box
        data-tauri-drag-region
        sx={{
          position: 'fixed',
          top: 0,
          left: 0,
          right: 0,
          height: 32,
          zIndex: theme.zIndex.drawer + 2,
          pointerEvents: 'none', // Allow clicking through to underlying elements
          '& > *': { pointerEvents: 'auto' } // But children should catch events
        }}
      />

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
          <Toolbar
            data-tauri-drag-region
            sx={{
              px: 2,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              cursor: 'default',
              WebkitAppRegion: 'drag' // Additional dragging support for CSS
            }}
          >
            <Box
              data-tauri-drag-region
              sx={{ display: 'flex', alignItems: 'center', pointerEvents: 'none' }}
            >
              <Typography variant="h6" sx={{ fontWeight: 700 }}>
                Aether
              </Typography>
              <Typography variant="caption" sx={{ ml: 0.5, opacity: 0.7 }}>
                Realist
              </Typography>
            </Box>
            <Box sx={{ display: 'flex', gap: 0.5, alignItems: 'center', WebkitAppRegion: 'no-drag' }}>
              <IconButton size="small" onClick={() => appWindow.minimize()} sx={{ p: 0.5 }}>
                <MinimizeIcon sx={{ fontSize: 18 }} />
              </IconButton>
              <IconButton size="small" onClick={() => appWindow.close()} sx={{ p: 0.5 }}>
                <CloseIcon sx={{ fontSize: 18 }} />
              </IconButton>
            </Box>
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

          <Box sx={{ p: 2, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <IconButton onClick={onToggleDarkMode} color="inherit" size="small">
              {darkMode ? <LightModeIcon /> : <DarkModeIcon />}
            </IconButton>

            <Chip
              size="small"
              color={getConnectionColor() as any}
              sx={{
                width: 8,
                height: 8,
                minWidth: 8,
                '& .MuiChip-label': { display: 'none' }
              }}
            />

            {connectionState === 'connected' ? (
              <IconButton onClick={disconnect} color="error" size="small">
                <PowerIcon />
              </IconButton>
            ) : (
              <IconButton onClick={connect} color="success" size="small">
                <PowerIcon />
              </IconButton>
            )}
          </Box>
        </Drawer>

        {/* Main content */}
        <Box
          component="main"
          sx={{
            flexGrow: 1,
            bgcolor: 'background.default',
            overflow: 'auto',
            position: 'relative'
          }}
        >
          {/* Top drag handle for main content */}
          <Box
            data-tauri-drag-region
            sx={{
              height: 32,
              width: '100%',
              position: 'absolute',
              top: 0,
              left: 0,
              zIndex: 10
            }}
          />
          {children}
        </Box>
      </Box>
    </>
  );
}
