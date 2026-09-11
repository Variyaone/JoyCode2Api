// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import AccountCredentialStatus, { credentialState } from './AccountCredentialStatus';
import type { AccountCredentialStatusProps } from './AccountCredentialStatus';

beforeEach(() => {
  // JSDOM has no layout observer; keep Ant Design's real tooltip rendering.
  if (!globalThis.ResizeObserver) {
    vi.stubGlobal('ResizeObserver', class {
      observe() {}
      unobserve() {}
      disconnect() {}
    });
  }
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('credentialState', () => {
  it.each([
    { value: 1, expected: 'passed' },
    { value: 0, expected: 'failed' },
    { value: -1, expected: 'unknown' },
    { value: 2, expected: 'unknown' },
    { value: undefined, expected: 'unknown' },
    { value: null, expected: 'unknown' },
    { value: '1', expected: 'unknown' },
    { value: '0', expected: 'unknown' },
    { value: true, expected: 'unknown' },
    { value: false, expected: 'unknown' },
    { value: NaN, expected: 'unknown' },
    { value: {}, expected: 'unknown' },
    { value: [], expected: 'unknown' },
  ])('maps $value strictly to $expected', ({ value, expected }) => {
    expect(credentialState(value)).toBe(expected);
  });
});

describe('AccountCredentialStatus', () => {
  it.each([
    { value: 1, label: '上次校验通过', icon: 'check-circle' },
    { value: 0, label: '上次校验失败', icon: 'close-circle' },
    { value: -1, label: '尚未验证', icon: 'question-circle' },
    { value: 2, label: '尚未验证', icon: 'question-circle' },
  ])('renders the historical label and icon for $value', ({ value, label, icon }) => {
    const { container } = render(<AccountCredentialStatus account={{ credential_valid: value }} />);
    expect(screen.getByText(label)).toBeTruthy();
    expect(container.querySelector(`.ant-tag .anticon-${icon}`)).not.toBeNull();
    expect(screen.getByText('记录时间：未知')).toBeTruthy();
    expect(screen.queryByText(/^(有效|已过期|检测中)$/)).toBeNull();
  });

  it.each([undefined, null, '', '   ', 'not a date', 123, '2026-13-11 12:30:00', 'https://upstream.invalid/?token=secret'])('uses an unknown time for %s', (time) => {
    const account = { credential_valid: 1, credential_checked_at: time } as AccountCredentialStatusProps['account'];
    render(<AccountCredentialStatus account={account} />);
    expect(screen.getByText('记录时间：未知')).toBeTruthy();
    expect(document.body.textContent).not.toContain('token=secret');
  });

  it.each(['2026-09-11 12:30:00', '2026-09-11T12:30:00Z', '2026-09-11T12:30:00.123+08:00'])('preserves the recorded time %s without timezone guessing', (time) => {
    render(<AccountCredentialStatus account={{ credential_valid: 1, credential_checked_at: time }} />);
    expect(screen.getByText(`记录时间：${time}`)).toBeTruthy();
  });

  it('explains failed records without leaking raw errors, URLs, tokens or refresh times', async () => {
    const account = {
      credential_valid: 0,
      credential_checked_at: '2026-09-11 12:30:00',
      credential_error: 'GET https://upstream.invalid/?token=private-token: authentication failed',
      api_token: 'private-api-token',
      credential_refreshed_at: '2026-01-01 01:02:03',
    };
    render(<AccountCredentialStatus account={account} />);
    fireEvent.mouseOver(screen.getByText('上次校验失败'));
    const tooltip = await screen.findByRole('tooltip');
    expect(tooltip.textContent).toContain('后端保存的历史校验记录');
    expect(tooltip.textContent).toContain('不是实时上游探测');
    expect(tooltip.textContent).toContain('网络或认证问题');
    expect(tooltip.textContent).toContain('不表示已确认过期');
    expect(tooltip.textContent).toContain('记录时间：2026-09-11 12:30:00');
    expect(document.body.textContent).not.toMatch(/upstream\.invalid|private-token|private-api-token|2026-01-01/);
  });

  it.each([1, 0, -1])('keeps time and history explanation keyboard-accessible in compact mode (%s)', async (value) => {
    const { container } = render(<AccountCredentialStatus account={{ credential_valid: value }} compact />);
    expect(screen.queryByText('记录时间：未知')).toBeNull();
    const trigger = container.querySelector('[tabindex="0"]');
    expect(trigger).not.toBeNull();
    fireEvent.focus(trigger!);
    const tooltip = await screen.findByRole('tooltip');
    expect(tooltip.textContent).toContain('记录时间：未知');
    expect(tooltip.textContent).toContain('后端保存的历史校验记录');
    expect(tooltip.textContent).toContain('不是实时上游探测');
  });

  it('never probes or mutates status when rendered or hovered', async () => {
    const fetch = vi.fn();
    vi.stubGlobal('fetch', fetch);
    const { rerender } = render(<AccountCredentialStatus account={{ credential_valid: -1 }} />);
    fireEvent.mouseOver(screen.getByText('尚未验证'));
    await screen.findByRole('tooltip');
    rerender(<AccountCredentialStatus account={{ credential_valid: 1 }} />);
    expect(screen.getByText('上次校验通过')).toBeTruthy();
    expect(fetch).not.toHaveBeenCalled();
  });
});
