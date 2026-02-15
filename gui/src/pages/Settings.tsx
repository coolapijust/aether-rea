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
  const {
    language,
    setLanguage,
    editingConfig,
    updateEditingConfig,
    applyConfig,
    hasUnsavedChanges
  } = useCoreStore();
  const t = translations[language];

  const handleSave = async () => {
    try {
      await applyConfig();
    } catch (err) {
      console.error(err);
    }
  };

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
            value="http://localhost:9880"
            disabled
            fullWidth
            sx={{ mb: 2 }}
            helperText="Custom API address support coming soon"
          />

          <FormControlLabel
            control={
              <Switch
                checked={editingConfig.rotation.enabled}
                onChange={(e) => updateEditingConfig({ rotation: { ...editingConfig.rotation, enabled: e.target.checked } })}
              />
            }
            label={t.settings.label_auto_reconnect}
          />

          <Divider sx={{ my: 3 }} />

          <Typography variant="h6" gutterBottom>
            {t.settings.group_ui}
          </Typography>

          <FormControl fullWidth sx={{ mb: 2 }}>
            <InputLabel>{t.settings.label_theme}</InputLabel>
            <Select
              value="system"
              label={t.settings.label_theme}
              disabled
            >
              <MenuItem value="light">{t.settings.theme_light}</MenuItem>
              <MenuItem value="dark">{t.settings.theme_dark}</MenuItem>
              <MenuItem value="system">{t.settings.theme_system}</MenuItem>
            </Select>
          </FormControl>

          <FormControl fullWidth sx={{ mb: 2 }}>
            <InputLabel>Language / 语言</InputLabel>
            <Select
              value={language}
              label="Language / 语言"
              onChange={(e) => setLanguage(e.target.value as any)}
            >
              <MenuItem value="en">English</MenuItem>
              <MenuItem value="zh">简体中文</MenuItem>
            </Select>
          </FormControl>

          <Divider sx={{ my: 3 }} />

          <Typography variant="h6" gutterBottom>
            {t.settings.group_about}
          </Typography>

          <Typography variant="body2" color="text.secondary">
            Aether-Realist GUI v0.1.1 (Optimized)
          </Typography>
          <Typography variant="body2" color="text.secondary">
            Protocol: Aether-Realist v5.1
          </Typography>

          {hasUnsavedChanges && (
            <Box sx={{ mt: 3, display: 'flex', gap: 2 }}>
              <Button variant="contained" onClick={handleSave}>
                {t.rules.btn_save}
              </Button>
            </Box>
          )}
        </CardContent>
      </Card>
    </Box>
  );
}
