export default function Home() {
  return (
    <div className="flex flex-1 flex-col items-center justify-center bg-zinc-50 font-sans dark:bg-black">
      <main className="flex flex-col items-center gap-4 text-center">
        <h1 className="text-4xl font-semibold tracking-tight text-black dark:text-zinc-50">
          RevGuard
        </h1>
        <p className="text-lg text-zinc-600 dark:text-zinc-400">
          AI Revenue Recovery Control Plane
        </p>
        <span className="rounded-full border border-black/[.08] px-4 py-1 text-sm font-medium text-zinc-500 dark:border-white/[.145] dark:text-zinc-400">
          Foundation Ready
        </span>
      </main>
    </div>
  );
}
