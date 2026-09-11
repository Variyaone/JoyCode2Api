import { useCallback, useEffect, useRef, useState } from 'react';

const DEFAULT_INTERVAL_MS = 30_000;
const REQUEST_TIMEOUT_MS = 20_000;
const STALE_AFTER_MS = 60_000;

type Options = {
  enabled?: boolean;
  autoRefresh?: boolean;
  intervalMs?: number;
};

type ResourceState<T> = {
  data: T | null;
  pending: boolean;
  error: string;
  lastSuccessAt: number | null;
};

type Controls = {
  refresh: () => Promise<void>;
  update: () => void;
};

const isVisible = () => typeof document === 'undefined' || document.visibilityState !== 'hidden';

/**
 * Per-owner resource lifecycle, not a global cache. Pass changing loader inputs via
 * a closure; inline loaders do not restart polling. Call refresh when those inputs
 * need an immediate reload. Manual refresh bypasses the automatic polling gates.
 */
export function useRefreshableResource<T>(
  loader: (signal: AbortSignal) => Promise<T>,
  { enabled = true, autoRefresh = true, intervalMs = DEFAULT_INTERVAL_MS }: Options = {},
): {
  data: T | null;
  initialLoading: boolean;
  refreshing: boolean;
  error: string;
  lastSuccessAt: number | null;
  stale: boolean;
  paused: boolean;
  refresh: () => Promise<void>;
} {
  const interval = Number.isFinite(intervalMs) && intervalMs > 0 ? intervalMs : DEFAULT_INTERVAL_MS;
  const loaderRef = useRef(loader);
  const optionsRef = useRef({ enabled, autoRefresh, interval });
  const controlsRef = useRef<Controls | null>(null);
  const lastSettledAtRef = useRef<number | null>(null);
  const [visible, setVisible] = useState(isVisible);
  const [now, setNow] = useState(Date.now);
  const [state, setState] = useState<ResourceState<T>>({
    data: null,
    pending: enabled,
    error: '',
    lastSuccessAt: null,
  });

  // Commit the latest loader without treating its identity as a reload trigger.
  useEffect(() => { loaderRef.current = loader; }, [loader]);

  useEffect(() => {
    let active = true;
    let pageVisible = isVisible();
    let pollTimer: ReturnType<typeof setTimeout> | undefined;
    let flight: {
      controller: AbortController;
      timer: ReturnType<typeof setTimeout>;
      promise: Promise<void>;
      resolve: () => void;
    } | null = null;

    function clearPoll() {
      clearTimeout(pollTimer);
      pollTimer = undefined;
    }

    function cancelFlight(updateState: boolean) {
      const request = flight;
      if (!request) return;
      // Invalidate before abort: even a loader ignoring AbortSignal cannot commit.
      flight = null;
      clearTimeout(request.timer);
      request.controller.abort();
      request.resolve();
      if (updateState) setState(previous => ({ ...previous, pending: false }));
    }

    function synchronize() {
      clearPoll();
      const options = optionsRef.current;
      if (!active || !options.enabled || !pageVisible || flight) return;
      // First activation loads once even for a non-polling/static resource.
      if (lastSettledAtRef.current === null) {
        void refresh();
      } else if (options.autoRefresh) {
        const delay = lastSettledAtRef.current + options.interval - Date.now();
        if (delay <= 0) void refresh();
        else pollTimer = setTimeout(synchronize, delay);
      }
    }

    function refresh(): Promise<void> {
      if (!active) return Promise.resolve();
      if (flight) return flight.promise;
      clearPoll();
      const controller = new AbortController();
      let resolve!: () => void;
      const promise = new Promise<void>(done => { resolve = done; });
      const request = {
        controller,
        promise,
        resolve,
        timer: setTimeout(() => {
          finish({ error: '请求超时（20 秒），请重试' });
          controller.abort();
        }, REQUEST_TIMEOUT_MS),
      };
      flight = request;
      setState(previous => ({ ...previous, pending: true }));

      function finish(result: { data: T } | { error: string }) {
        if (!active || flight !== request) return;
        flight = null;
        clearTimeout(request.timer);
        // Retry cadence uses completion time, including failures, to avoid a hot
        // retry loop. Freshness always uses successful completion time only.
        const completedAt = Date.now();
        lastSettledAtRef.current = completedAt;
        if ('data' in result) {
          setState({ data: result.data, pending: false, error: '', lastSuccessAt: completedAt });
          setNow(completedAt);
        } else {
          setState(previous => ({ ...previous, pending: false, error: result.error }));
        }
        resolve();
        synchronize();
      }

      function fail(error: unknown) {
        const message = error instanceof Error ? error.message : typeof error === 'string' ? error : '';
        finish({ error: message || '刷新失败，请重试' });
      }

      try {
        Promise.resolve(loaderRef.current(controller.signal)).then(data => finish({ data }), fail);
      } catch (error) {
        fail(error);
      }
      return promise;
    }

    function update() {
      clearPoll();
      if (!optionsRef.current.enabled) {
        cancelFlight(true);
        setState(previous => previous.pending ? { ...previous, pending: false } : previous);
      }
      // React StrictMode replays effect setup/cleanup synchronously. Deferring
      // automatic startup lets the discarded setup cancel before it calls I/O.
      queueMicrotask(() => { if (active) synchronize(); });
    }

    function onVisibilityChange() {
      pageVisible = isVisible();
      setVisible(pageVisible);
      setNow(Date.now());
      synchronize();
    }

    const controls = { refresh, update };
    controlsRef.current = controls;
    setVisible(pageVisible);
    document.addEventListener('visibilitychange', onVisibilityChange);
    return () => {
      active = false;
      clearPoll();
      document.removeEventListener('visibilitychange', onVisibilityChange);
      cancelFlight(false);
      if (controlsRef.current === controls) controlsRef.current = null;
    };
  }, []);

  useEffect(() => {
    optionsRef.current = { enabled, autoRefresh, interval };
    controlsRef.current?.update();
  }, [enabled, autoRefresh, interval]);

  // Freshness must age even while polling is paused, without a per-second tick.
  useEffect(() => {
    if (state.lastSuccessAt === null) return;
    const delay = state.lastSuccessAt + STALE_AFTER_MS + 1 - Date.now();
    const timer = setTimeout(() => setNow(Date.now()), Math.max(0, delay));
    return () => clearTimeout(timer);
  }, [state.lastSuccessAt]);

  const refresh = useCallback(() => controlsRef.current?.refresh() ?? Promise.resolve(), []);
  return {
    data: state.data,
    initialLoading: state.pending && state.lastSuccessAt === null,
    refreshing: state.pending && state.lastSuccessAt !== null,
    error: state.error,
    lastSuccessAt: state.lastSuccessAt,
    stale: Boolean(state.error) || (state.lastSuccessAt !== null && Math.max(now, Date.now()) - state.lastSuccessAt > STALE_AFTER_MS),
    paused: !enabled || !autoRefresh || !visible,
    refresh,
  };
}
