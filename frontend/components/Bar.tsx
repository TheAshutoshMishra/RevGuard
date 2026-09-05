// A minimal, dependency-free horizontal bar — no chart library for what
// a handful of divs communicate just as clearly. `value`/`max` come
// directly from real API data; this component never invents a number.
export function Bar({
  label,
  value,
  max,
  formattedValue,
  colorClassName = "bg-blue-600",
}: {
  label: string;
  value: number;
  max: number;
  formattedValue: string;
  colorClassName?: string;
}) {
  const pct = max > 0 ? Math.max(0, Math.min(100, (value / max) * 100)) : 0;
  return (
    <div className="flex items-center gap-3 text-sm">
      <span className="w-40 shrink-0 truncate font-mono text-xs text-zinc-600 dark:text-zinc-400">{label}</span>
      <div className="h-4 flex-1 overflow-hidden rounded bg-zinc-100 dark:bg-zinc-800">
        <div className={`h-full rounded ${colorClassName}`} style={{ width: `${pct}%` }} />
      </div>
      <span className="w-32 shrink-0 text-right font-mono text-xs tabular-nums text-zinc-700 dark:text-zinc-300">
        {formattedValue}
      </span>
    </div>
  );
}
