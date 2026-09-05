"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const LINKS = [
  { href: "/", label: "Overview" },
  { href: "/recovery-cases", label: "Recovery Cases" },
  { href: "/evaluation", label: "Evaluation" },
  { href: "/policies", label: "Policies" },
  { href: "/system-health", label: "System Health" },
];

export function Nav() {
  const pathname = usePathname();
  return (
    <header className="border-b border-black/[.08] bg-white dark:border-white/[.1] dark:bg-zinc-950">
      <div className="mx-auto flex max-w-7xl items-center gap-6 px-6 py-3">
        <span className="text-sm font-semibold tracking-tight text-black dark:text-zinc-50">
          RevGuard
        </span>
        <nav className="flex gap-1 text-sm">
          {LINKS.map((link) => {
            const active = link.href === "/" ? pathname === "/" : pathname.startsWith(link.href);
            return (
              <Link
                key={link.href}
                href={link.href}
                className={`rounded-md px-3 py-1.5 transition-colors ${
                  active
                    ? "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900"
                    : "text-zinc-600 hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-900"
                }`}
              >
                {link.label}
              </Link>
            );
          })}
        </nav>
      </div>
    </header>
  );
}
