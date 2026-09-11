import type { CostRow } from '../api';

export type Currency = 'CNY' | 'USD';

export function summary(rows: CostRow[]) {
  return {
    amount: rows.reduce((n, r) => n + (r.amount_tenth_micro_usd ?? 0), 0),
    known: rows.some(r => r.amount_tenth_micro_usd !== null && r.requests > r.missing_usage),
    requests: rows.reduce((n, r) => n + r.requests, 0),
    missing: rows.reduce((n, r) => n + r.missing_usage, 0),
    unpriced: rows.reduce((n, r) => n + (r.amount_tenth_micro_usd === null ? r.requests : 0), 0),
  };
}

export function money(valueTenthMicroUsd: number, currency: Currency) {
  const usd = valueTenthMicroUsd / 10_000_000;
  const shown = currency === 'CNY' ? usd * 7.2 : usd;
  if (shown > 0 && shown < 0.01) return currency === 'CNY' ? '< ¥0.01' : '< $0.01';
  return (currency === 'CNY' ? '¥' : '$') + shown.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

export function formatCost(rows: CostRow[], currency: Currency, fallback = '—（未能估算）') {
  const s = summary(rows);
  if (s.known) return money(s.amount, currency);
  return s.requests === 0 ? (currency === 'CNY' ? '¥0.00（无记录）' : '$0.00（无记录）') : fallback;
}
