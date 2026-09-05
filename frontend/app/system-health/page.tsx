"use client";

import { useApi, formatDateTime } from "@/lib/api";
import type { SystemHealthResponse } from "@/lib/types";
import { LoadingState, ErrorState } from "@/components/DataState";
import { StatusBadge } from "@/components/StatusBadge";

export default function SystemHealthPage() {
  const { data, loading, error, refetch } = useApi<SystemHealthResponse>("/v1/system-health");

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight text-black dark:text-zinc-50">System Health</h1>
        <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
          Real checks only. A component is never reported UP merely because this dashboard is running — see each
          component&apos;s detail for exactly what was checked.
        </p>
      </div>

      {loading && <LoadingState label="Checking component health…" />}
      {error && <ErrorState error={error} onRetry={refetch} />}

      {!loading && !error && data && (
        <div className="overflow-hidden rounded-lg border border-black/[.08] bg-white dark:border-white/[.1] dark:bg-zinc-950">
          <table className="min-w-full text-sm">
            <thead>
              <tr className="border-b border-black/[.08] text-left text-xs uppercase text-zinc-500 dark:border-white/[.1] dark:text-zinc-400">
                <th className="px-4 py-2">Component</th>
                <th className="px-4 py-2">Status</th>
                <th className="px-4 py-2">Detail</th>
                <th className="px-4 py-2">Checked At</th>
              </tr>
            </thead>
            <tbody>
              {data.components.map((c) => (
                <tr key={c.name} className="border-b border-black/[.04] last:border-0 dark:border-white/[.06]">
                  <td className="px-4 py-2 font-medium">{c.name}</td>
                  <td className="px-4 py-2"><StatusBadge status={c.status} /></td>
                  <td className="px-4 py-2 text-xs text-zinc-500 dark:text-zinc-400">{c.detail || "—"}</td>
                  <td className="px-4 py-2 text-xs text-zinc-500 dark:text-zinc-400">{formatDateTime(c.checked_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <button
        onClick={refetch}
        className="w-fit rounded-md border border-black/[.12] bg-white px-3 py-1.5 text-xs font-medium hover:bg-zinc-50 dark:border-white/[.15] dark:bg-zinc-950 dark:hover:bg-zinc-900"
      >
        Re-check now
      </button>
    </div>
  );
}
