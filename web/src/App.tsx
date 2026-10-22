import { useEffect, useState } from 'react';
import { ConfigProvider, Layout, Menu, Tag, theme, type ThemeConfig } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import { DataBrowser } from './pages/DataBrowser';
import { McpInspector } from './pages/McpInspector';
import { Policies } from './pages/Policies';
import { SqlWorkbench } from './pages/SqlWorkbench';

type Page = 'table' | 'sql' | 'policies' | 'mcp';

interface Route {
  page: Page;
  table?: string;
}

function parseHash(): Route {
  const [path] = window.location.hash.replace(/^#\/?/, '').split('?');
  const [page, table] = path.split('/');
  if (page === 'sql' || page === 'policies' || page === 'mcp') return { page };
  return { page: 'table', table: table ? decodeURIComponent(table) : undefined };
}

function useDarkMode() {
  const query = window.matchMedia('(prefers-color-scheme: dark)');
  const [dark, setDark] = useState(query.matches);
  useEffect(() => {
    const listener = (e: MediaQueryListEvent) => setDark(e.matches);
    query.addEventListener('change', listener);
    return () => query.removeEventListener('change', listener);
  }, [query]);
  return dark;
}

// Apple-style frosted glass tokens; the translucent layers let the body gradient show through.
function buildTheme(dark: boolean): ThemeConfig {
  const glass = dark ? 'rgba(36, 36, 40, 0.55)' : 'rgba(255, 255, 255, 0.6)';
  const glassStrong = dark ? 'rgba(28, 28, 32, 0.72)' : 'rgba(255, 255, 255, 0.74)';
  return {
    algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      fontFamily: '-apple-system, BlinkMacSystemFont, "SF Pro Text", "PingFang SC", "Helvetica Neue", Arial, sans-serif',
      fontSize: 13,
      controlHeight: 30,
      controlHeightSM: 24,
      borderRadius: 8,
      borderRadiusLG: 12,
      colorPrimary: '#0071e3',
      colorBgContainer: glass,
      colorBgElevated: glassStrong,
      colorBgLayout: 'transparent',
      colorBorderSecondary: dark ? 'rgba(255, 255, 255, 0.08)' : 'rgba(255, 255, 255, 0.85)',
    },
    components: {
      Layout: { headerBg: glassStrong, headerHeight: 44 },
      Menu: { horizontalLineHeight: '42px' },
      Card: { headerHeight: 40, headerHeightSM: 36, paddingLG: 14 },
      Table: {
        colorBgContainer: 'transparent',
        headerBg: dark ? '#262629' : '#f5f5f7',
        // Set explicitly: antd derives these from the translucent container, which renders near-black on hover.
        rowHoverBg: dark ? '#2c2c30' : '#f0f6ff',
        bodySortBg: dark ? '#222225' : '#fafafc',
        headerSortHoverBg: dark ? '#303034' : '#ebebf0',
        cellPaddingBlock: 6,
        cellPaddingInline: 10,
        cellPaddingBlockSM: 4,
        cellPaddingInlineSM: 8,
      },
    },
  };
}

const titles: Record<Page, string> = {
  sql: 'SQL控制台',
  table: '表数据',
  policies: '规则配置',
  mcp: 'MCP 检查',
};

export function App() {
  const [route, setRoute] = useState(parseHash);
  const dark = useDarkMode();

  useEffect(() => {
    const onHash = () => {
      // Preserve table names and query filters in bookmarked legacy links.
      const hash = window.location.hash.replace(/^#\/?data(?=\/|\?|$)/, '#/table');
      if (hash !== window.location.hash) window.history.replaceState(null, '', hash);
      setRoute(parseHash());
    };
    onHash();
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  }, []);

  useEffect(() => {
    document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  }, [dark]);

  const openTable = (table: string, filter?: { col: string; val: string }) => {
    const qs = filter ? `?${new URLSearchParams({ col: filter.col, val: filter.val })}` : '';
    window.location.hash = `#/table/${encodeURIComponent(table)}${qs}`;
  };

  return (
    <ConfigProvider locale={zhCN} theme={buildTheme(dark)}>
      <Layout style={{ minHeight: '100vh', background: 'transparent' }}>
        <Layout.Header className="app-header">
          <span className="app-title">SQL ReWrite</span>
          <Tag color="red">超级管理员 · 全量数据</Tag>
          <Menu
            mode="horizontal"
            theme={dark ? 'dark' : 'light'}
            selectedKeys={[route.page]}
            style={{ flex: 1, minWidth: 0, background: 'transparent', borderBottom: 'none' }}
            items={(Object.keys(titles) as Page[]).map((key) => ({ key, label: titles[key] }))}
            onClick={({ key }) => (window.location.hash = `#/${key}`)}
          />
        </Layout.Header>
        <Layout.Content className="app-content">
          {route.page === 'table' && <DataBrowser table={route.table} onOpenTable={openTable} />}
          {route.page === 'sql' && <SqlWorkbench />}
          {route.page === 'policies' && <Policies />}
          {route.page === 'mcp' && <McpInspector />}
        </Layout.Content>
      </Layout>
    </ConfigProvider>
  );
}
