import { useEffect, useState } from 'react';
import { Alert, Button, Card, Empty, Input, Select, Space, Spin, Table, Tag, Typography } from 'antd';
import { ReloadOutlined, TrophyOutlined } from '@ant-design/icons';
import { api } from '../api';
import type { BenchmarkModel, BenchmarkResult, BenchmarkSnapshot } from '../api';

const mappingLabels: Record<BenchmarkModel['mapping'], string> = {
  name_match: '名称对应 · 配置未核对',
  deployment_reference: '基础模型参考',
  version_ambiguous: '版本待确认',
  unverified: '暂无可核实数据',
};

function bestResult(model: BenchmarkModel, source: string): BenchmarkResult | undefined {
  if (model.mapping !== 'name_match') return undefined;
  return model.results
    .filter(r => r.source_id === source && r.score !== null && !r.footnote)
    .sort((a, b) => (b.score ?? 0) - (a.score ?? 0))[0];
}

export default function ModelBenchmarks() {
  const [snapshot, setSnapshot] = useState<BenchmarkSnapshot | null>(null);
  const [sourceId, setSourceId] = useState('aa');
  const [query, setQuery] = useState('');
  const [ascending, setAscending] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let active = true;
    api.getModelBenchmarks().then(data => {
      if (active) setSnapshot(data);
    }).catch(e => {
      if (active) setError(e instanceof Error ? e.message : '读取失败');
    }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, []);

  const reload = async () => {
    setLoading(true);
    setError('');
    try { setSnapshot(await api.getModelBenchmarks()); }
    catch (e) { setError(e instanceof Error ? e.message : '读取失败'); }
    finally { setLoading(false); }
  };

  const source = snapshot?.sources.find(s => s.id === sourceId);
  const models = (snapshot?.models ?? []).filter(m => m.id.toLowerCase().includes(query.toLowerCase()));
  const rows = models.map(m => ({ ...m, best: bestResult(m, sourceId) })).sort((a, b) => {
    const av = a.best?.score, bv = b.best?.score;
    if (av == null) return bv == null ? a.id.localeCompare(b.id) : 1;
    if (bv == null) return -1;
    return (ascending ? av - bv : bv - av) || a.id.localeCompare(b.id);
  });
  const scored = snapshot?.models.filter(m => bestResult(m, sourceId)).length ?? 0;

  return (
    <Card size="small" className="jc-benchmarks" style={{ marginTop: 16 }}
      title={<span className="jc-section-title"><TrophyOutlined />公开模型评测</span>}
      extra={<Button aria-label="重新读取评测快照" icon={<ReloadOutlined />} onClick={reload} loading={loading}>刷新快照</Button>}>
      <Spin spinning={loading}>
        {error && <Alert type="error" showIcon title="评测数据加载失败" description={error} style={{ marginBottom: 12 }} />}
        {snapshot && source ? <>
          <Alert type="info" showIcon title="公开最高档参考 ≠ JoyCode 当前表现"
            description={snapshot.notice} style={{ marginBottom: 16 }} />
          <Space wrap style={{ marginBottom: 12 }}>
            <Select aria-label="评测来源" value={sourceId} onChange={setSourceId} style={{ width: 240 }}
              options={snapshot.sources.map(s => ({ value: s.id, label: s.name }))} />
            <Input aria-label="筛选评测模型" placeholder="搜索模型名称" allowClear value={query}
              onChange={e => setQuery(e.target.value)} style={{ width: 210 }} />
            <Select aria-label="分数排序" value={ascending ? 'asc' : 'desc'} style={{ width: 165 }}
              onChange={v => setAscending(v === 'asc')}
              options={[{ value: 'desc', label: '分数从高到低' }, { value: 'asc', label: '分数从低到高' }]} />
          </Space>
          <div style={{ marginBottom: 12 }}>
            <Typography.Text strong>{source.metric} · {source.version}</Typography.Text>{' '}
            <Tag>{source.status}</Tag><Tag>{scored} / {snapshot.models.length} 个名称对应模型有评分</Tag>
            <div style={{ margin: '8px 0', lineHeight: 1.7 }}>
              <Typography.Text type="secondary">{source.description}</Typography.Text>
            </div>
            <Space wrap>
              <Typography.Link href={source.url} target="_blank" rel="noopener noreferrer">官方榜单 ↗</Typography.Link>
              <Typography.Link href={source.methodology_url} target="_blank" rel="noopener noreferrer">评测方法 ↗</Typography.Link>
              <Typography.Text type="secondary">采集：{snapshot.collected_at} · 本地快照，非实时抓取</Typography.Text>
            </Space>
          </div>
          <Table rowKey="id" size="small" pagination={false} scroll={{ x: 860 }} dataSource={rows}
            locale={{ emptyText: '没有匹配的模型' }}
            expandable={{
              rowExpandable: m => m.results.some(r => r.source_id === sourceId),
              expandedRowRender: m => <div style={{ padding: 8 }}>
                <Typography.Paragraph>{m.note}</Typography.Paragraph>
                <Table size="small" pagination={false} rowKey={r => `${r.public_model}-${r.variant}`}
                  dataSource={m.results.filter(r => r.source_id === sourceId)} columns={[
                    { title: '公开评测型号', dataIndex: 'public_model' },
                    { title: '评测档位', dataIndex: 'variant' },
                    { title: source.unit, dataIndex: 'score', render: (v: number | null) => v ?? '—' },
                    { title: '脚注', dataIndex: 'footnote', render: (v: string) => v || '—' },
                    { title: '发布日期', dataIndex: 'published_at', render: (v: string | null) => v ?? '来源未提供确切日期' },
                    { title: '证据', dataIndex: 'url', render: (url: string) => <a href={url} target="_blank" rel="noopener noreferrer">原站详情 ↗</a> },
                  ]} />
              </div>,
            }} columns={[
              { title: 'JoyCode 模型', dataIndex: 'id', width: 180, render: (id: string) => <Typography.Text strong>{id}</Typography.Text> },
              { title: `公开最高档 · ${source.unit}`, width: 155, render: (_, m) => m.best
                ? <Typography.Text strong style={{ fontSize: 21, color: '#22C55E' }}>{m.best.score}</Typography.Text>
                : <Typography.Text type="secondary">—</Typography.Text> },
              { title: '公开评测档位', width: 130, render: (_, m) => m.best ? <Tag>{m.best.variant}</Tag> : '—' },
              { title: '对应关系', width: 180, render: (_, m) => <Tag color={sourceId === 'aa' && m.mapping === 'name_match' ? 'blue' : 'default'}>{sourceId === 'aa' ? mappingLabels[m.mapping] : '当前来源暂无已核实成绩'}</Tag> },
              { title: '说明 / 来源', render: (_, m) => <>
                <Typography.Text type="secondary">{sourceId === 'aa' ? m.note : source.description}</Typography.Text>
                {m.best && <div><a href={m.best.url} target="_blank" rel="noopener noreferrer">{m.best.public_model} · 原站详情 ↗</a></div>}
                {!m.best && m.results.some(r => r.source_id === sourceId) && <div><Typography.Text>展开查看参考记录（不参加主排名）</Typography.Text></div>}
              </> },
            ]} />
          <Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0, fontSize: 12 }}>
            分数越高表示该指标表现越好，不是百分制正确率。最高档不是统一计算预算；不同档位请展开比较。
            jcloud 部署和日期不明版本不进入主排名；缺失数据不计为 0。外部测得的价格、速度不代表本地代理费用或速度。
          </Typography.Paragraph>
        </> : !loading && !error && <Empty description="暂无公开评测快照" />}
      </Spin>
    </Card>
  );
}
