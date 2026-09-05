"use client";

import { useState } from "react";
import { useApi, formatMoney, formatPercent } from "@/lib/api";
import type { EvaluationResult } from "@/lib/types";
import { LoadingState, ErrorState } from "@/components/DataState";
import { Bar } from "@/components/Bar";

const STRATEGY_ORDER = [
  "fixed_retry",
  "static_rules",
  "revguard_conservative",
  "revguard_balanced",
  "revguard_aggressive",
];

const COMPARISON_ORDER = [
  "revguard_conservative_vs_fixed_retry",
  "revguard_conservative_vs_static_rules",
  "revguard_balanced_vs_fixed_retry",
  "revguard_balanced_vs_static_rules",
  "revguard_aggressive_vs_fixed_retry",
  "revguard_aggressive_vs_static_rules",
];

const STRATEGY_LABELS: Record<string, string> = {
  fixed_retry: "Fixed Retry",
  static_rules: "Static Rules",
  revguard_conservative: "RevGuard Conservative",
  revguard_balanced: "RevGuard Balanced",
  revguard_aggressive: "RevGuard Aggressive",
};

export default function EvaluationPage() {
  const [seed, setSeed] = useState(12345);
  const [cases, setCases] = useState(1000);
  const [pendingSeed, setPendingSeed] = useState("12345");
  const [pendingCases, setPendingCases] = useState("1000");

  const { data, loading, error, refetch } = useApi<EvaluationResult>(`/v1/evaluation?seed=${seed}&cases=${cases}`);

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight text-black dark:text-zinc-50">Evaluation</h1>
        <p className="mt-1 inline-block rounded-full border border-amber-300 bg-amber-50 px-3 py-1 text-xs font-medium text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-300">
          Synthetic evaluation — not production performance.
        </p>
      </div>

      <form
        className="flex flex-wrap items-end gap-3 text-sm"
        onSubmit={(e) => {
          e.preventDefault();
          setSeed(Number(pendingSeed) || 12345);
          setCases(Number(pendingCases) || 1000);
        }}
      >
        <label className="flex flex-col gap-1">
          <span className="text-xs text-zinc-500 dark:text-zinc-400">Seed</span>
          <input
            value={pendingSeed}
            onChange={(e) => setPendingSeed(e.target.value)}
            className="w-32 rounded-md border border-black/[.12] bg-white px-2 py-1 dark:border-white/[.15] dark:bg-zinc-950"
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-xs text-zinc-500 dark:text-zinc-400">Scenario count</span>
          <input
            value={pendingCases}
            onChange={(e) => setPendingCases(e.target.value)}
            className="w-32 rounded-md border border-black/[.12] bg-white px-2 py-1 dark:border-white/[.15] dark:bg-zinc-950"
          />
        </label>
        <button type="submit" className="rounded-md bg-zinc-900 px-3 py-1.5 text-white dark:bg-zinc-100 dark:text-zinc-900">
          Run evaluation
        </button>
      </form>

      {loading && <LoadingState label="Running evaluation…" />}
      {error && <ErrorState error={error} onRetry={refetch} />}
      {!loading && !error && data && <EvaluationView result={data} />}
    </div>
  );
}

