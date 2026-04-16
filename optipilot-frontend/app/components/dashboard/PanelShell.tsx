import type { ReactNode } from "react";

interface PanelShellProps {
  title: string;
  subtitle?: string;
  children: ReactNode;
  className?: string;
}

export function PanelShell({
  title,
  subtitle,
  children,
  className = "",
}: PanelShellProps) {
  return (
    <section
      className={`rounded-xl border border-zinc-800 bg-[#12171f] p-4 shadow-[0_0_0_1px_rgba(17,24,39,0.2)] ${className}`}
    >
      <header className="mb-4 flex items-start justify-between gap-3 border-b border-zinc-800 pb-3">
        <div>
          <h2 className="text-sm font-semibold uppercase tracking-wide text-zinc-200">
            {title}
          </h2>
          {subtitle ? (
            <p className="mt-1 text-xs text-zinc-400">{subtitle}</p>
          ) : null}
        </div>
      </header>
      {children}
    </section>
  );
}
