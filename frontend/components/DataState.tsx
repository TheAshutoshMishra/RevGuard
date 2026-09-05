import { ApiError } from "@/lib/api";

// The single loading/error/empty presentation every page uses, so no
// page invents its own ad-hoc "still loading" text or swallows a fetch
// failure. Errors always show the endpoint and a retry action — never a
// silent blank page.

export function LoadingState({ label = "Loading…" }: { label?: string }) {
  return (
    <div className="flex items-center gap-2 py-8 text-sm text-zinc-500 dark:text-zinc-400">
      <span className="h-3 w-3 animate-pulse rounded-full bg-zinc-400 dark:bg-zinc-600" />
      {label}
    </div>
  );
}

export function ErrorState({ error, onRetry }: { error: ApiError; onRetry: () => void }) {
  return (
    <div className="rounded-md border border-red-200 bg-red-50 p-4 text-sm dark:border-red-900 dark:bg-red-950/40">
      <p className="font-medium text-red-800 dark:text-red-300">
        Failed to load data from <code>{error.endpoint}</code>
      </p>
      <p className="mt-1 text-red-700 dark:text-red-400">{error.message}</p>
      <button
        onClick={onRetry}
        className="mt-3 rounded-md border border-red-300 bg-white px-3 py-1 text-xs font-medium text-red-800 hover:bg-red-100 dark:border-red-800 dark:bg-transparent dark:text-red-300 dark:hover:bg-red-900/40"
      >
        Retry
      </button>
    </div>
  );
}

export function EmptyState({ label }: { label: string }) {
  return <p className="py-8 text-sm text-zinc-500 dark:text-zinc-400">{label}</p>;
}
