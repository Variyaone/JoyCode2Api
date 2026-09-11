import { describe, expect, it } from 'vitest';
import type { CostRow } from '../api';
import { formatCost, money, summary } from './costs';

function row(overrides: Partial<CostRow> = {}): CostRow {
  return {
    day: '2026-09-11', model: 'test-model', price_version: 'test-price', requests: 1,
    input_tokens: 100, output_tokens: 50, missing_usage: 0, input_rate: 1, output_rate: 2,
    amount_tenth_micro_usd: 10_000_000, ...overrides,
  };
}

describe('cost summary', () => {
  it('keeps an empty ledger distinct from a failed or unavailable estimate', () => {
    expect(summary([])).toEqual({ amount: 0, known: false, requests: 0, missing: 0, unpriced: 0 });
    expect(formatCost([], 'CNY')).toBe('¥0.00（无记录）');
    expect(formatCost([], 'USD')).toBe('$0.00（无记录）');
  });

  it('sums the known amount and counts overlapping unknown-price and missing-usage requests', () => {
    const rows = [
      row({ requests: 3, missing_usage: 1, amount_tenth_micro_usd: 20_000_000 }),
      row({ requests: 4, missing_usage: 2, amount_tenth_micro_usd: null }),
    ];
    expect(summary(rows)).toEqual({ amount: 20_000_000, known: true, requests: 7, missing: 3, unpriced: 4 });
    expect(formatCost(rows, 'USD')).toBe('$2.00');
    expect(formatCost(rows, 'CNY')).toBe('¥14.40');
  });

  it('does not present unavailable prices or wholly missing usage as a zero-cost estimate', () => {
    const unpriced = [row({ amount_tenth_micro_usd: null })];
    const missing = [row({ requests: 2, missing_usage: 2, amount_tenth_micro_usd: 0 })];
    expect(summary(unpriced).known).toBe(false);
    expect(summary(missing).known).toBe(false);
    expect(formatCost(unpriced, 'USD')).toBe('—（未能估算）');
    expect(formatCost(missing, 'CNY')).toBe('—（未能估算）');
    expect(formatCost(missing, 'USD', 'Not estimated')).toBe('Not estimated');
  });

  it('recognizes a measured and explicitly priced zero cost as known', () => {
    const rows = [row({ amount_tenth_micro_usd: 0 })];
    expect(summary(rows).known).toBe(true);
    expect(formatCost(rows, 'CNY')).toBe('¥0.00');
    expect(formatCost(rows, 'USD')).toBe('$0.00');
  });
});

describe('cost currency formatting', () => {
  it.each([
    [0, 'USD', '$0.00'], [0, 'CNY', '¥0.00'],
    [1, 'USD', '< $0.01'], [1, 'CNY', '< ¥0.01'],
    [99_999, 'USD', '< $0.01'], [100_000, 'USD', '$0.01'],
    [10_000_000, 'USD', '$1.00'], [10_000_000, 'CNY', '¥7.20'],
    [12_345_600_000, 'USD', '$1,234.56'], [12_345_600_000, 'CNY', '¥8,888.83'],
  ] as const)('formats %s tenth-micro-USD in %s as %s', (amount, currency, expected) => {
    expect(money(amount, currency)).toBe(expected);
  });
});
