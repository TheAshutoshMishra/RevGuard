"use client";

import Link from "next/link";
import { useApi, formatMoney, formatPercent } from "@/lib/api";
import type { RecoveryCaseListResponse } from "@/lib/types";
import { KpiCard } from "@/components/KpiCard";
import { Bar } from "@/components/Bar";
import { LoadingState, ErrorState, EmptyState } from "@/components/DataState";
import { StatusBadge } from "@/components/StatusBadge";

const FUNNEL_ORDER = [
  "DETECTED",
  "ANALYZING",
  "ANALYZED",
  "POLICY_CHECK",
  "ALLOW",
  "BLOCK",
  "ESCALATE",
  "EXECUTING",
  "VERIFYING",
  "SUCCESS",
  "FAILED",
  "UNKNOWN",
];

export default function OverviewPage() {
  const { data, loading, error, refetch } = useApi<RecoveryCaseListResponse>("/v1/recovery-cases?limit=200");

  return (
    <div className="flex flex-col gap-8">
      <div>
        <h1 className="text-xl font-semibold tracking-tight text-black dark:text-zinc-50">
          Revenue Recovery Control Plane
        </h1>
        <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
          AI recommends. Policy decides. Infrastructure executes. Webhooks and reconciliation verify.
        </p>
      </div>

      {loading && <LoadingState label="Loading recovery cases…" />}
      {error && <ErrorState error={error} onRetry={refetch} />}
      {!loading && !error && data && data.cases.length === 0 && (
        <EmptyState label="No recovery cases yet. Run `go run ./cmd/demo` to generate real end-to-end scenarios, then refresh." />
      )}
      {!loading && !error && data && data.cases.length > 0 && <Overview cases={data.cases} />}
    </div>
  );
}

