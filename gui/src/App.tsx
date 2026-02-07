import { useState, useMemo } from 'react';
import { ThemeProvider, CssBaseline } from '@mui/material';
import { lightTheme, darkTheme } from '@/theme';
import Layout from '@/components/layout/Layout';
import Dashboard from '@/pages/Dashboard';
import Proxy from '@/pages/Proxy';
import Rules from '@/pages/Rules';
import Connections from '@/pages/Connections';
import Logs from '@/pages/Logs';
import Settings from '@/pages/Settings';

type Page = 'dashboard' | 'proxy' | 'rules' | 'connections' | 'logs' | 'settings';

const pages: Record<Page, React.ComponentType> = {
  dashboard: Dashboard,
  proxy: Proxy,
  rules: Rules,
  connections: Connections,
  logs: Logs,
  settings: Settings,
};

function App() {
  const [currentPage, setCurrentPage] = useState<Page>('dashboard');
  const [darkMode, setDarkMode] = useState(false);

  const theme = useMemo(() => (darkMode ? darkTheme : lightTheme), [darkMode]);

  const PageComponent = pages[currentPage];

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <Layout
        currentPage={currentPage}
        onPageChange={(page) => setCurrentPage(page as Page)}
        darkMode={darkMode}
        onToggleDarkMode={() => setDarkMode(!darkMode)}
      >
        <PageComponent />
      </Layout>
    </ThemeProvider>
  );
}

export default App;
