import {
  Box,
  Card,
  CardContent,
  Typography,
  TextField,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Switch,
  FormControlLabel,
  Divider,
  Button,
} from '@mui/material';
import { useCoreStore } from '@/store/coreStore';

export default function Settings() {
  const { currentConfig } = useCoreStore();

  return (
    <Box sx={{ p: 3 }}>
      <Typography variant="h5" sx={{ mb: 3, fontWeight: 600 }}>
        设置
      </Typography>

      <Card>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            核心连接
          </Typography>
          
          <TextField
            label="Core API 地址"
            defaultValue="http://localhost:9880"
            fullWidth
            sx={{ mb: 2 }}
          />
          
          <FormControlLabel
            control={<Switch defaultChecked />}
            label="自动重连"
          />
          
          <TextField
            label="重连间隔 (秒)"
            type="number"
            defaultValue={5}
            sx={{ ml: 2, width: 120 }}
          />

          <Divider sx={{ my: 3 }} />

          <Typography variant="h6" gutterBottom>
            外观
          </Typography>
          
          <FormControl fullWidth sx={{ mb: 2 }}>
            <InputLabel>主题</InputLabel>
            <Select defaultValue="system" label="主题">
              <MenuItem value="light">浅色</MenuItem>
              <MenuItem value="dark">深色</MenuItem>
              <MenuItem value="system">跟随系统</MenuItem>
            </Select>
          </FormControl>
          
          <FormControlLabel
            control={<Switch defaultChecked />}
            label="启动时自动连接"
          />
          
          <FormControlLabel
            control={<Switch defaultChecked />}
            label="最小化到系统托盘"
          />

          <Divider sx={{ my: 3 }} />

          <Typography variant="h6" gutterBottom>
            关于
          </Typography>
          
          <Typography variant="body2" color="text.secondary">
            Aether-Realist GUI v0.1.0
          </Typography>
          <Typography variant="body2" color="text.secondary">
            Protocol: Aether-Realist v3
          </Typography>
          <Typography variant="body2" color="text.secondary">
            Core: aetherd v0.1.0
          </Typography>

          <Box sx={{ mt: 3 }}>
            <Button variant="outlined" color="error">
              重置所有设置
            </Button>
          </Box>
        </CardContent>
      </Card>
    </Box>
  );
}