function Overview({ cases }: { cases: RecoveryCaseListResponse["cases"] }) {
  const currency = cases[0]?.currency ?? "INR";

  const revenueAtRisk = sum(cases, (c) => c.revenue_at_risk_minor_units);
  const recoveredCases = cases.filter((c) => c.outcome_status === "SUCCESS");
  const revenueRecovered = sum(recoveredCases, (c) => c.recovered_amount_minor_units ?? 0);
  const recoveryRate = revenueAtRisk > 0 ? revenueRecovered / revenueAtRisk : 0;
  const recoveryCost = sum(cases, (c) => (c.action_cost_minor_units ?? 0) + (c.risk_cost_minor_units ?? 0));
  const escalations = cases.filter((c) => c.policy_decision === "ESCALATE").length;
  const blocks = cases.filter((c) => c.policy_decision === "BLOCK").length;

  // "Potentially recoverable" for REAL cases (as opposed to the
  // Evaluation page's synthetic ground truth) is defined honestly as:
  // revenue at risk for cases not yet BLOCKed or definitively FAILED —
  // i.e. still has a live path to recovery. This is a real, computed
  // figure from real data, not a fabricated projection.
  const potentiallyRecoverable = sum(
    cases.filter((c) => c.policy_decision !== "BLOCK" && c.outcome_status !== "FAILED"),
    (c) => c.revenue_at_risk_minor_units
  );

  const funnelCounts = FUNNEL_ORDER.map((status) => ({
    status,
    count: cases.filter((c) => c.status === status).length,
  }));
  const maxFunnel = Math.max(1, ...funnelCounts.map((f) => f.count));

  const outcomeCounts = [
    { label: "SUCCESS", count: cases.filter((c) => c.outcome_status === "SUCCESS").length, color: "bg-emerald-600" },
    { label: "FAILED", count: cases.filter((c) => c.outcome_status === "FAILED").length, color: "bg-red-600" },
    { label: "UNKNOWN", count: cases.filter((c) => c.outcome_status === "UNKNOWN" || c.status === "UNKNOWN").length, color: "bg-zinc-500" },
    { label: "BLOCKED", count: blocks, color: "bg-red-400" },
    { label: "ESCALATED", count: escalations, color: "bg-amber-500" },
  ];
  const maxOutcome = Math.max(1, ...outcomeCounts.map((o) => o.count));

  return (
    <>
      <section className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4">
        <KpiCard label="Revenue At Risk" value={formatMoney(revenueAtRisk, currency)} sub={`${cases.length} recovery cases`} />
        <KpiCard label="Potentially Recoverable" value={formatMoney(potentiallyRecoverable, currency)} sub="not yet blocked or failed" />
        <KpiCard label="Revenue Recovered" value={formatMoney(revenueRecovered, currency)} sub={`${recoveredCases.length} confirmed`} />
        <KpiCard label="Recovery Rate" value={formatPercent(recoveryRate)} sub="recovered / at risk" />
        <KpiCard
          label="Incremental Recovered Revenue"
          value="N/A"
          sub="requires a baseline — see the Evaluation page"
        />
        <KpiCard label="Recovery Cost" value={formatMoney(recoveryCost, currency)} sub="action + risk cost" />
        <KpiCard label="Human Escalations" value={String(escalations)} sub="policy ESCALATE decisions" />
        <KpiCard label="Policy Blocks" value={String(blocks)} sub="policy BLOCK decisions" />
      </section>

      <section>
        <h2 className="mb-3 text-sm font-semibold text-black dark:text-zinc-50">Recovery pipeline (current case statuses)</h2>
        <div className="flex flex-col gap-1.5 rounded-lg border border-black/[.08] bg-white p-4 dark:border-white/[.1] dark:bg-zinc-950">
          {funnelCounts.map((f) => (
            <Link key={f.status} href={`/recovery-cases?status=${f.status}`} className="block hover:opacity-80">
              <Bar label={f.status} value={f.count} max={maxFunnel} formattedValue={String(f.count)} />
            </Link>
          ))}
        </div>
      </section>

      <section>
        <h2 className="mb-3 text-sm font-semibold text-black dark:text-zinc-50">Outcome breakdown</h2>
        <div className="flex flex-col gap-1.5 rounded-lg border border-black/[.08] bg-white p-4 dark:border-white/[.1] dark:bg-zinc-950">
          {outcomeCounts.map((o) => (
            <Bar key={o.label} label={o.label} value={o.count} max={maxOutcome} formattedValue={String(o.count)} colorClassName={o.color} />
          ))}
        </div>
      </section>

      <section className="rounded-lg border border-black/[.08] bg-white p-4 text-sm dark:border-white/[.1] dark:bg-zinc-950">
        <h2 className="mb-2 font-semibold text-black dark:text-zinc-50">Safety architecture</h2>
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <span className="rounded-md bg-blue-100 px-2 py-1 font-medium text-blue-800 dark:bg-blue-950 dark:text-blue-300">AI recommends</span>
          <span className="text-zinc-400">→</span>
          <span className="rounded-md bg-purple-100 px-2 py-1 font-medium text-purple-800 dark:bg-purple-950 dark:text-purple-300">Policy decides</span>
          <span className="text-zinc-400">→</span>
          <span className="rounded-md bg-emerald-100 px-2 py-1 font-medium text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300">Infrastructure executes</span>
          <span className="text-zinc-400">→</span>
          <span className="rounded-md bg-zinc-100 px-2 py-1 font-medium text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300">Webhooks/reconciliation verify</span>
        </div>
        <p className="mt-3 text-zinc-600 dark:text-zinc-400">
          AI recommendations never directly authorize financial actions. Every ALLOW/BLOCK/ESCALATE decision below comes
          from the deterministic Policy Engine, never from the AI diagnosis alone.
        </p>
      </section>

      <section>
        <h2 className="mb-3 text-sm font-semibold text-black dark:text-zinc-50">Most recently updated cases</h2>
        <div className="overflow-x-auto rounded-lg border border-black/[.08] bg-white dark:border-white/[.1] dark:bg-zinc-950">
          <table className="min-w-full text-sm">
            <thead>
              <tr className="border-b border-black/[.08] text-left text-xs uppercase text-zinc-500 dark:border-white/[.1] dark:text-zinc-400">
                <th className="px-4 py-2">Case</th>
                <th className="px-4 py-2">Status</th>
                <th className="px-4 py-2">Amount</th>
                <th className="px-4 py-2">Policy</th>
                <th className="px-4 py-2">Outcome</th>
              </tr>
            </thead>
            <tbody>
              {cases.slice(0, 10).map((c) => (
                <tr key={c.id} className="border-b border-black/[.04] last:border-0 dark:border-white/[.06]">
                  <td className="px-4 py-2">
                    <Link href={`/recovery-cases/${c.id}`} className="font-mono text-xs text-blue-700 hover:underline dark:text-blue-400">
                      {c.id.slice(0, 8)}
                    </Link>
                  </td>
                  <td className="px-4 py-2"><StatusBadge status={c.status} /></td>
                  <td className="px-4 py-2 tabular-nums">{formatMoney(c.revenue_at_risk_minor_units, c.currency)}</td>
                  <td className="px-4 py-2">{c.policy_decision ? <StatusBadge status={c.policy_decision} /> : "—"}</td>
                  <td className="px-4 py-2">{c.outcome_status ? <StatusBadge status={c.outcome_status} /> : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}

function sum<T>(items: T[], selector: (item: T) => number): number {
  return items.reduce((total, item) => total + selector(item), 0);
}
