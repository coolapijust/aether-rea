import React, { useEffect, useRef } from 'react';
import {
    Box,
    Typography,
    Paper,
    IconButton,
    Tooltip,
    Divider,
} from '@mui/material';
import {
    DeleteSweep as ClearIcon,
    VerticalAlignBottom as ScrollIcon,
} from '@mui/icons-material';
import { useCoreStore } from '@/store/coreStore';

const LogPanel: React.FC = () => {
    const logs = useCoreStore((state) => state.logs);
    const clearLogs = useCoreStore((state) => state.clearLogs);
    const scrollRef = useRef<HTMLDivElement>(null);

    // Auto scroll to bottom
    useEffect(() => {
        if (scrollRef.current) {
            scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
        }
    }, [logs]);

    const getLevelColor = (level: string) => {
        switch (level.toLowerCase()) {
            case 'error':
                return '#ff4d4f';
            case 'warn':
                return '#faad14';
            default:
                return '#1890ff';
        }
    };

    const formatTime = (ts: number) => {
        return new Date(ts).toLocaleTimeString([], {
            hour12: false,
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
        });
    };

    return (
        <Paper
            elevation={0}
            sx={{
                height: '400px',
                display: 'flex',
                flexDirection: 'column',
                bgcolor: 'rgba(0, 0, 0, 0.05)',
                borderRadius: 2,
                overflow: 'hidden',
                border: '1px solid rgba(255, 255, 255, 0.1)',
            }}
        >
            <Box
                sx={{
                    p: 1.5,
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    bgcolor: 'rgba(255, 255, 255, 0.02)',
                }}
            >
                <Typography variant="subtitle2" sx={{ fontWeight: 600, opacity: 0.8 }}>
                    实时系统日志
                </Typography>
                <Box>
                    <Tooltip title="自动滚动到底部">
                        <IconButton
                            size="small"
                            onClick={() => {
                                if (scrollRef.current) {
                                    scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
                                }
                            }}
                        >
                            <ScrollIcon fontSize="small" />
                        </IconButton>
                    </Tooltip>
                    <Tooltip title="清空日志">
                        <IconButton size="small" onClick={clearLogs}>
                            <ClearIcon fontSize="small" />
                        </IconButton>
                    </Tooltip>
                </Box>
            </Box>
            <Divider sx={{ opacity: 0.1 }} />
            <Box
                ref={scrollRef}
                sx={{
                    flexGrow: 1,
                    overflowY: 'auto',
                    p: 1.5,
                    fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
                    fontSize: '0.8rem',
                    '&::-webkit-scrollbar': {
                        width: '6px',
                    },
                    '&::-webkit-scrollbar-thumb': {
                        bgcolor: 'rgba(255, 255, 255, 0.1)',
                        borderRadius: '3px',
                    },
                }}
            >
                {logs.length === 0 ? (
                    <Box
                        sx={{
                            height: '100%',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            opacity: 0.3,
                        }}
                    >
                        <Typography variant="body2">等待系统日志输入...</Typography>
                    </Box>
                ) : (
                    logs.map((log, index) => (
                        <Box key={index} sx={{ mb: 0.5, display: 'flex' }}>
                            <Typography
                                component="span"
                                sx={{
                                    color: 'rgba(255, 255, 255, 0.4)',
                                    mr: 1,
                                    userSelect: 'none',
                                }}
                            >
                                [{formatTime(log.timestamp)}]
                            </Typography>
                            <Typography
                                component="span"
                                sx={{
                                    color: getLevelColor(log.level),
                                    fontWeight: 600,
                                    mr: 1,
                                    minWidth: '45px',
                                    display: 'inline-block',
                                }}
                            >
                                {log.level.toUpperCase()}
                            </Typography>
                            <Typography
                                component="span"
                                sx={{ color: 'rgba(255, 255, 255, 0.9)', wordBreak: 'break-all' }}
                            >
                                {log.message}
                            </Typography>
                        </Box>
                    ))
                )}
            </Box>
        </Paper>
    );
};

export default LogPanel;
