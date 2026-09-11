import { useEffect, useState } from 'react';
import { Alert, Button, Card, Collapse, Empty, Skeleton, Space, Switch, Table, Tabs, Tag, Tooltip, Typography } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { Link, useOutletContext } from 'react-router-dom';
import { api, accountDisplayName } from '../api';
import type { Account } from '../api';
import type { DashboardOutletContext } from '../layouts/MainLayout';
import { useRefreshableResource } from '../hooks/useRefreshableResource';
import AccountCredentialStatus, { credentialState } from '../components/AccountCredentialStatus';
import CostOverview from '../components/CostOverview';
import ModelBenchmarks from '../components/ModelBenchmarks';
import ResourceStatus from '../components/ResourceStatus';
import { CapabilityRecords, MoreStats, RecentRequests } from '../components/DashboardDetails';
import DashboardTrends from '../components/DashboardTrends';
import { formatCost } from '../utils/costs';
import type { Currency } from '../utils/costs';
import { fmt, fmtLatency, percentage } from '../utils/dashboard';

type Panel = 'live' | 'costs' | 'models';

export default function Dashboard() {
  const { autoRefresh, setAutoRefresh, health } = useOutletContext<DashboardOutletContext>();
  const [panel, setPanel] = useState<Panel>('live');
  const [currency, setCurrency] = useState<Currency>(() => localStorage.getItem('jc_cost_currency') === 'USD' ? 'USD' : 'CNY');
  useEffect(() => { localStorage.setItem('jc_cost_currency', currency); }, [currency]);

  const live = { enabled: panel === 'live', autoRefresh };
  const stats = useRefreshableResource(api.getStats, live);
  const accounts = useRefreshableResource(api.listAccounts, live);
  const logs = useRefreshableResource(signal => api.getRecentLogs(50, signal), live);
  const costs = useRefreshableResource(api.getCosts, { enabled: panel !== 'models', autoRefresh });
  const benchmarks = useRefreshableResource(api.getModelBenchmarks, { enabled: panel === 'models', autoRefresh: false });
  const capabilities = useRefreshableResource(api.getModelCapabilities, { enabled: panel === 'models', autoRefresh: false });

  const currentResources = panel === 'live' ? [stats, accounts, logs, costs] : panel === 'costs' ? [costs] : [benchmarks, capabilities];
  const refreshing = health.refreshing || currentResources.some(r => r.refreshing || r.initialLoading);
  const refresh = () => Promise.all([health.refresh(), ...currentResources.map(r => r.refresh())]);
  const data = stats.data;
  const todayCost = costs.data ? formatCost(costs.data.rows.filter(r => r.day === costs.data!.today), currency) : '—';
  const accountRows = accounts.data ?? [];
  const counts = accountRows.reduce((n, a) => { n[credentialState(a.credential_valid)]++; return n; }, { passed: 0, failed: 0, unknown: 0 });

  const livePanel = <div className="jc-panel-stack">
    <ResourceStatus label="运行统计" resource={stats} />
    <div className="jc-overview-kpis" aria-label="今日关键指标">
      <div className="jc-overview-kpi"><div className="jc-overview-label">今日请求</div><div className="jc-overview-value">{data ? data.total_requests.toLocaleString() : '—'}</div>
        <div className="jc-overview-note">{data ? `${data.error_count.toLocaleString()} 次失败` : '等待统计数据'}</div></div>
      <div className="jc-overview-kpi"><div className="jc-overview-label">今日 Token</div><div className="jc-overview-value">{data ? fmt(data.total_input_tokens + data.total_output_tokens) : '—'}</div>
        <div className="jc-overview-note">已记录输入 + 输出</div></div>
      <div className="jc-overview-kpi"><div className="jc-overview-label">今日成功率</div><div className="jc-overview-value">{data ? percentage(data.success_count, data.total_requests) : '—'}</div>
        <div className="jc-overview-note">{data?.total_requests ? '基于今日已记录请求' : '暂无请求，不计算成功率'}</div></div>
      <div className="jc-overview-kpi"><Tooltip title="请求完成总耗时，包含流式生成全程，不是首 Token 延迟。"><div className="jc-overview-label">平均请求耗时</div></Tooltip>
        <div className="jc-overview-value">{data?.total_requests ? fmtLatency(data.avg_latency_ms) : '—'}</div><div className="jc-overview-note">包含流式生成全程</div></div>
      <div className="jc-overview-kpi jc-cost-kpi"><div className="jc-overview-label">今日估算 · {currency === 'CNY' ? '人民币' : '美元'}</div><div className="jc-overview-value">{todayCost}</div>
        <div className="jc-overview-note">参考费用，非实际账单 <Button type="link" size="small" onClick={() => setPanel('costs')}>明细</Button></div>
        <ResourceStatus label="费用" resource={costs} /></div>
    </div>
    {stats.initialLoading && <Card><Skeleton active paragraph={{ rows: 4 }} /></Card>}
    {data && <DashboardTrends stats={data} at={stats.lastSuccessAt ?? Date.now()} />}
    <section aria-label="最近请求">
      <ResourceStatus label="请求日志" resource={logs} />
      {logs.initialLoading ? <Card><Skeleton active paragraph={{ rows: 3 }} /></Card> : <RecentRequests recentLogs={logs.data?.logs ?? []} />}
    </section>
    <Card size="small" title="账号历史校验" extra={<Link to="/accounts">管理账号</Link>}>
      <ResourceStatus label="账号" resource={accounts} />
      <div className="jc-account-summary">
        <Typography.Text>{accounts.data ? `通过 ${counts.passed} · 失败 ${counts.failed} · 未知 ${counts.unknown}` : '校验数量待读取'}</Typography.Text>
        <Typography.Text type="secondary">后台历史记录，非实时上游探测；校验失败不一定是凭据过期。</Typography.Text>
      </div>
      <Table<Account> rowKey="user_id" size="small" dataSource={accountRows} loading={accounts.initialLoading} pagination={false} scroll={{ x: 660 }}
        locale={{ emptyText: accounts.error ? '账号信息暂不可用，请重试' : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚未配置账号"><Link to="/accounts">添加账号</Link></Empty> }}
        columns={[
          { title: '账号', render: (_, a) => <Typography.Text strong>{accountDisplayName(a)}</Typography.Text> },
          { title: '默认模型', dataIndex: 'default_model', render: (m: string) => m ? <Tag>{m}</Tag> : '—' },
          { title: '今日请求', align: 'right', render: (_, a) => a.today_requests.toLocaleString() },
          { title: '校验记录', width: 260, render: (_, a) => <AccountCredentialStatus account={a} /> },
        ]} />
    </Card>
    {data && <Collapse items={[{ key: 'more', label: '更多统计：累计用量、响应质量、模型与账号分布', children: <MoreStats stats={data} /> }]} />}
    {data && data.total_requests === 0 && (data.all_time?.total_requests ?? 0) === 0 &&
      <Alert type="info" showIcon title="暂无请求数据" description="配置账号并通过本地代理发起请求后，即可查看运行统计。" />}
  </div>;

  return <div className="jc-page jc-dashboard">
    <div className="jc-dashboard-toolbar">
      <div><Typography.Title level={2}>数据概览</Typography.Title><Typography.Text type="secondary">先看运行情况，再查看费用与模型参考。</Typography.Text></div>
      <Space wrap className="jc-refresh-controls">
        <label className="jc-auto-refresh"><Switch size="small" checked={autoRefresh} onChange={setAutoRefresh} aria-label="30 秒自动刷新" />30 秒自动刷新</label>
        <Button icon={<ReloadOutlined />} onClick={() => void refresh()} loading={refreshing}>刷新当前页</Button>
      </Space>
    </div>
    <div className="jc-refresh-hint"><ResourceStatus label="代理连接" resource={health} />
      <Typography.Text type="secondary">{!autoRefresh ? '自动刷新已关闭，可手动刷新。' : panel === 'models' ? '模型参考仅手动重读；代理连接继续定时检查。' : '后台暂停，回到前台时更新过期数据。'}</Typography.Text>
    </div>
    <Tabs activeKey={panel} onChange={key => setPanel(key as Panel)} destroyOnHidden={false} items={[
      { key: 'live', label: '运行概览', children: livePanel },
      { key: 'costs', label: '费用明细', children: <CostOverview resource={costs} currency={currency} onCurrencyChange={setCurrency} /> },
      { key: 'models', label: '模型参考', children: <div className="jc-panel-stack">
        <ModelBenchmarks resource={benchmarks} />
        <ResourceStatus label="历史能力记录" resource={capabilities} snapshot />
        {capabilities.initialLoading ? <Card><Skeleton active /></Card> : <CapabilityRecords caps={capabilities.data?.models ?? []} />}
      </div> },
    ]} />
  </div>;
}
