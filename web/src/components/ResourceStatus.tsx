import { useEffect, useState } from 'react';
import { Alert, Button, Typography } from 'antd';

export interface ResourceState {
  error: string;
  lastSuccessAt: number | null;
  refreshing: boolean;
  stale: boolean;
  refresh: () => Promise<void>;
}

export function ageLabel(at: number | null, now: number) {
  if (at === null) return '尚未读取';
  const seconds = Math.max(0, Math.floor((now - at) / 1000));
  if (seconds < 5) return '刚刚更新';
  if (seconds < 60) return `${seconds} 秒前更新`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前更新`;
  return `${Math.floor(seconds / 3600)} 小时前更新`;
}

export default function ResourceStatus({ label, resource, snapshot = false }: {
  label: string; resource: ResourceState; snapshot?: boolean;
}) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  const outdated = !snapshot && resource.lastSuccessAt !== null && now - resource.lastSuccessAt > 60000;
  const stamp = resource.lastSuccessAt === null ? undefined : new Date(resource.lastSuccessAt).toLocaleString('zh-CN');
  return <div className="jc-resource-status" data-resource={label}>
    <Typography.Text type="secondary" title={stamp}>
      {label}：{resource.refreshing ? '正在刷新…' : ageLabel(resource.lastSuccessAt, now)}
      {(resource.error || outdated) && resource.lastSuccessAt !== null ? '（数据可能过期）' : ''}
      {snapshot ? ' · 本地快照，不是实时探测' : ''}
    </Typography.Text>
    {resource.error && <Alert type="warning" showIcon
      title={`${label}读取失败${resource.lastSuccessAt !== null ? '，保留上次数据' : ''}`}
      description="请检查本地代理连接后重试。其他已加载内容仍可使用。"
      action={<Button size="small" onClick={() => void resource.refresh()} loading={resource.refreshing}>重试</Button>} />}
  </div>;
}
