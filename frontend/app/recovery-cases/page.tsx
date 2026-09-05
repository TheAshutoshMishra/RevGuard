"use client";

import { Suspense, useMemo, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useApi, formatMoney, formatDateTime } from "@/lib/api";
import type { RecoveryCaseListResponse } from "@/lib/types";
import { LoadingState, ErrorState, EmptyState } from "@/components/DataState";
import { StatusBadge } from "@/components/StatusBadge";

const STATUSES = [
  "DETECTED", "ANALYZING", "ANALYZED", "POLICY_CHECK", "ALLOW", "BLOCK", "ESCALATE",
  "EXECUTING", "VERIFYING", "SUCCESS", "FAILED", "UNKNOWN", "CLOSED",
];

export default function RecoveryCasesPage() {
  return (
    <Suspense fallback={<LoadingState label="Loading recovery cases…" />}>
      <RecoveryCasesContent />
    </Suspense>
  );
}

function RecoveryCasesContent() {
  const searchParams = useSearchParams();
  const [status, setStatus] = useState(searchParams.get("status") ?? "");
  const [search, setSearch] = useState("");

  const path = status ? `/v1/recovery-cases?status=${status}&limit=200` : "/v1/recovery-cases?limit=200";
  const { data, loading, error, refetch } = useApi<RecoveryCaseListResponse>(path);

  const filtered = useMemo(() => {
    if (!data) return [];
    if (!search.trim()) return data.cases;
    const needle = search.trim().toLowerCase();
    return data.cases.filter(
      (c) =>
        c.id.toLowerCase().includes(needle) ||
        c.payment_id.toLowerCase().includes(needle) ||
        (c.failure_category ?? "").toLowerCase().includes(needle)
    );
  }, [data, search]);

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-xl font-semibold tracking-tight text-black dark:text-zinc-50">Recovery Cases</h1>
        <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
          Operational view of every RecoveryCase RevGuard has processed — real, persisted state, never fabricated.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <select
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          className="rounded-md border border-black/[.12] bg-white px-3 py-1.5 text-sm dark:border-white/[.15] dark:bg-zinc-950"
        >
          <option value="">All statuses</option>
          {STATUSES.map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
        </select>
        <input
          type="text"
          placeholder="Search by case ID, payment ID, or failure category…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="min-w-64 rounded-md border border-black/[.12] bg-white px-3 py-1.5 text-sm dark:border-white/[.15] dark:bg-zinc-950"
        />
        {data && <span className="text-xs text-zinc-500 dark:text-zinc-400">{filtered.length} of {data.total} cases</span>}
      </div>

      {loading && <LoadingState label="Loading recovery cases…" />}
      {error && <ErrorState error={error} onRetry={refetch} />}
      {!loading && !error && filtered.length === 0 && (
        <EmptyState label="No recovery cases match this filter. Run `go run ./cmd/demo` to generate real scenarios." />
      )}

      {!loading && !error && filtered.length > 0 && (
        <div className="overflow-x-auto rounded-lg border border-black/[.08] bg-white dark:border-white/[.1] dark:bg-zinc-950">
          <table className="min-w-full text-sm">
            <thead>
              <tr className="border-b border-black/[.08] text-left text-xs uppercase text-zinc-500 dark:border-white/[.1] dark:text-zinc-400">
                <th className="px-3 py-2">Case</th>
                <th className="px-3 py-2">Amount</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2">Failure Category</th>
                <th className="px-3 py-2">AI Recommendation</th>
                <th className="px-3 py-2">Confidence</th>
                <th className="px-3 py-2">Policy</th>
                <th className="px-3 py-2">Outcome</th>
                <th className="px-3 py-2">Recovered</th>
                <th className="px-3 py-2">Updated</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((c) => (
                <tr key={c.id} className="border-b border-black/[.04] last:border-0 hover:bg-zinc-50 dark:border-white/[.06] dark:hover:bg-zinc-900">
                  <td className="px-3 py-2">
                    <Link href={`/recovery-cases/${c.id}`} className="font-mono text-xs text-blue-700 hover:underline dark:text-blue-400">
                      {c.id.slice(0, 8)}…
                    </Link>
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap tabular-nums">{formatMoney(c.revenue_at_risk_minor_units, c.currency)}</td>
                  <td className="px-3 py-2"><StatusBadge status={c.status} /></td>
                  <td className="px-3 py-2 text-xs">{c.failure_category ?? "—"}</td>
                  <td className="px-3 py-2 text-xs">{c.recommended_action ?? "—"}</td>
                  <td className="px-3 py-2 text-xs tabular-nums">{c.confidence != null ? c.confidence.toFixed(2) : "—"}</td>
                  <td className="px-3 py-2">{c.policy_decision ? <StatusBadge status={c.policy_decision} /> : "—"}</td>
                  <td className="px-3 py-2">{c.outcome_status ? <StatusBadge status={c.outcome_status} /> : "—"}</td>
                  <td className="px-3 py-2 whitespace-nowrap tabular-nums">
                    {c.recovered_amount_minor_units != null ? formatMoney(c.recovered_amount_minor_units, c.currency) : "—"}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap text-xs text-zinc-500 dark:text-zinc-400">{formatDateTime(c.updated_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
