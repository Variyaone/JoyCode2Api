import { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Col, Row, Segmented, Statistic, Table, Typography } from 'antd';
import { api } from '../api';
import type { CostRow, CostSnapshot } from '../api';

type Currency = 'CNY' | 'USD';

const modelLogo: Record<string, string> = {
  'GLM': '/logo-glm.svg',
  'Kimi': '/logo-kimi.svg',
  'Claude': '/logo-claude.svg',
  'GPT': '/logo-openai.svg',
  'DeepSeek': '/logo-deepseek.svg',
  'MiniMax': '/logo-minimax.svg',
  'Doubao': '/logo-bytedance.svg',
  'JoyAI': '/logo-jd.ico',
  'JoyCode': '/logo-jd.ico',
};

function logoFor(model: string) {
  const key = (Object.keys(modelLogo) as string[]).find(k => model.startsWith(k));
  return key ? modelLogo[key] : null;
}

function summary(rows: CostRow[]) {
  return {
    amount: rows.reduce((n, r) => n + (r.amount_tenth_micro_usd ?? 0), 0),
    known: rows.some(r => r.amount_tenth_micro_usd !== null && r.requests > r.missing_usage),
    requests: rows.reduce((n, r) => n + r.requests, 0),
    missing: rows.reduce((n, r) => n + r.missing_usage, 0),
    unpriced: rows.reduce((n, r) => n + (r.amount_tenth_micro_usd === null ? r.requests : 0), 0),
  };
}

function money(valueTenthMicroUsd: number, currency: Currency) {
  const usd = valueTenthMicroUsd / 10_000_000;
  const shown = currency === 'CNY' ? usd * 7.2 : usd;
  if (shown > 0 && shown < 0.01) return currency === 'CNY' ? '< ¥0.01' : '< $0.01';
  return (currency === 'CNY' ? '¥' : '$') + shown.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

export default function CostOverview() {
  const [data, setData] = useState<CostSnapshot | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [currency, setCurrency] = useState<Currency>(() => localStorage.getItem('jc_cost_currency') === 'USD' ? 'USD' : 'CNY');
  const load = async () => {
    setLoading(true); setError('');
    try { setData(await api.getCosts()); }
    catch (e) { setError(e instanceof Error ? e.message : '加载失败'); }
    finally { setLoading(false); }
  };
  useEffect(() => { void load(); }, []);
  useEffect(() => { localStorage.setItem('jc_cost_currency', currency); }, [currency]);

  const fmt = (rows: CostRow[], fallback: string) => {
    const s = summary(rows);
    if (s.known) return money(s.amount, currency);
    return s.requests === 0 ? (currency === 'CNY' ? '¥0.00（无记录）' : '$0.00（无记录）') : fallback;
  };

  const all = summary(data?.rows ?? []);
  const todayRows = useMemo(() => data?.rows.filter(r => r.day === data.today) ?? [], [data]);
  const dayList = useMemo(() => {
    if (!data) return [];
    const cutoff = new Date(Date.parse(data.today + 'T00:00:00') - 29 * 86400000).toISOString().slice(0, 10);
    return [...new Set(data.rows.map(r => r.day))].filter(d => d >= cutoff).sort().reverse();
  }, [data]);
  const byModel = useMemo(() => [...new Set(data?.rows.map(r => r.model) ?? [])].map(model => ({ model, rows: data!.rows.filter(r => r.model === model) })), [data]);

  const currencyCell = { align: 'right' as const };

  return <Card size="small" style={{ marginTop: 16 }} title="用量费用估算"
    extra={<Segmented value={currency} onChange={v => setCurrency(v as Currency)} options={[{ label: '人民币 ¥', value: 'CNY' }, { label: '美元 $', value: 'USD' }]} />}>
    {error && <Alert type="error" showIcon title="费用加载失败（已有数据可能过期）" description={error} />}
    {data && <>
      <Alert type="warning" showIcon title={`参考费用，不是实际扣款或公司账单（按 1 USD ≈ 7.2 CNY 估算汇率换算）`} description={data.notice} style={{ marginBottom: 16 }} />
      <Row gutter={[16, 16]}>
        <Col xs={24} md={12}><Statistic title={`今日估算 · ${data.today}`} value={fmt(todayRows, '—（未能估算）')} />
          <Typography.Text type="secondary">{summary(todayRows).unpriced} 条未知价格 · {summary(todayRows).missing} 条未记录用量</Typography.Text></Col>
        <Col xs={24} md={12}><Statistic title="累计估算（有价格且有用量的部分）" value={fmt(data.rows, '—（未能估算）')} />
          <Typography.Text type="secondary">{all.unpriced} 条未知价格 · {all.missing} 条未记录用量（可重叠）</Typography.Text></Col>
      </Row>
      <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>统计起点：{data.coverage_start || '暂无记录'} · 日期按服务器 {data.timezone} · 价格采集：{data.collected_at}。原始日志清理不减少费用账本；非完整终身账单。</Typography.Paragraph>
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={12}><Typography.Title level={5}>每日估算 · 近30日有记录日期</Typography.Title>
          <Table rowKey="day" size="small" pagination={false} scroll={{ x: 380 }} dataSource={dayList.map(day => ({ day, rows: data.rows.filter(r => r.day === day) }))} columns={[
            { title: '日期', dataIndex: 'day' },
            { title: '费用（已知部分）', ...currencyCell, render: (_, r) => <span style={{ fontWeight: 600 }}>{fmt(r.rows, '—（未能估算）')}</span> },
            { title: '未知价 / 缺用量', ...currencyCell, render: (_, r) => `${summary(r.rows).unpriced} / ${summary(r.rows).missing}` },
          ]} /></Col>
        <Col xs={24} xl={12}><Typography.Title level={5}>按模型累计估算</Typography.Title>
          <Table rowKey="model" size="small" pagination={false} scroll={{ x: 500 }} dataSource={byModel} columns={[
            { title: '模型', dataIndex: 'model', render: (m: string) => <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
              {logoFor(m) && <img src={logoFor(m)!} alt="" width={22} height={22} style={{ flexShrink: 0, objectFit: 'contain' }} />}
              <span>{m}</span></span> },
            { title: '费用（已知部分）', ...currencyCell, render: (_, r) => <span style={{ fontWeight: 600 }}>{fmt(r.rows, '—（未能估算）')}</span> },
            { title: '未知价 / 缺用量', ...currencyCell, render: (_, r) => `${summary(r.rows).unpriced} / ${summary(r.rows).missing}` },
          ]} /></Col>
      </Row>
      <details style={{ marginTop: 16 }}><summary style={{ cursor: 'pointer' }}>模型单价与价格来源（原币标价）</summary>
        <Table size="small" rowKey="model" pagination={false} scroll={{ x: 700 }} dataSource={data.rates} columns={[
          { title: '模型', dataIndex: 'model' },
          { title: '输入价', ...currencyCell, render: (_, r) => r.input_tenth_micro_usd === null ? '未知' : money(r.input_tenth_micro_usd * 1_000_000, currency) + ' / MTok' },
          { title: '输出价', ...currencyCell, render: (_, r) => r.output_tenth_micro_usd === null ? '未知' : money(r.output_tenth_micro_usd * 1_000_000, currency) + ' / MTok' },
          { title: '来源', render: (_, r) => r.url ? <a href={r.url} target="_blank" rel="noopener noreferrer">{r.source} ↗</a> : '待核实' },
          { title: '说明', dataIndex: 'note' },
        ]} />
      </details>
      <Button onClick={load} loading={loading} style={{ marginTop: 12 }}>刷新费用</Button>
    </>}
  </Card>;
}
