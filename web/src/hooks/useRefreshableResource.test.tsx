import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useRefreshableResource } from './useRefreshableResource';

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

async function flush() {
  await act(async () => { await Promise.resolve(); });
}

async function advance(ms: number) {
  await act(async () => { await vi.advanceTimersByTimeAsync(ms); });
}

function visibility(value: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { configurable: true, value });
  act(() => { document.dispatchEvent(new Event('visibilitychange')); });
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-09-11T00:00:00.000Z'));
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  Reflect.deleteProperty(document, 'visibilityState');
});

describe('useRefreshableResource', () => {
  it('loads initially, records successful completion time, and polls every 30 seconds', async () => {
    const first = deferred<{ count: number }>();
    const next = deferred<{ count: number }>();
    const loader = vi.fn().mockReturnValueOnce(first.promise).mockReturnValue(next.promise);
    const { result } = renderHook(() => useRefreshableResource(loader));
    expect(result.current).toMatchObject({
      data: null, initialLoading: true, refreshing: false, error: '',
      lastSuccessAt: null, stale: false, paused: false,
    });
    await flush();
    expect(loader).toHaveBeenCalledTimes(1);
    expect(loader.mock.calls[0][0]).toBeInstanceOf(AbortSignal);
    await advance(123);
    await act(async () => { first.resolve({ count: 1 }); });
    const succeededAt = Date.now();
    expect(result.current).toMatchObject({
      data: { count: 1 }, initialLoading: false, refreshing: false, lastSuccessAt: succeededAt,
    });
    await advance(29_999);
    expect(loader).toHaveBeenCalledTimes(1);
    await advance(1);
    expect(loader).toHaveBeenCalledTimes(2);
    expect(result.current).toMatchObject({ data: { count: 1 }, initialLoading: false, refreshing: true });
    await act(async () => { next.resolve({ count: 2 }); });
    expect(result.current).toMatchObject({ data: { count: 2 }, refreshing: false, lastSuccessAt: Date.now() });
  });

  it('joins overlapping initial, manual, timer, and foreground triggers into one flight', async () => {
    const pending = deferred<number>();
    const loader = vi.fn().mockResolvedValueOnce(1).mockReturnValue(pending.promise);
    const { result } = renderHook(() => useRefreshableResource(loader));
    await flush();
    await advance(30_000);
    expect(loader).toHaveBeenCalledTimes(2);
    let first!: Promise<void>;
    let second!: Promise<void>;
    act(() => {
      first = result.current.refresh();
      second = result.current.refresh();
    });
    expect(first).toBe(second);
    visibility('hidden');
    await advance(5_000);
    visibility('visible');
    expect(loader).toHaveBeenCalledTimes(2);
    await act(async () => { pending.resolve(2); await Promise.all([first, second]); });
    expect(result.current.data).toBe(2);
    await advance(29_999);
    expect(loader).toHaveBeenCalledTimes(2);
  });

  it('joins manual refresh with a pending initial load', async () => {
    const pending = deferred<number>();
    const loader = vi.fn((_signal: AbortSignal) => pending.promise);
    const { result } = renderHook(() => useRefreshableResource(loader));
    await flush();
    let joined!: Promise<void>;
    act(() => { joined = result.current.refresh(); });
    expect(loader).toHaveBeenCalledTimes(1);
    await act(async () => { pending.resolve(3); await joined; });
    expect(result.current).toMatchObject({ data: 3, initialLoading: false });
  });

  it('retains data, error, and successful timestamp until a later successful refresh', async () => {
    const retry = deferred<number>();
    const loader = vi.fn().mockResolvedValueOnce(7).mockRejectedValueOnce(new Error('Unavailable')).mockReturnValue(retry.promise);
    const { result } = renderHook(() => useRefreshableResource(loader));
    await flush();
    const succeededAt = result.current.lastSuccessAt;
    await advance(30_000);
    expect(result.current).toMatchObject({
      data: 7, error: 'Unavailable', lastSuccessAt: succeededAt, stale: true, refreshing: false,
    });
    await advance(1_000);
    let refreshing!: Promise<void>;
    act(() => { refreshing = result.current.refresh(); });
    expect(result.current).toMatchObject({ data: 7, error: 'Unavailable', stale: true, refreshing: true });
    await act(async () => { retry.resolve(8); await refreshing; });
    expect(result.current).toMatchObject({ data: 8, error: '', stale: false, lastSuccessAt: Date.now() });
  });

  it('handles first-load failure and synchronous throws without a retry spin', async () => {
    const loader = vi.fn(() => { throw new Error('Cannot load'); });
    const { result } = renderHook(() => useRefreshableResource(loader));
    await flush();
    expect(result.current).toMatchObject({ data: null, error: 'Cannot load', initialLoading: false, stale: true, lastSuccessAt: null });
    await advance(29_999);
    expect(loader).toHaveBeenCalledTimes(1);
    await advance(1);
    expect(loader).toHaveBeenCalledTimes(2);
    await act(async () => { await expect(result.current.refresh()).resolves.toBeUndefined(); });
  });

  it('times out at 20 seconds, aborts, settles joiners, and ignores obsolete replies', async () => {
    const late = deferred<number>();
    const loader = vi.fn().mockResolvedValueOnce(1).mockReturnValueOnce(late.promise).mockResolvedValue(3);
    const { result } = renderHook(() => useRefreshableResource(loader));
    await flush();
    const succeededAt = result.current.lastSuccessAt;
    let request!: Promise<void>;
    act(() => { request = result.current.refresh(); });
    const signal: AbortSignal = loader.mock.calls[1][0];
    const settled = vi.fn();
    void request.then(settled);
    await advance(19_999);
    expect(signal.aborted).toBe(false);
    expect(result.current.refreshing).toBe(true);
    await advance(1);
    expect(signal.aborted).toBe(true);
    expect(settled).toHaveBeenCalledTimes(1);
    expect(result.current).toMatchObject({ data: 1, refreshing: false, stale: true, lastSuccessAt: succeededAt });
    expect(result.current.error).toContain('20');
    await act(async () => { await result.current.refresh(); });
    const latest = result.current;
    await act(async () => { late.resolve(2); });
    expect(result.current).toEqual(latest);
    expect(result.current.data).toBe(3);
  });

  it('ignores a late rejection after timeout and schedules the next retry once', async () => {
    const late = deferred<number>();
    const loader = vi.fn(() => late.promise);
    const { result } = renderHook(() => useRefreshableResource(loader));
    await flush();
    await advance(20_000);
    const timeoutError = result.current.error;
    await act(async () => { late.reject(new Error('Late rejection')); });
    expect(result.current.error).toBe(timeoutError);
    await advance(29_999);
    expect(loader).toHaveBeenCalledTimes(1);
    await advance(1);
    expect(loader).toHaveBeenCalledTimes(2);
  });

  it('pauses hidden polling and reloads once on return only when due', async () => {
    const loader = vi.fn().mockResolvedValue(1);
    const { result } = renderHook(() => useRefreshableResource(loader));
    await flush();
    await advance(10_000);
    visibility('hidden');
    expect(result.current.paused).toBe(true);
    await advance(10_000);
    visibility('visible');
    expect(result.current.paused).toBe(false);
    expect(loader).toHaveBeenCalledTimes(1);
    await advance(9_999);
    expect(loader).toHaveBeenCalledTimes(1);
    await advance(1);
    expect(loader).toHaveBeenCalledTimes(2);
    visibility('hidden');
    await advance(90_000);
    expect(loader).toHaveBeenCalledTimes(2);
    expect(result.current.stale).toBe(true);
    visibility('visible');
    await flush();
    expect(loader).toHaveBeenCalledTimes(3);
    expect(result.current.stale).toBe(false);
  });

  it('allows a hidden in-flight request to finish but schedules no hidden polling', async () => {
    const pending = deferred<number>();
    const loader = vi.fn((_signal: AbortSignal) => pending.promise);
    const { result } = renderHook(() => useRefreshableResource(loader));
    await flush();
    const signal = loader.mock.calls[0][0];
    visibility('hidden');
    await act(async () => { pending.resolve(4); });
    expect(signal.aborted).toBe(false);
    expect(result.current).toMatchObject({ data: 4, paused: true, initialLoading: false });
    await advance(90_000);
    expect(loader).toHaveBeenCalledTimes(1);
  });

  it('loads once with autoRefresh off, permits manual refresh, and does not reload on visibility return', async () => {
    const loader = vi.fn().mockResolvedValue(1);
    const { result, rerender } = renderHook(({ autoRefresh }) => useRefreshableResource(loader, { autoRefresh }), { initialProps: { autoRefresh: false } });
    await flush();
    expect(loader).toHaveBeenCalledTimes(1);
    expect(result.current.paused).toBe(true);
    await advance(60_000);
    expect(result.current.stale).toBe(false);
    await advance(1);
    expect(result.current.stale).toBe(true);
    visibility('hidden');
    await advance(30_000);
    visibility('visible');
    await flush();
    expect(loader).toHaveBeenCalledTimes(1);
    await act(async () => { await result.current.refresh(); });
    expect(loader).toHaveBeenCalledTimes(2);
    expect(result.current).toMatchObject({ stale: false, paused: true });
    await advance(30_000);
    rerender({ autoRefresh: true });
    await flush();
    expect(loader).toHaveBeenCalledTimes(3);
    expect(result.current.paused).toBe(false);
  });

  it('turning automatic refresh off cancels scheduled polling but not an active request', async () => {
    const pending = deferred<number>();
    const loader = vi.fn().mockResolvedValueOnce(1).mockReturnValue(pending.promise);
    const { result, rerender } = renderHook(({ autoRefresh }) => useRefreshableResource(loader, { autoRefresh }), { initialProps: { autoRefresh: true } });
    await flush();
    rerender({ autoRefresh: false });
    await advance(30_000);
    expect(loader).toHaveBeenCalledTimes(1);
    rerender({ autoRefresh: true });
    await flush();
    expect(loader).toHaveBeenCalledTimes(2);
    rerender({ autoRefresh: false });
    expect(loader.mock.calls[1][0].aborted).toBe(false);
    await act(async () => { pending.resolve(2); });
    await advance(60_001);
    expect(loader).toHaveBeenCalledTimes(2);
    expect(result.current).toMatchObject({ data: 2, stale: true, paused: true });
  });

  it('does not start in a hidden document and loads on its first visible activation even without polling', async () => {
    visibility('hidden');
    const loader = vi.fn().mockResolvedValue(1);
    const { result } = renderHook(() => useRefreshableResource(loader, { autoRefresh: false }));
    await flush();
    await advance(90_000);
    expect(loader).not.toHaveBeenCalled();
    expect(result.current.paused).toBe(true);
    visibility('visible');
    await flush();
    expect(loader).toHaveBeenCalledTimes(1);
    expect(result.current.data).toBe(1);
  });

  it('loads on initial enabled activation and revalidates an inactive tab only if due', async () => {
    const loader = vi.fn().mockResolvedValue(1);
    const { result, rerender } = renderHook(({ enabled }) => useRefreshableResource(loader, { enabled }), { initialProps: { enabled: false } });
    await flush();
    expect(result.current).toMatchObject({ data: null, initialLoading: false, paused: true });
    expect(loader).not.toHaveBeenCalled();
    rerender({ enabled: true });
    await flush();
    expect(loader).toHaveBeenCalledTimes(1);
    rerender({ enabled: false });
    await advance(10_000);
    rerender({ enabled: true });
    await flush();
    expect(loader).toHaveBeenCalledTimes(1);
    rerender({ enabled: false });
    await advance(60_001);
    expect(result.current).toMatchObject({ stale: true, paused: true });
    expect(loader).toHaveBeenCalledTimes(1);
    rerender({ enabled: true });
    await flush();
    expect(loader).toHaveBeenCalledTimes(2);
    expect(result.current).toMatchObject({ stale: false, paused: false });
  });

  it('does not automatically reload previously loaded static snapshots on tab return', async () => {
    const loader = vi.fn().mockResolvedValue(1);
    const { rerender } = renderHook(({ enabled }) => useRefreshableResource(loader, { enabled, autoRefresh: false }), { initialProps: { enabled: false } });
    await flush();
    rerender({ enabled: true });
    await flush();
    expect(loader).toHaveBeenCalledTimes(1);
    rerender({ enabled: false });
    await advance(120_000);
    rerender({ enabled: true });
    await flush();
    expect(loader).toHaveBeenCalledTimes(1);
  });

  it('aborts inactive requests, settles their callers, and invalidates old replies on tab return', async () => {
    const old = deferred<number>();
    const next = deferred<number>();
    const loader = vi.fn().mockReturnValueOnce(old.promise).mockReturnValue(next.promise);
    const { result, rerender } = renderHook(({ enabled }) => useRefreshableResource(loader, { enabled }), { initialProps: { enabled: true } });
    await flush();
    let joined!: Promise<void>;
    act(() => { joined = result.current.refresh(); });
    const signal: AbortSignal = loader.mock.calls[0][0];
    rerender({ enabled: false });
    expect(signal.aborted).toBe(true);
    await act(async () => { await joined; });
    expect(result.current).toMatchObject({ initialLoading: false, refreshing: false, error: '' });
    rerender({ enabled: true });
    await flush();
    expect(loader).toHaveBeenCalledTimes(2);
    await act(async () => { next.resolve(2); });
    await act(async () => { old.resolve(1); });
    expect(result.current.data).toBe(2);
  });

  it('permits explicit manual refresh while disabled without enabling automatic polling', async () => {
    const loader = vi.fn().mockResolvedValue(5);
    const { result } = renderHook(() => useRefreshableResource(loader, { enabled: false }));
    await flush();
    await act(async () => { await result.current.refresh(); });
    expect(result.current).toMatchObject({ data: 5, paused: true, initialLoading: false });
    await advance(90_000);
    expect(loader).toHaveBeenCalledTimes(1);
  });

  it('uses current inline loaders without restarting a flight or resetting the polling cadence', async () => {
    const load = vi.fn().mockResolvedValue(1);
    const { result, rerender } = renderHook(({ value }) => useRefreshableResource(signal => load(value, signal)), { initialProps: { value: 'first' } });
    await flush();
    const stableRefresh = result.current.refresh;
    await advance(10_000);
    rerender({ value: 'latest' });
    await flush();
    expect(load).toHaveBeenCalledTimes(1);
    expect(result.current.refresh).toBe(stableRefresh);
    await advance(20_000);
    expect(load).toHaveBeenCalledTimes(2);
    expect(load.mock.calls[1][0]).toBe('latest');
  });

  it('supports custom intervals and reschedules changed intervals from the last completion', async () => {
    const loader = vi.fn().mockResolvedValue(1);
    const { rerender } = renderHook(({ intervalMs }) => useRefreshableResource(loader, { intervalMs }), { initialProps: { intervalMs: 5_000 } });
    await flush();
    await advance(5_000);
    expect(loader).toHaveBeenCalledTimes(2);
    rerender({ intervalMs: 10_000 });
    await advance(5_000);
    expect(loader).toHaveBeenCalledTimes(2);
    await advance(5_000);
    expect(loader).toHaveBeenCalledTimes(3);
  });

  it('does not duplicate StrictMode startup and completely cleans up pending work on unmount', async () => {
    const pending = deferred<number>();
    const loader = vi.fn((_signal: AbortSignal) => pending.promise);
    const add = vi.spyOn(document, 'addEventListener');
    const remove = vi.spyOn(document, 'removeEventListener');
    const { result, unmount } = renderHook(() => useRefreshableResource(loader), { reactStrictMode: true });
    await flush();
    expect(loader).toHaveBeenCalledTimes(1);
    let joined!: Promise<void>;
    act(() => { joined = result.current.refresh(); });
    const signal = loader.mock.calls[0][0];
    const refreshAfterUnmount = result.current.refresh;
    unmount();
    expect(signal.aborted).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
    await joined;
    const addedListeners = add.mock.calls.filter(([event]) => event === 'visibilitychange').map(([, listener]) => listener);
    const removedListeners = remove.mock.calls.filter(([event]) => event === 'visibilitychange').map(([, listener]) => listener);
    expect(addedListeners).toHaveLength(2);
    expect(removedListeners).toEqual(addedListeners);
    await act(async () => { pending.resolve(9); });
    visibility('hidden');
    visibility('visible');
    await advance(120_000);
    await refreshAfterUnmount();
    expect(loader).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('unmount before the deferred startup performs no I/O and leaves no timers', async () => {
    const loader = vi.fn().mockResolvedValue(1);
    const { unmount } = renderHook(() => useRefreshableResource(loader));
    unmount();
    await flush();
    expect(loader).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('clears scheduled polling and age updates on successful-resource unmount', async () => {
    const loader = vi.fn().mockResolvedValue(1);
    const { unmount } = renderHook(() => useRefreshableResource(loader));
    await flush();
    expect(vi.getTimerCount()).toBe(2);
    unmount();
    expect(vi.getTimerCount()).toBe(0);
    await advance(120_000);
    expect(loader).toHaveBeenCalledTimes(1);
  });
});
