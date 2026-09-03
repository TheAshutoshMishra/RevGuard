"use client";

import { useEffect, useState } from "react";

// Minimal, read-only presentation of RevGuard's Milestone 8-10
// evaluation results. This page renders no evaluation logic of its own:
// every number comes from a live GET /v1/evaluation call to the Go
// backend (service.RunEvaluation), which is the sole authority for how
// these figures are computed. See docs/architecture/evaluation-engine.md.

type StrategyMetrics = {
  name: string;
  revenue_recovered_minor_units: number;
  recovery_rate: number;
  expected_recovery_value_minor_units: number;
  net_incremental_value_minor_units: number;
  actions_taken: number;
  actions_blocked: number;
  human_escalations: number;
  unsupported_actions: number;
  ambiguous_outcomes: number;
  unnecessary_actions: number;
  average_attempts: number;
  currency: string;
};

type ComparisonResult = {
  profile_name: string;
  baseline_name: string;
  incremental_recovered_revenue_minor_units: number;
  incremental_net_value_minor_units: number;
  action_reduction_percent: number;
  incremental_recovery_rate: number;
};

type EvaluationResult = {
  dataset: {
    seed: number;
    opportunities: number;
    type: string;
    revenue_at_risk_minor_units: number;
    potentially_recoverable_revenue_minor_units: number;
    currency: string;
  };
  strategies: Record<string, StrategyMetrics>;
  comparisons: Record<string, ComparisonResult>;
  disclaimer: string;
};

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

function formatMoney(minorUnits: number, currency: string): string {
  return `${(minorUnits / 100).toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })} ${currency}`;
}

export default function EvaluationPage() {
  const [result, setResult] = useState<EvaluationResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const apiUrl = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
    fetch(`${apiUrl}/v1/evaluation`)
      .then((res) => {
        if (!res.ok) throw new Error(`request failed: ${res.status}`);
        return res.json();
      })
      .then(setResult)
      .catch((err) => setError(String(err)));
  }, []);

  return (
    <div className="flex flex-1 flex-col gap-6 bg-zinc-50 p-8 font-sans dark:bg-black">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight text-black dark:text-zinc-50">
          RevGuard Evaluation
        </h1>
        <span className="w-fit rounded-full border border-black/[.08] px-3 py-1 text-xs font-medium text-zinc-500 dark:border-white/[.145] dark:text-zinc-400">
          Synthetic evaluation — not production performance
        </span>
      </header>

      {error && (
        <p className="text-sm text-red-600 dark:text-red-400">
          Failed to load evaluation results: {error}
        </p>
      )}

      {!result && !error && (
        <p className="text-sm text-zinc-500 dark:text-zinc-400">Loading…</p>
      )}

      {result && (
        <>
          <section className="text-sm text-zinc-600 dark:text-zinc-400">
            <p>
              Seed <code>{result.dataset.seed}</code> · {result.dataset.opportunities}{" "}
              synthetic opportunities · dataset type: <code>{result.dataset.type}</code>
            </p>
            <p>
              Revenue At Risk: {formatMoney(result.dataset.revenue_at_risk_minor_units, result.dataset.currency)}
              {" · "}
              Potentially Recoverable: {formatMoney(result.dataset.potentially_recoverable_revenue_minor_units, result.dataset.currency)}
            </p>
          </section>

          <section className="overflow-x-auto">
            <h2 className="mb-2 text-lg font-medium text-black dark:text-zinc-50">
              Strategy comparison
            </h2>
            <table className="min-w-full border-collapse text-sm">
              <thead>
                <tr className="border-b border-black/[.08] text-left dark:border-white/[.145]">
                  <th className="py-2 pr-4">Strategy</th>
                  <th className="py-2 pr-4">Recovered</th>
                  <th className="py-2 pr-4">Recovery Rate</th>
                  <th className="py-2 pr-4">Actions Taken</th>
                  <th className="py-2 pr-4">Blocked</th>
                  <th className="py-2 pr-4">Escalated</th>
                  <th className="py-2 pr-4">Unsupported</th>
                  <th className="py-2 pr-4">Unnecessary</th>
                  <th className="py-2 pr-4">Net Value</th>
                </tr>
              </thead>
              <tbody>
                {STRATEGY_ORDER.filter((name) => result.strategies[name]).map((name) => {
                  const m = result.strategies[name];
                  return (
                    <tr key={name} className="border-b border-black/[.04] dark:border-white/[.08]">
                      <td className="py-2 pr-4 font-mono">{m.name}</td>
                      <td className="py-2 pr-4">{formatMoney(m.revenue_recovered_minor_units, m.currency)}</td>
                      <td className="py-2 pr-4">{(m.recovery_rate * 100).toFixed(2)}%</td>
                      <td className="py-2 pr-4">{m.actions_taken}</td>
                      <td className="py-2 pr-4">{m.actions_blocked}</td>
                      <td className="py-2 pr-4">{m.human_escalations}</td>
                      <td className="py-2 pr-4">{m.unsupported_actions}</td>
                      <td className="py-2 pr-4">{m.unnecessary_actions}</td>
                      <td className="py-2 pr-4">{formatMoney(m.net_incremental_value_minor_units, m.currency)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </section>

          <section className="overflow-x-auto">
            <h2 className="mb-2 text-lg font-medium text-black dark:text-zinc-50">
              RevGuard profile vs. baseline (incremental)
            </h2>
            <table className="min-w-full border-collapse text-sm">
              <thead>
                <tr className="border-b border-black/[.08] text-left dark:border-white/[.145]">
                  <th className="py-2 pr-4">Profile</th>
                  <th className="py-2 pr-4">Baseline</th>
                  <th className="py-2 pr-4">Incremental Recovered Revenue</th>
                  <th className="py-2 pr-4">Incremental Net Value</th>
                  <th className="py-2 pr-4">Action Reduction %</th>
                </tr>
              </thead>
              <tbody>
                {COMPARISON_ORDER.filter((key) => result.comparisons[key]).map((key) => {
                  const c = result.comparisons[key];
                  return (
                    <tr key={key} className="border-b border-black/[.04] dark:border-white/[.08]">
                      <td className="py-2 pr-4 font-mono">{c.profile_name}</td>
                      <td className="py-2 pr-4 font-mono">{c.baseline_name}</td>
                      <td className="py-2 pr-4">
                        {formatMoney(c.incremental_recovered_revenue_minor_units, result.dataset.currency)}
                      </td>
                      <td className="py-2 pr-4">
                        {formatMoney(c.incremental_net_value_minor_units, result.dataset.currency)}
                      </td>
                      <td className="py-2 pr-4">{c.action_reduction_percent.toFixed(2)}%</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </section>

          <p className="text-xs text-zinc-500 dark:text-zinc-400">{result.disclaimer}</p>
        </>
      )}
    </div>
  );
}
