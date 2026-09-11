import { useEffect, useState } from 'react';
import { Alert, Button, Card, Col, Row, Space, Statistic, Table, Typography } from 'antd';
import { api } from '../api';
import type { CostRow, CostSnapshot } from '../api';

function summary(rows: CostRow[]) {
  return {
    amount: rows.reduce((n,r) => n + (r.amount_tenth_micro_usd ?? 0), 0),
    known: rows.some(r => r.amount_tenth_micro_usd !== null && r.requests > r.missing_usage),
    requests: rows.reduce((n,r) => n+r.requests,0),
    missing: rows.reduce((n,r) => n+r.missing_usage,0),
    unpriced: rows.reduce((n,r) => n+(r.amount_tenth_micro_usd === null ? r.requests : 0),0),
  };
}
function money(value: number) {
  return value > 0 && value < 1000 ? '< $0.0001' : `$${(value/10_000_000).toLocaleString('en-US',{minimumFractionDigits:4,maximumFractionDigits:4})}`;
}
function total(rows: CostRow[]) {
  const s=summary(rows);
  return s.known ? money(s.amount) : s.requests === 0 ? '$0.0000（无记录）' : '—（未能估算）';
}

export default function CostOverview() {
  const [data,setData]=useState<CostSnapshot|null>(null);
  const [error,setError]=useState('');
  const [loading,setLoading]=useState(false);
  const load=async () => {
    setLoading(true); setError('');
    try {setData(await api.getCosts());}
    catch(e){setError(e instanceof Error?e.message:'加载失败');}
    finally {setLoading(false);}
  };
  useEffect(()=>{void load();},[]);
  const all=summary(data?.rows??[]);
  const today=data?.rows.filter(r=>r.day===data.today)??[];
  const days=[...new Set(data?.rows.map(r=>r.day)??[])].sort().reverse()
    .filter(day=>!data || day>=new Date(Date.parse(data.today+'T00:00:00Z')-29*86400000).toISOString().slice(0,10));
  const byModel=[...new Set(data?.rows.map(r=>r.model)??[])].map(model=>({model,rows:data!.rows.filter(r=>r.model===model)}));
  return <Card size="small" style={{marginTop:16}} title="用量费用估算 · USD" extra={<Button onClick={load} loading={loading}>刷新费用</Button>}>
    {error&&<Alert type="error" showIcon title="费用加载失败（已有数据可能过期）" description={error}/>}
    {data&&<>
      <Alert type="warning" showIcon title="参考费用，不是实际扣款或公司账单" description={data.notice} style={{marginBottom:16}}/>
      <Row gutter={[16,16]}>
        <Col xs={24} md={12}><Statistic title={`今日估算 · ${data.today}`} value={total(today)}/><Typography.Text type="secondary">{summary(today).unpriced} 条未知价格 · {summary(today).missing} 条未记录用量</Typography.Text></Col>
        <Col xs={24} md={12}><Statistic title="累计估算（有价格且有用量的部分）" value={total(data.rows)}/><Typography.Text type="secondary">{all.unpriced} 条未知价格 · {all.missing} 条未记录用量（可重叠）</Typography.Text></Col>
      </Row>
      <Typography.Paragraph type="secondary" style={{marginTop:12}}>统计起点：{data.coverage_start||'暂无记录'} · 日期按服务器 {data.timezone} · 价格采集：{data.collected_at}。原始日志清理不减少费用账本；非完整终身账单。</Typography.Paragraph>
      <details><summary style={{cursor:'pointer',marginBottom:12}}>模型单价与价格来源（USD / 每百万 tokens）</summary>
        <Table size="small" rowKey="model" pagination={false} scroll={{x:700}} dataSource={data.rates} columns={[
          {title:'模型',dataIndex:'model'},
          {title:'输入 $/MTok',dataIndex:'input_tenth_micro_usd',render:(v:number|null)=>v===null?'未知':(v/10).toFixed(2)},
          {title:'输出 $/MTok',dataIndex:'output_tenth_micro_usd',render:(v:number|null)=>v===null?'未知':(v/10).toFixed(2)},
          {title:'来源',render:(_,r)=>r.url?<a href={r.url} target="_blank" rel="noopener noreferrer">{r.source} ↗</a>:'待核实'},
          {title:'说明',dataIndex:'note'},
        ]}/>
      </details>
      <Row gutter={[16,16]} style={{marginTop:16}}>
        <Col xs={24} xl={12}><Typography.Title level={5}>每日估算 · 近30日有记录日期</Typography.Title><Table rowKey="day" size="small" pagination={{pageSize:7}} scroll={{x:450}} dataSource={days.map(day=>({day,rows:data.rows.filter(r=>r.day===day)}))} columns={[
          {title:'日期',dataIndex:'day'}, {title:'费用（已知部分）',render:(_,r)=>total(r.rows)},
          {title:'未知价 / 缺用量',render:(_,r)=>`${summary(r.rows).unpriced} / ${summary(r.rows).missing}`},
        ]}/></Col>
        <Col xs={24} xl={12}><Typography.Title level={5}>按模型累计估算</Typography.Title><Table rowKey="model" size="small" pagination={{pageSize:7}} scroll={{x:450}} dataSource={byModel} columns={[
          {title:'模型',dataIndex:'model'}, {title:'费用（已知部分）',render:(_,r)=>total(r.rows)},
          {title:'价格版本',render:(_,r)=><Space direction="vertical">{[...new Set(r.rows.map(x=>x.price_version))].map(v=><Typography.Text key={v} type="secondary">{v}</Typography.Text>)}</Space>},
        ]}/></Col>
      </Row>
    </>}
  </Card>;
}