function EvaluationView({ result }: { result: EvaluationResult }) {
  const currency = result.dataset.currency;
  const strategies = STRATEGY_ORDER.filter((k) => result.strategies[k]).map((k) => result.strategies[k]);
  const maxRecovered = Math.max(1, ...strategies.map((s) => s.revenue_recovered_minor_units));
  const maxActions = Math.max(1, ...strategies.map((s) => s.actions_taken));
  const maxNetValue = Math.max(1, ...strategies.map((s) => Math.abs(s.net_incremental_value_minor_units)));

  return (
    <div className="flex flex-col gap-8">
      <section className="grid grid-cols-2 gap-4 text-sm sm:grid-cols-4">
        <InfoStat label="Seed" value={String(result.dataset.seed)} />
        <InfoStat label="Scenario Count" value={String(result.dataset.opportunities)} />
        <InfoStat label="Dataset Type" value={result.dataset.type} />
        <InfoStat label="Fetched" value={new Date().toLocaleTimeString()} />
      </section>

      <section className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <InfoStat label="Revenue At Risk" value={formatMoney(result.dataset.revenue_at_risk_minor_units, currency)} />
        <InfoStat label="Potentially Recoverable" value={formatMoney(result.dataset.potentially_recoverable_revenue_minor_units, currency)} />
      </section>

      {/* Strategy comparison table */}
      <section className="overflow-x-auto">
        <h2 className="mb-3 text-sm font-semibold text-black dark:text-zinc-50">Strategy comparison</h2>
        <table className="min-w-full rounded-lg border border-black/[.08] bg-white text-sm dark:border-white/[.1] dark:bg-zinc-950">
          <thead>
            <tr className="border-b border-black/[.08] text-left text-xs uppercase text-zinc-500 dark:border-white/[.1] dark:text-zinc-400">
              <th className="px-3 py-2">Strategy</th>
              <th className="px-3 py-2">Recovered</th>
              <th className="px-3 py-2">Rate</th>
              <th className="px-3 py-2">Actions</th>
              <th className="px-3 py-2">Blocked</th>
              <th className="px-3 py-2">Escalated</th>
              <th className="px-3 py-2">Unsupported</th>
              <th className="px-3 py-2">Ambiguous</th>
              <th className="px-3 py-2">Unnecessary</th>
              <th className="px-3 py-2">Net Value</th>
              <th className="px-3 py-2">Avg Attempts</th>
            </tr>
          </thead>
          <tbody>
            {strategies.map((s) => (
              <tr key={s.name} className="border-b border-black/[.04] last:border-0 dark:border-white/[.06]">
                <td className="px-3 py-2 font-medium">{STRATEGY_LABELS[s.name] ?? s.name}</td>
                <td className="px-3 py-2 tabular-nums">{formatMoney(s.revenue_recovered_minor_units, s.currency)}</td>
                <td className="px-3 py-2 tabular-nums">{formatPercent(s.recovery_rate)}</td>
                <td className="px-3 py-2 tabular-nums">{s.actions_taken}</td>
                <td className="px-3 py-2 tabular-nums">{s.actions_blocked}</td>
                <td className="px-3 py-2 tabular-nums">{s.human_escalations}</td>
                <td className="px-3 py-2 tabular-nums">{s.unsupported_actions}</td>
                <td className="px-3 py-2 tabular-nums">{s.ambiguous_outcomes}</td>
                <td className="px-3 py-2 tabular-nums">{s.unnecessary_actions}</td>
                <td className={`px-3 py-2 tabular-nums ${s.net_incremental_value_minor_units < 0 ? "text-red-600 dark:text-red-400" : ""}`}>
                  {formatMoney(s.net_incremental_value_minor_units, s.currency)}
                </td>
                <td className="px-3 py-2 tabular-nums">{s.average_attempts.toFixed(2)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      {/* Recovered revenue comparison */}
      <ChartSection title="Recovered revenue comparison">
        {strategies.map((s) => (
          <Bar key={s.name} label={STRATEGY_LABELS[s.name] ?? s.name} value={s.revenue_recovered_minor_units} max={maxRecovered} formattedValue={formatMoney(s.revenue_recovered_minor_units, s.currency)} />
        ))}
      </ChartSection>

      {/* Action volume comparison */}
      <ChartSection title="Action volume comparison">
        {strategies.map((s) => (
          <Bar key={s.name} label={STRATEGY_LABELS[s.name] ?? s.name} value={s.actions_taken} max={maxActions} formattedValue={String(s.actions_taken)} colorClassName="bg-purple-600" />
        ))}
      </ChartSection>

      {/* Safety/outcome comparison */}
      <section>
        <h2 className="mb-3 text-sm font-semibold text-black dark:text-zinc-50">Safety / outcome comparison</h2>
        <div className="flex flex-col gap-3 rounded-lg border border-black/[.08] bg-white p-4 dark:border-white/[.1] dark:bg-zinc-950">
          {strategies.map((s) => {
            const total = s.actions_taken + s.actions_blocked + s.human_escalations || 1;
            return (
              <div key={s.name} className="flex items-center gap-3 text-xs">
                <span className="w-40 shrink-0 font-mono text-zinc-600 dark:text-zinc-400">{STRATEGY_LABELS[s.name] ?? s.name}</span>
                <div className="flex h-4 flex-1 overflow-hidden rounded bg-zinc-100 dark:bg-zinc-800">
                  <div className="h-full bg-emerald-500" style={{ width: `${(s.actions_taken / total) * 100}%` }} title={`${s.actions_taken} taken`} />
                  <div className="h-full bg-red-500" style={{ width: `${(s.actions_blocked / total) * 100}%` }} title={`${s.actions_blocked} blocked`} />
                  <div className="h-full bg-amber-500" style={{ width: `${(s.human_escalations / total) * 100}%` }} title={`${s.human_escalations} escalated`} />
                </div>
                <span className="w-56 shrink-0 tabular-nums text-zinc-600 dark:text-zinc-400">
                  {s.actions_taken} taken · {s.actions_blocked} blocked · {s.human_escalations} escalated
                </span>
              </div>
            );
          })}
        </div>
      </section>

      {/* Net value comparison */}
      <ChartSection title="Net economic value comparison">
        {strategies.map((s) => (
          <Bar
            key={s.name}
            label={STRATEGY_LABELS[s.name] ?? s.name}
            value={Math.abs(s.net_incremental_value_minor_units)}
            max={maxNetValue}
            formattedValue={formatMoney(s.net_incremental_value_minor_units, s.currency)}
            colorClassName={s.net_incremental_value_minor_units < 0 ? "bg-red-500" : "bg-blue-600"}
          />
        ))}
      </ChartSection>

      {/* Comparison + trade-off narrative */}
      <section>
        <h2 className="mb-3 text-sm font-semibold text-black dark:text-zinc-50">RevGuard profile vs. baseline trade-offs</h2>
        <div className="flex flex-col gap-2">
          {COMPARISON_ORDER.filter((k) => result.comparisons[k]).map((key) => {
            const c = result.comparisons[key];
            return (
              <div key={key} className="rounded-lg border border-black/[.08] bg-white p-3 text-sm dark:border-white/[.1] dark:bg-zinc-950">
                <p className="font-medium text-black dark:text-zinc-50">
                  {STRATEGY_LABELS[c.profile_name] ?? c.profile_name} vs. {STRATEGY_LABELS[c.baseline_name] ?? c.baseline_name}
                </p>
                <p className="mt-1 text-zinc-600 dark:text-zinc-400">{tradeOffSentence(c, currency)}</p>
              </div>
            );
          })}
        </div>
      </section>

      <p className="rounded-md bg-zinc-100 p-3 text-xs text-zinc-600 dark:bg-zinc-900 dark:text-zinc-400">{result.disclaimer}</p>
    </div>
  );
}

function tradeOffSentence(
  c: EvaluationResult["comparisons"][string],
  currency: string
): string {
  const profile = STRATEGY_LABELS[c.profile_name] ?? c.profile_name;
  const baseline = STRATEGY_LABELS[c.baseline_name] ?? c.baseline_name;
  const revenueVerb = c.incremental_recovered_revenue_minor_units >= 0 ? "recovers more revenue than" : "recovers less revenue than";
  const revenueAmount = formatMoney(Math.abs(c.incremental_recovered_revenue_minor_units), currency);
  const actionClause =
    c.action_reduction_percent > 0
      ? `uses ${c.action_reduction_percent.toFixed(0)}% fewer actions`
      : c.action_reduction_percent < 0
      ? `uses ${Math.abs(c.action_reduction_percent).toFixed(0)}% more actions`
      : "uses the same number of actions";
  const netVerb = c.incremental_net_value_minor_units >= 0 ? "a higher" : "a lower";
  return `${profile} ${revenueVerb} ${baseline} by ${revenueAmount}, while it ${actionClause}. Net economic value is ${netVerb} than ${baseline} by ${formatMoney(Math.abs(c.incremental_net_value_minor_units), currency)}.`;
}

function InfoStat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-xs uppercase tracking-wide text-zinc-500 dark:text-zinc-400">{label}</p>
      <p className="font-mono text-sm text-black dark:text-zinc-50">{value}</p>
    </div>
  );
}

function ChartSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section>
      <h2 className="mb-3 text-sm font-semibold text-black dark:text-zinc-50">{title}</h2>
      <div className="flex flex-col gap-1.5 rounded-lg border border-black/[.08] bg-white p-4 dark:border-white/[.1] dark:bg-zinc-950">
        {children}
      </div>
    </section>
  );
}
