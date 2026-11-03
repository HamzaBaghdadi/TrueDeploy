const nav = [
  { label: "Servers", active: true },
  { label: "Projects", soon: true },
  { label: "Deployments", soon: true },
  { label: "Settings", soon: true },
];

export function Sidebar() {
  return (
    <aside className="flex w-56 shrink-0 flex-col border-r border-border bg-surface">
      <div className="flex items-center gap-2 px-5 py-5">
        <div className="grid size-7 place-items-center rounded-md bg-accent font-mono text-sm font-bold text-accent-ink">
          T
        </div>
        <span className="text-[15px] font-semibold tracking-tight">TrueDeploy</span>
      </div>
      <nav className="flex flex-col gap-0.5 px-3">
        {nav.map((item) => (
          <div
            key={item.label}
            className={`flex items-center justify-between rounded-md px-3 py-2 text-sm ${
              item.active ? "bg-surface-2 text-foreground" : "text-muted"
            }`}
          >
            {item.label}
            {item.soon && (
              <span className="rounded bg-surface-2 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted">
                soon
              </span>
            )}
          </div>
        ))}
      </nav>
      <div className="mt-auto px-5 py-4 font-mono text-[11px] text-muted">v0.1.0</div>
    </aside>
  );
}
