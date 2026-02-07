import {
  Box,
  Card,
  CardContent,
  Typography,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  IconButton,
  Chip,
  Paper,
} from '@mui/material';
import { Close as CloseIcon } from '@mui/icons-material';
import { useCoreStore } from '@/store/coreStore';
import { formatBytes, formatDuration } from '@/utils/format';

export default function Connections() {
  const { streams, closeStream } = useCoreStore();

  const getStateColor = (state: string) => {
    switch (state) {
      case 'active': return 'success';
      case 'opening': return 'warning';
      case 'closing': return 'default';
      default: return 'default';
    }
  };

  const activeStreams = Array.from(streams.values()).filter(s => s.state === 'active' || s.state === 'opening');

  return (
    <Box sx={{ p: 3 }}>
      <Typography variant="h5" sx={{ mb: 3, fontWeight: 600 }}>
        活动连接
      </Typography>

      <Card>
        <CardContent sx={{ p: 0 }}>
          <TableContainer>
            <Table>
              <TableHead>
                <TableRow>
                  <TableCell>目标地址</TableCell>
                  <TableCell>状态</TableCell>
                  <TableCell>上传</TableCell>
                  <TableCell>下载</TableCell>
                  <TableCell>时长</TableCell>
                  <TableCell>操作</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {activeStreams.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6} align="center" sx={{ py: 4 }}>
                      <Typography color="text.secondary">
                        暂无活动连接
                      </Typography>
                    </TableCell>
                  </TableRow>
                ) : (
                  activeStreams.map((stream) => (
                    <TableRow key={stream.id}>
                      <TableCell>
                        <Typography variant="body2" fontWeight={500}>
                          {stream.targetHost}
                        </Typography>
                        <Typography variant="caption" color="text.secondary">
                          :{stream.targetPort}
                        </Typography>
                      </TableCell>
                      <TableCell>
                        <Chip
                          label={stream.state === 'active' ? '活跃' : '连接中'}
                          color={getStateColor(stream.state) as any}
                          size="small"
                        />
                      </TableCell>
                      <TableCell>{formatBytes(stream.bytesSent)}</TableCell>
                      <TableCell>{formatBytes(stream.bytesReceived)}</TableCell>
                      <TableCell>
                        {formatDuration(Date.now() - stream.openedAt)}
                      </TableCell>
                      <TableCell>
                        <IconButton
                          size="small"
                          onClick={() => closeStream(stream.id)}
                        >
                          <CloseIcon fontSize="small" />
                        </IconButton>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </TableContainer>
        </CardContent>
      </Card>
    </Box>
  );
}
