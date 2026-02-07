# Aether GUI Assets Guide

## Icons

### Current: Material Icons (from @mui/icons-material)

Already included via MUI. Usage:
```tsx
import { Home, Settings, Power } from '@mui/icons-material';
<HomeIcon />
```

### Alternative: Lucide React (Recommended for more options)

Install:
```bash
npm install lucide-react
```

Usage:
```tsx
import { Home, Settings, Activity } from 'lucide-react';
<Home size={20} strokeWidth={1.5} />
```

Benefits:
- 1000+ icons
- Customizable stroke width
- Consistent design language

## Application Icons

### For Tauri Build

Place icons in `src-tauri/icons/`:

```
src-tauri/icons/
├── icon.png          # 256x256 (Linux default)
├── icon.ico          # Windows (multi-resolution)
├── icon.icns         # macOS
├── 32x32.png         # Tray icon
├── 128x128.png       # App store
└── 128x128@2x.png    # Retina
```

### Generate Icons

Option 1: Online Generator
- https://tauri.app/v1/guides/features/icons/#creating-icons
- Upload 1240x1240 PNG, get all sizes

Option 2: Figma Template
- Design 1024x1024 icon
- Export to all sizes using plugins

Option 3: AI Generated
- Midjourney/Stable Diffusion
- Prompt: "Minimalist network node icon, blue gradient, geometric, transparent background, app icon style"

## Brand Colors

Primary: `#3b82f6` (Blue-500)
Secondary: `#8b5cf6` (Violet-500)
Success: `#10b981` (Emerald-500)
Error: `#ef4444` (Red-500)

## Fonts

Primary: Inter (Google Fonts)
Monospace: JetBrains Mono (for JSON editor)

Already configured in theme.ts
