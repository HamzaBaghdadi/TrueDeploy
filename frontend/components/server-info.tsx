import {
  CircleCheck,
  Clock,
  Container,
  Cpu,
  Fingerprint,
  Gauge,
  Monitor,
  ShieldCheck,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";
import type { ServerInfo } from "@/lib/api";
import { CopyButton } from "@/components/copy-button";

function Row({
  icon: Icon,
  label,
  value,
  tone,
  copy,
}: {
  icon: LucideIcon;
  label: string;
  value: string;
  tone?: "ok" | "warn";
  copy?: boolean;
}) {
  const color = tone === "ok" ? "text-success" : tone === "warn" ? "text-warning" : "text-foreground";
  return (
    <div className="flex items-center justify-between gap-4 py-1.5">
      <span className="flex items-center gap-2 text-muted-foreground">
        <Icon className="size-3.5" />
        {label}
      </span>
      <span className={`flex min-w-0 items-center gap-1.5 ${color}`}>
        {tone === "ok" && <CircleCheck className="size-3.5 shrink-0" />}
        {tone === "warn" && <TriangleAlert className="size-3.5 shrink-0" />}
        <span className="selectable truncate text-right font-mono">{value}</span>
        {copy && <CopyButton text={value} />}
      </span>
    </div>
  );
}

export function ServerInfoPanel({ info }: { info: ServerInfo }) {
  return (
    <div className="divide-y divide-border text-[13px]">
      <Row icon={Monitor} label="Hostname" value={info.hostname || "—"} />
      <Row icon={Monitor} label="OS" value={info.os || "—"} />
      <Row icon={Cpu} label="Kernel" value={`${info.kernel} (${info.arch})`} />
      <Row icon={Clock} label="Uptime" value={info.uptime || "—"} />
      <Row
        icon={Container}
        label="Docker"
        value={info.dockerVersion || "not installed"}
        tone={info.dockerVersion ? "ok" : "warn"}
      />
      <Row
        icon={ShieldCheck}
        label="Privileges"
        value={info.isRoot ? "root" : info.hasSudo ? "sudo (passwordless)" : "no passwordless sudo"}
        tone={info.isRoot || info.hasSudo ? "ok" : "warn"}
      />
      <Row icon={Gauge} label="Latency" value={`${info.latencyMs} ms`} />
      <Row icon={Fingerprint} label="Host key" value={info.fingerprint} copy />
    </div>
  );
}
