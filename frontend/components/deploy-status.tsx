const styles: Record<string, string> = {
  success: "bg-accent/10 text-accent",
  running: "bg-sky-400/10 text-sky-300",
  failed: "bg-danger/10 text-danger",
  cancelled: "bg-surface-2 text-muted",
};
const labels: Record<string, string> = {
  success: "Live",
  running: "Deploying",
  failed: "Failed",
  cancelled: "Cancelled",
};

export function DeployStatus({ status }: { status: string }) {
  return (
    <span className={`inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 text-[11px] font-medium ${styles[status] ?? styles.cancelled}`}>
      {status === "running" && <span className="size-1.5 animate-pulse rounded-full bg-current" />}
      {labels[status] ?? status}
    </span>
  );
}
