import type { ServerInfo } from "@/lib/api";

function Row({ label, value, tone }: { label: string; value: string; tone?: "ok" | "warn" }) {
  const color = tone === "ok" ? "text-accent" : tone === "warn" ? "text-warning" : "text-foreground";
  return (
    <div className="flex justify-between gap-4 py-1.5">
      <span className="text-muted">{label}</span>
      <span className={`selectable truncate text-right font-mono ${color}`}>{value}</span>
    </div>
  );
}

export function ServerInfoPanel({ info }: { info: ServerInfo }) {
  return (
    <div className="divide-y divide-border text-[13px]">
      <Row label="Hostname" value={info.hostname || "—"} />
      <Row label="OS" value={info.os || "—"} />
      <Row label="Kernel" value={`${info.kernel} (${info.arch})`} />
      <Row label="Uptime" value={info.uptime || "—"} />
      <Row
        label="Docker"
        value={info.dockerVersion || "not installed"}
        tone={info.dockerVersion ? "ok" : "warn"}
      />
      <Row
        label="Privileges"
        value={info.isRoot ? "root" : info.hasSudo ? "sudo (passwordless)" : "no passwordless sudo"}
        tone={info.isRoot || info.hasSudo ? "ok" : "warn"}
      />
      <Row label="Latency" value={`${info.latencyMs} ms`} />
      <Row label="Host key" value={info.fingerprint} />
    </div>
  );
}
