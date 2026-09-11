import React, { useEffect, useState } from 'react';
import { Layout, Menu, Typography, Tag, theme, Tooltip, Button, message } from 'antd';
import {
  DashboardOutlined,
  TeamOutlined,
  SettingOutlined,
  CheckCircleOutlined,
  QuestionCircleOutlined,
  WarningOutlined,
  GithubOutlined,
  StarFilled,
  LogoutOutlined,
} from '@ant-design/icons';
import { useNavigate, useLocation, Outlet } from 'react-router-dom';
import UsageNotice from '../components/UsageNotice';
import useDocumentTitle from '../hooks/useDocumentTitle';
import { api, clearToken } from '../api';
import { useRefreshableResource } from '../hooks/useRefreshableResource';
import type { ResourceState } from '../components/ResourceStatus';

export interface DashboardOutletContext {
  autoRefresh: boolean;
  setAutoRefresh: (value: boolean) => void;
  health: ResourceState;
}

const { Header, Sider, Content } = Layout;
const { Text } = Typography;

const menuItems = [
  { key: '/dashboard', icon: <DashboardOutlined />, label: '数据概览' },
  { key: '/accounts', icon: <TeamOutlined />, label: '账号管理' },
  { key: '/settings', icon: <SettingOutlined />, label: '系统设置' },
];

const COLLAPSED_KEY = 'joycode_sider_collapsed';

const MainLayout: React.FC = () => {
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(COLLAPSED_KEY) === 'true');
  const navigate = useNavigate();
  const location = useLocation();
  const { token } = theme.useToken();
  useDocumentTitle();

  const [autoRefresh, setAutoRefresh] = useState(() => localStorage.getItem('jc_auto_refresh') !== 'false');
  const health = useRefreshableResource(async signal => {
    const result = await api.getHealth(signal);
    if (result.status !== 'ok') throw new Error('代理状态异常');
    return result;
  }, { autoRefresh });
  const [stars, setStars] = useState<number | null>(null);

  useEffect(() => { localStorage.setItem('jc_auto_refresh', String(autoRefresh)); }, [autoRefresh]);
  useEffect(() => {
    api.getGitHubStars().then((s) => { if (s > 0) setStars(s); }).catch(() => {});
  }, []);

  const selectedKey = location.pathname.startsWith('/accounts') ? '/accounts'
    : location.pathname.startsWith('/settings') ? '/settings'
    : '/dashboard';

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider
        collapsible
        breakpoint="lg"
        collapsedWidth={48}
        collapsed={collapsed}
        onCollapse={(val) => { setCollapsed(val); localStorage.setItem(COLLAPSED_KEY, String(val)); }}
        width={220}
      >
        <div style={{
          height: 56,
          display: 'flex',
          alignItems: 'center',
          justifyContent: collapsed ? 'center' : 'flex-start',
          padding: collapsed ? 0 : '0 20px',
          borderBottom: `1px solid ${token.colorBorderSecondary}`,
          gap: 10,
        }}>
          <div style={{
            width: 28, height: 28, borderRadius: 8, flexShrink: 0,
            background: 'linear-gradient(135deg, #22C55E, #16A34A)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            boxShadow: '0 2px 8px rgba(34, 197, 94, 0.3)',
          }}>
            <img src="/favicon.ico" alt="JoyCode" style={{ width: 18, height: 18, filter: 'brightness(0) invert(1)' }} />
          </div>
          {!collapsed && <Text strong style={{ fontSize: 14, letterSpacing: 0.2 }}>JoyCode 代理</Text>}
        </div>
        <Menu
          mode="inline"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
        />
      </Sider>
      <Layout style={{ minWidth: 0 }}>
        <Header className="jc-main-header">
          <div className="jc-health-summary">
            <Tooltip title="仅表示上次检查时本地代理 HTTP 可达，不代表上游模型可用。">
              <Tag color={health.error ? 'error' : !health.data || health.stale ? 'default' : 'success'}
                icon={health.error ? <WarningOutlined /> : !health.data || health.stale ? <QuestionCircleOutlined /> : <CheckCircleOutlined />}>
                {health.error ? '状态获取失败' : !health.data ? '正在读取状态' : health.stale ? '代理状态待更新' : '代理可连接'}
              </Tag>
            </Tooltip>
            <Text type="secondary" style={{ fontSize: 13 }}>
              {health.data ? `已配置 ${health.data.accounts} 个账号${health.error || health.stale ? '（上次记录）' : ''}` : '账号数量待读取'}
            </Text>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 16, flexShrink: 0 }}>
            <Tooltip title="退出登录">
              <Button
                type="text"
                icon={<LogoutOutlined />}
                onClick={() => {
                  clearToken();
                  message.success('已退出登录');
                  window.location.href = '/login';
                }}
              />
            </Tooltip>
            <Tooltip title="去 GitHub Star 支持我们">
            <a
              href="https://github.com/vibe-coding-labs/JoyCode2Api"
              target="_blank"
              rel="noopener noreferrer"
              style={{ display: 'flex', alignItems: 'center', gap: 6, color: token.colorTextSecondary, fontSize: 13, textDecoration: 'none', transition: 'color 200ms ease' }}
            >
              <GithubOutlined style={{ fontSize: 18 }} />
              GitHub
              {stars !== null && (
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: 3, marginLeft: 2 }}>
                  <StarFilled style={{ fontSize: 13, color: '#F59E0B' }} />
                  <span style={{ fontSize: 12 }}>{stars.toLocaleString()}</span>
                </span>
              )}
            </a>
          </Tooltip>
          </div>
        </Header>
        <Content>
          <Outlet context={{ autoRefresh, setAutoRefresh, health } satisfies DashboardOutletContext} />
          <UsageNotice />
        </Content>
      </Layout>
    </Layout>
  );
};

export default MainLayout;
