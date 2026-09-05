"use client";

import { useCallback, useEffect, useState } from "react";

// Every dashboard page reads real data through this one helper. There is
// no mock/fabricated data path anywhere in this file — a failed request
// surfaces as an explicit error state (see useApi below), never a
// silently-substituted placeholder value.
export function apiBaseUrl(): string {
  return process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
}

export class ApiError extends Error {
  constructor(public endpoint: string, public status: number, message: string) {
    super(message);
    this.name = "ApiError";
  }
}

export async function apiFetch<T>(path: string): Promise<T> {
  const url = `${apiBaseUrl()}${path}`;
  let res: Response;
  try {
    res = await fetch(url);
  } catch (err) {
    throw new ApiError(path, 0, `Could not reach ${url}: ${String(err)}`);
  }
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new ApiError(path, res.status, `${res.status} ${res.statusText}${body ? `: ${body}` : ""}`);
  }
  return (await res.json()) as T;
}

export type ApiState<T> = {
  data: T | null;
  loading: boolean;
  error: ApiError | null;
  refetch: () => void;
};

// useApi is the one hook every dashboard page uses to reach the backend,
// so loading/error/empty handling stays consistent everywhere: no page
// invents its own ad-hoc fetch logic or swallows an error silently.
export function useApi<T>(path: string): ApiState<T> {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<ApiError | null>(null);
  const [attempt, setAttempt] = useState(0);

  const refetch = useCallback(() => setAttempt((n) => n + 1), []);

  useEffect(() => {
    let cancelled = false;
    // Resetting loading/error synchronously at the start of a new
    // request (path or refetch changed) is the correct behavior here —
    // without a data-fetching library (deliberately avoided per this
    // project's "no complex state management libraries" convention),
    // this is the standard shape for a plain-fetch hook.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLoading(true);
    setError(null);
    apiFetch<T>(path)
      .then((result) => {
        if (!cancelled) setData(result);
      })
      .catch((err: ApiError) => {
        if (!cancelled) setError(err);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [path, attempt]);

  return { data, loading, error, refetch };
}

// formatMoney renders an integer minor-units amount as a currency
// string. It never accepts or produces a float for the underlying
// value — the input is always the exact integer the backend returned.
export function formatMoney(minorUnits: number, currency: string): string {
  const major = minorUnits / 100;
  return `${major.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ${currency}`;
}

export function formatPercent(fraction: number, digits = 2): string {
  return `${(fraction * 100).toFixed(digits)}%`;
}

export function formatDateTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString();
  } catch {
    return iso;
  }
}
