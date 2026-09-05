// A small set of status -> color mappings covering every real vocabulary
// value in the system (RecoveryCaseStatus, PolicyDecisionOutcome,
// RecoveryActionStatus, RecoveryOutcomeStatus, component health). An
// unrecognized value still renders (neutral gray), it never throws or
// silently disappears.
const STATUS_STYLES: Record<string, string> = {
  ALLOW: "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300",
  SUCCESS: "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300",
  SUCCEEDED: "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300",
  UP: "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300",
  CAPTURED: "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300",

  BLOCK: "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300",
  FAILED: "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300",
  DOWN: "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300",

  ESCALATE: "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",
  DEGRADED: "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",

  UNKNOWN: "bg-zinc-200 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300",
  NOT_CONFIGURED: "bg-zinc-200 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300",

  VERIFYING: "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300",
  EXECUTING: "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300",
  ANALYZING: "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300",
  ANALYZED: "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300",
  POLICY_CHECK: "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300",
  DETECTED: "bg-zinc-200 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300",
};

export function StatusBadge({ status }: { status: string }) {
  const style = STATUS_STYLES[status] ?? "bg-zinc-200 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300";
  return (
    <span className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium ${style}`}>
      {status || "—"}
    </span>
  );
}
