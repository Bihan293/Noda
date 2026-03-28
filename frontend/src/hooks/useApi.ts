import { useState, useEffect, useCallback, useRef } from 'react';

/**
 * Generic hook for fetching data with loading/error states.
 * Supports auto-refresh interval.
 */
export function useApi<T>(
  fetcher: () => Promise<T>,
  options?: { autoRefreshMs?: number; immediate?: boolean }
) {
  const { autoRefreshMs, immediate = true } = options || {};
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  const execute = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await fetcher();
      if (mountedRef.current) {
        setData(result);
      }
    } catch (err: unknown) {
      if (mountedRef.current) {
        setError(err instanceof Error ? err.message : 'Unknown error');
      }
    } finally {
      if (mountedRef.current) {
        setLoading(false);
      }
    }
  }, [fetcher]);

  useEffect(() => {
    mountedRef.current = true;
    if (immediate) {
      execute();
    }
    return () => {
      mountedRef.current = false;
    };
  }, [execute, immediate]);

  useEffect(() => {
    if (!autoRefreshMs) return;
    const interval = setInterval(execute, autoRefreshMs);
    return () => clearInterval(interval);
  }, [autoRefreshMs, execute]);

  return { data, loading, error, refetch: execute };
}

/**
 * Copy text to clipboard.
 */
export function useCopyToClipboard() {
  const [copied, setCopied] = useState(false);

  const copy = useCallback(async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
      return true;
    } catch {
      // Fallback
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
      return true;
    }
  }, []);

  return { copied, copy };
}
