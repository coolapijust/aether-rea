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
import { translations } from '@/lib/i18n';

export default function Settings() {
  const { language } = useCoreStore();
  const t = translations[language];

  return (
    <Box sx={{ p: 3 }}>
      <Typography variant="h5" sx={{ mb: 3, fontWeight: 600 }}>
        {t.settings.title}
      </Typography>

      <Card>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            {t.settings.group_core}
          </Typography>

          <TextField
            label={t.settings.label_api_addr}
            defaultValue="http://localhost:9880"
            fullWidth
            sx={{ mb: 2 }}
          />

          <FormControlLabel
            control={<Switch defaultChecked />}
            label={t.settings.label_auto_reconnect}
          />

          <TextField
            label={t.settings.label_reconnect_interval}
            type="number"
            defaultValue={5}
            sx={{ ml: 2, width: 120 }}
          />

          <Divider sx={{ my: 3 }} />

          <Typography variant="h6" gutterBottom>
            {t.settings.group_ui}
          </Typography>

          <FormControl fullWidth sx={{ mb: 2 }}>
            <InputLabel>{t.settings.label_theme}</InputLabel>
            <Select defaultValue="system" label={t.settings.label_theme}>
              <MenuItem value="light">{t.settings.theme_light}</MenuItem>
              <MenuItem value="dark">{t.settings.theme_dark}</MenuItem>
              <MenuItem value="system">{t.settings.theme_system}</MenuItem>
            </Select>
          </FormControl>

          <FormControlLabel
            control={<Switch defaultChecked />}
            label={t.settings.label_auto_connect}
          />

          <FormControlLabel
            control={<Switch defaultChecked />}
            label={t.settings.label_minimize}
          />

          <Divider sx={{ my: 3 }} />

          <Typography variant="h6" gutterBottom>
            {t.settings.group_about}
          </Typography>

          <Typography variant="body2" color="text.secondary">
            Aether-Realist GUI v0.1.0
          </Typography>
          <Typography variant="body2" color="text.secondary">
            Protocol: Aether-Realist v5.1
          </Typography>
          <Typography variant="body2" color="text.secondary">
            Core: aetherd v0.1.0
          </Typography>

          <Box sx={{ mt: 3 }}>
            <Button variant="outlined" color="error">
              {t.settings.btn_reset}
            </Button>
          </Box>
        </CardContent>
      </Card>
    </Box>
  );
}
