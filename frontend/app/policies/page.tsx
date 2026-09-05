"use client";

import { useApi, formatMoney } from "@/lib/api";
import type { PoliciesResponse, PolicyProfile } from "@/lib/types";
import { LoadingState, ErrorState } from "@/components/DataState";

const PROFILE_DESCRIPTIONS: Record<string, string> = {
  conservative: "Lower risk tolerance, more human escalation. Favors review over automation.",
  balanced: "RevGuard's default trade-off — the original policy-v1 thresholds, unchanged.",
  aggressive: "Higher automation, higher recovery opportunity — and higher exposure per automated decision.",
};

export default function PoliciesPage() {
  const { data, loading, error, refetch } = useApi<PoliciesResponse>("/v1/policies");

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight text-black dark:text-zinc-50">Policies</h1>
        <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
          Read-only. AI does not modify policy. Policy is deterministic — the same rules run for every profile; only
          the threshold values differ. There is no API on this page (or anywhere in RevGuard) that can bypass a
          safety control.
        </p>
      </div>

      {loading && <LoadingState label="Loading policy profiles…" />}
      {error && <ErrorState error={error} onRetry={refetch} />}
      {!loading && !error && data && (
        <div className="grid gap-4 lg:grid-cols-3">
          {data.profiles.map((p) => (
            <ProfileCard key={p.key} profile={p} currency={data.currency} />
          ))}
        </div>
      )}

      {data && (
        <p className="rounded-md bg-zinc-100 p-3 text-xs text-zinc-600 dark:bg-zinc-900 dark:text-zinc-400">{data.note}</p>
      )}
    </div>
  );
}

function ProfileCard({ profile, currency }: { profile: PolicyProfile; currency: string }) {
  return (
    <div className="rounded-lg border border-black/[.08] bg-white p-4 dark:border-white/[.1] dark:bg-zinc-950">
      <h2 className="text-sm font-semibold capitalize text-black dark:text-zinc-50">{profile.key}</h2>
      <p className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">{PROFILE_DESCRIPTIONS[profile.key] ?? ""}</p>
      <dl className="mt-4 flex flex-col gap-2 text-sm">
        <Row label="Minimum AI confidence" value={profile.minimum_confidence.toFixed(2)} />
        <Row label="Max automatic amount" value={formatMoney(profile.max_auto_amount_minor_units, currency)} />
        <Row label="Min expected incremental value" value={formatMoney(profile.minimum_expected_incremental_value_minor_units, currency)} />
        <Row label="Max payment attempts" value={String(profile.max_payment_attempts)} />
        <Row label="Max prior recovery actions" value={String(profile.max_prior_recovery_actions)} />
      </dl>
      <div className="mt-4">
        <p className="text-xs uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Allowed actions</p>
        <div className="mt-1 flex flex-wrap gap-1">
          {profile.auto_allowed_actions.map((a) => (
            <span key={a} className="rounded-md bg-zinc-100 px-2 py-0.5 text-xs font-mono text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300">
              {a}
            </span>
          ))}
        </div>
      </div>
      <p className="mt-4 text-xs text-zinc-400 dark:text-zinc-600">version: {profile.version}</p>
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between">
      <dt className="text-zinc-500 dark:text-zinc-400">{label}</dt>
      <dd className="font-mono text-black dark:text-zinc-50">{value}</dd>
    </div>
  );
}
