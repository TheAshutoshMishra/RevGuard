"use client";

import Link from "next/link";
import { useApi, formatMoney, formatDateTime } from "@/lib/api";
import type { RecoveryCaseDetail } from "@/lib/types";
import { LoadingState, ErrorState } from "@/components/DataState";
import { StatusBadge } from "@/components/StatusBadge";

const LIFECYCLE = [
  "DETECTED", "ANALYZING", "ANALYZED", "POLICY_CHECK",
  "ALLOW", "EXECUTING", "VERIFYING", "SUCCESS",
];

export function RecoveryCaseDetailClient({ id }: { id: string }) {
  const { data, loading, error, refetch } = useApi<RecoveryCaseDetail>(`/v1/recovery-cases/${id}`);

  if (loading) return <LoadingState label="Loading case detail…" />;
  if (error) return <ErrorState error={error} onRetry={refetch} />;
  if (!data) return null;

  const { case: c, diagnoses, economic_evaluation, policy_decision, actions, audit_trail } = data;
  const latestDiagnosis = diagnoses[0];

  return (
    <div className="flex flex-col gap-6">
      <div>
        <Link href="/recovery-cases" className="text-xs text-blue-700 hover:underline dark:text-blue-400">
          ← All recovery cases
        </Link>
      </div>

      {/* Case header */}
      <section className="rounded-lg border border-black/[.08] bg-white p-4 dark:border-white/[.1] dark:bg-zinc-950">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="font-mono text-sm text-black dark:text-zinc-50">{c.id}</h1>
          <StatusBadge status={c.status} />
        </div>
        <dl className="mt-3 grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4">
          <Field label="Payment ID" value={c.payment_id} mono />
          <Field label="Customer ID" value={c.customer_id} mono />
          <Field label="Amount" value={formatMoney(c.revenue_at_risk_minor_units, c.currency)} />
          <Field label="Currency" value={c.currency} />
          <Field label="Created" value={formatDateTime(c.created_at)} />
          <Field label="Updated" value={formatDateTime(c.updated_at)} />
        </dl>
      </section>

      {/* Recovery timeline */}
      <section>
        <h2 className="mb-3 text-sm font-semibold text-black dark:text-zinc-50">Recovery timeline</h2>
        <div className="flex flex-wrap items-center gap-1 rounded-lg border border-black/[.08] bg-white p-4 dark:border-white/[.1] dark:bg-zinc-950">
          {LIFECYCLE.map((stage, i) => {
            const reached = LIFECYCLE.indexOf(c.status) >= i || c.status === stage || ["BLOCK", "ESCALATE", "FAILED", "UNKNOWN"].includes(c.status);
            const isCurrent = c.status === stage;
            return (
              <div key={stage} className="flex items-center gap-1">
                <span
                  className={`rounded-md px-2 py-1 text-xs font-medium ${
                    isCurrent
                      ? "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900"
                      : reached
                      ? "bg-zinc-200 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300"
                      : "bg-zinc-50 text-zinc-400 dark:bg-zinc-900 dark:text-zinc-600"
                  }`}
                >
                  {stage}
                </span>
                {i < LIFECYCLE.length - 1 && <span className="text-zinc-300 dark:text-zinc-700">→</span>}
              </div>
            );
          })}
          {["BLOCK", "ESCALATE", "FAILED", "UNKNOWN"].includes(c.status) && (
            <>
              <span className="text-zinc-300 dark:text-zinc-700">→</span>
              <span className="rounded-md bg-zinc-900 px-2 py-1 text-xs font-medium text-white dark:bg-zinc-100 dark:text-zinc-900">{c.status}</span>
            </>
          )}
        </div>
      </section>

      <div className="grid gap-6 lg:grid-cols-2">
        {/* AI diagnosis */}
        <section className="rounded-lg border border-black/[.08] bg-white p-4 dark:border-white/[.1] dark:bg-zinc-950">
          <h2 className="mb-1 text-sm font-semibold text-black dark:text-zinc-50">AI Diagnosis</h2>
          <p className="mb-3 text-xs text-amber-700 dark:text-amber-400">AI recommendation ≠ authorization — see Policy Decision below.</p>
          {!latestDiagnosis ? (
            <p className="text-sm text-zinc-500 dark:text-zinc-400">No diagnosis recorded for this case yet.</p>
          ) : (
            <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
              <Field label="Failure Category" value={latestDiagnosis.failure_category} />
              <Field label="Recommendation" value={latestDiagnosis.recommended_action} />
              <Field label="Confidence" value={latestDiagnosis.confidence.toFixed(2)} />
              <Field label="Provider / Model" value={`${latestDiagnosis.provider} / ${latestDiagnosis.model}`} />
              <Field label="Prompt Version" value={latestDiagnosis.prompt_version} />
              <Field label="Risk Flags" value={latestDiagnosis.risk_flags.length ? latestDiagnosis.risk_flags.join(", ") : "none"} />
              <div className="col-span-2">
                <Field label="Explanation" value={latestDiagnosis.explanation} />
              </div>
            </dl>
          )}
        </section>

        {/* Economic evaluation */}
        <section className="rounded-lg border border-black/[.08] bg-white p-4 dark:border-white/[.1] dark:bg-zinc-950">
          <h2 className="mb-3 text-sm font-semibold text-black dark:text-zinc-50">Economic Evaluation</h2>
          {!economic_evaluation ? (
            <p className="text-sm text-zinc-500 dark:text-zinc-400">No economic evaluation recorded for this case yet.</p>
          ) : (
            <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
              <Field label="Revenue At Risk" value={formatMoney(economic_evaluation.revenue_at_risk_minor_units, economic_evaluation.currency)} />
              <Field label="Recovery Probability" value={`${(economic_evaluation.recovery_probability_bps / 100).toFixed(1)}%`} />
              <Field label="Expected Gross Recovery" value={formatMoney(economic_evaluation.expected_gross_recovery_minor_units, economic_evaluation.currency)} />
              <Field label="Action Cost" value={formatMoney(economic_evaluation.action_cost_minor_units, economic_evaluation.currency)} />
              <Field label="Risk Cost" value={formatMoney(economic_evaluation.risk_cost_minor_units, economic_evaluation.currency)} />
              <Field
                label="Expected Incremental Value"
                value={`${economic_evaluation.expected_incremental_value_minor_units >= 0 ? "" : "-"}${formatMoney(Math.abs(economic_evaluation.expected_incremental_value_minor_units), economic_evaluation.currency)}`}
              />
              <div className="col-span-2 text-xs text-zinc-500 dark:text-zinc-400">
                formula: expected_gross_recovery − action_cost − risk_cost ({economic_evaluation.estimator_name} {economic_evaluation.estimator_version}, {economic_evaluation.economic_model_version})
              </div>
            </dl>
          )}
        </section>

        {/* Policy decision */}
        <section className="rounded-lg border border-black/[.08] bg-white p-4 dark:border-white/[.1] dark:bg-zinc-950">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-sm font-semibold text-black dark:text-zinc-50">Policy Decision</h2>
            {policy_decision && <StatusBadge status={policy_decision.decision} />}
          </div>
          {!policy_decision ? (
            <p className="text-sm text-zinc-500 dark:text-zinc-400">No policy decision recorded for this case yet.</p>
          ) : (
            <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
              <Field label="Policy Version" value={policy_decision.policy_version} />
              <Field label="Authorized Action" value={policy_decision.authorized_action || "none"} />
              <div className="col-span-2">
                <Field label="Reason Codes" value={policy_decision.reason_codes.join(", ")} />
              </div>
              <div className="col-span-2">
                <Field label="Explanation" value={policy_decision.explanation} />
              </div>
            </dl>
          )}
        </section>

        {/* Execution */}
        <section className="rounded-lg border border-black/[.08] bg-white p-4 dark:border-white/[.1] dark:bg-zinc-950">
          <h2 className="mb-3 text-sm font-semibold text-black dark:text-zinc-50">Execution &amp; Financial Outcome</h2>
          {actions.length === 0 ? (
            <p className="text-sm text-zinc-500 dark:text-zinc-400">No execution attempted for this case (BLOCK/ESCALATE never execute).</p>
          ) : (
            <div className="flex flex-col gap-4">
              {actions.map((a) => (
                <div key={a.id} className="rounded-md border border-black/[.06] p-3 dark:border-white/[.08]">
                  <div className="flex items-center justify-between">
                    <span className="font-mono text-xs text-zinc-500 dark:text-zinc-400">{a.action_type}</span>
                    <StatusBadge status={a.status} />
                  </div>
                  <dl className="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
                    <Field label="Provider" value={a.provider} />
                    <Field label="Provider Reference" value={a.provider_reference || "—"} mono />
                    <Field label="Attempt #" value={String(a.attempt_number)} />
                    <Field label="Error Code" value={a.error_code || "none"} />
                    <Field label="Requested At" value={formatDateTime(a.requested_at)} />
                    <Field label="Executed At" value={a.executed_at ? formatDateTime(a.executed_at) : "—"} />
                  </dl>
                  <div className="mt-3 border-t border-black/[.06] pt-2 dark:border-white/[.08]">
                    <p className="mb-1 text-xs font-medium text-zinc-500 dark:text-zinc-400">
                      Financial outcome — recovered revenue is only counted after provider-confirmed financial truth
                    </p>
                    {!a.outcome ? (
                      <p className="text-sm text-zinc-500 dark:text-zinc-400">Not yet established (case may still be VERIFYING).</p>
                    ) : (
                      <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
                        <div className="col-span-2"><StatusBadge status={a.outcome.status} /></div>
                        <Field label="Recovered Amount" value={formatMoney(a.outcome.recovered_amount_minor_units, a.outcome.currency)} />
                        <Field label="Source" value={a.outcome.source} />
                        <Field label="Provider" value={a.outcome.provider} />
                        <Field label="Verified At" value={formatDateTime(a.outcome.observed_at)} />
                      </dl>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>

      {/* Audit timeline */}
      <section>
        <h2 className="mb-3 text-sm font-semibold text-black dark:text-zinc-50">Audit Timeline</h2>
        {audit_trail.length === 0 ? (
          <p className="text-sm text-zinc-500 dark:text-zinc-400">No audit events recorded.</p>
        ) : (
          <ol className="flex flex-col gap-3 rounded-lg border border-black/[.08] bg-white p-4 dark:border-white/[.1] dark:bg-zinc-950">
            {audit_trail.map((e) => (
              <li key={e.id} className="flex items-start gap-3 text-sm">
                <span className="w-40 shrink-0 font-mono text-xs text-zinc-500 dark:text-zinc-400">{formatDateTime(e.created_at)}</span>
                <div>
                  <span className="font-medium text-black dark:text-zinc-50">{e.event_type}</span>
                  <span className="ml-2 text-xs text-zinc-500 dark:text-zinc-400">({e.actor_type})</span>
                </div>
              </li>
            ))}
          </ol>
        )}
      </section>
    </div>
  );
}

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <dt className="text-xs uppercase tracking-wide text-zinc-500 dark:text-zinc-400">{label}</dt>
      <dd className={`text-black dark:text-zinc-50 ${mono ? "font-mono text-xs break-all" : ""}`}>{value}</dd>
    </div>
  );
}
