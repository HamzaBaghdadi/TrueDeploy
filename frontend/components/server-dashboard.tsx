"use client";

import {
  ChevronRight,
  Container as ContainerIcon,
  Cpu,
  Gauge as GaugeIcon,
  HardDrive,
  Loader2,
  MemoryStick,
  Play,
  RotateCw,
  ScrollText,
  Settings2,
  ScanSearch,
  Sparkles,
  Square,
  SquareTerminal,
  type LucideIcon,
} from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { api, type Container, type DiskUsage, type Metrics, type Server } from "@/lib/api";
import { useConfirm } from "@/components/confirm-dialog";
import { LogConsole } from "@/components/log-console";
import { BackButton } from "@/components/back-button";
import { Notice } from "@/components/notice";
import { ServerHistoryCard } from "@/components/history-panels";
import { ServerSecurity } from "@/components/server-security";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { bytes, uptime } from "@/lib/format";

const METRICS_EVERY = 10_000;
const CONTAINERS_EVERY = 15_000;

type Msg = { tone: "success" | "error"; text: string };

/** Runs fn now and every `ms` while the window is visible. */
function usePolling(fn: () => Promise<unknown>, ms: number) {
  useEffect(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const tick = async () => {
      if (!document.hidden) await fn().catch(() => {});
      if (!stopped) timer = setTimeout(tick, ms);
    };
    timer = setTimeout(tick, 0);
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [fn, ms]);
}

export function ServerDashboard({ server, onBack, onSetup }: { server: Server; onBack: () => void; onSetup: () => void }) {
  const [metrics, setMetrics] = useState<Metrics | null>(null);
  const [error, setError] = useState<string | null>(null);

  const loadMetrics = useCallback(
    () =>
      api.serverMetrics(server.id).then(
        (m) => {
          setMetrics(m);
          setError(null);
        },
        (e: Error) => setError(e.message),
      ),
    [server.id],
  );
  usePolling(loadMetrics, METRICS_EVERY);

  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      <BackButton label="Servers" onClick={onBack} />
      <header className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">{server.name}</h1>
          <p className="selectable mt-1 font-mono text-xs text-muted-foreground">
            {server.user}@{server.host}
            {metrics?.os && ` · ${metrics.os}`}
            {metrics && ` · up ${uptime(metrics.uptimeSec)}`}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" asChild>
            <Link href={`/terminal/?server=${encodeURIComponent(server.id)}`}>
              <SquareTerminal />
              Terminal
            </Link>
          </Button>
          <Button variant="outline" asChild>
            <Link href={`/import/?server=${encodeURIComponent(server.id)}`}>
              <ScanSearch />
              Import
            </Link>
          </Button>
          <Button onClick={onSetup}>
            <Settings2 />
            Set up
          </Button>
        </div>
      </header>

      {error && (
        <Notice tone="error" className="mb-4">
          {error}
        </Notice>
      )}
      {metrics?.warnings?.map((w) => (
        <Notice key={w} tone="warning" className="mb-3">
          {w}
        </Notice>
      ))}

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Gauge
          icon={Cpu}
          label="CPU"
          percent={metrics?.cpuPercent}
          detail={metrics ? `${metrics.cpus} core${metrics.cpus === 1 ? "" : "s"}` : ""}
        />
        <Gauge
          icon={MemoryStick}
          label="Memory"
          percent={metrics && metrics.memTotal ? (100 * metrics.memUsed) / metrics.memTotal : undefined}
          detail={metrics ? `${bytes(metrics.memUsed)} of ${bytes(metrics.memTotal)}` : ""}
        />
        <Gauge
          icon={HardDrive}
          label="Disk"
          percent={metrics && metrics.diskTotal ? (100 * metrics.diskUsed) / metrics.diskTotal : undefined}
          detail={metrics ? `${bytes(metrics.diskUsed)} of ${bytes(metrics.diskTotal)}` : ""}
        />
        <Gauge
          icon={GaugeIcon}
          label="Load"
          percent={metrics && metrics.cpus ? (100 * (metrics.load?.[0] ?? 0)) / metrics.cpus : undefined}
          value={metrics?.load ? metrics.load[0].toFixed(2) : undefined}
          detail={metrics?.load ? `5 min ${metrics.load[1].toFixed(2)} · 15 min ${metrics.load[2].toFixed(2)}` : ""}
        />
      </div>

      <ServerHistoryCard serverId={server.id} />
      <ServerSecurity key={server.id + server.auth} server={server} />

      {metrics && !metrics.dockerReady ? (
        <div className="mt-6 rounded-lg border border-dashed border-border p-6 text-center text-sm text-muted-foreground">
          {server.user} can&apos;t use Docker on this server, so containers aren&apos;t shown.
          <div>
            <Button variant="link" onClick={onSetup}>
              Set up Docker
              <ChevronRight />
            </Button>
          </div>
        </div>
      ) : (
        <>
          <Containers serverId={server.id} />
          <DockerDisk serverId={server.id} onCleaned={() => void loadMetrics()} />
        </>
      )}
    </div>
  );
}

function Gauge({
  icon: Icon,
  label,
  percent,
  value,
  detail,
}: {
  icon: LucideIcon;
  label: string;
  percent?: number;
  value?: string;
  detail: string;
}) {
  const p = percent === undefined ? undefined : Math.max(0, Math.min(100, percent));
  const color = p === undefined ? "bg-muted-foreground/30" : p >= 90 ? "bg-destructive" : p >= 75 ? "bg-warning" : "bg-primary";
  return (
    <Card size="sm" className="gap-0 px-4 py-4">
      <div className="flex items-baseline justify-between">
        <span className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
          <Icon className="size-3.5" />
          {label}
        </span>
        <span className="font-mono text-lg">{value ?? (p === undefined ? "—" : `${Math.round(p)}%`)}</span>
      </div>
      <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-surface-2">
        <div className={`h-full rounded-full transition-all duration-500 ${color}`} style={{ width: `${p ?? 0}%` }} />
      </div>
      <div className="mt-2 truncate text-xs text-muted-foreground">{detail || " "}</div>
    </Card>
  );
}

function Containers({ serverId }: { serverId: string }) {
  const router = useRouter();
  const [list, setList] = useState<Container[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [logsFor, setLogsFor] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[] | null>(null);
  const [notice, setNotice] = useState<Msg | null>(null);

  const load = useCallback(
    () =>
      api.serverContainers(serverId).then(
        (l) => {
          setList(l);
          setError(null);
        },
        (e: Error) => setError(e.message),
      ),
    [serverId],
  );
  usePolling(load, CONTAINERS_EVERY);

  async function act(c: Container, action: "restart" | "stop" | "start") {
    setBusy(c.name + action);
    setNotice(null);
    try {
      setNotice({ tone: "success", text: await api.containerAction(serverId, c.name, action) });
      await load();
    } catch (e) {
      setNotice({ tone: "error", text: (e as Error).message });
    } finally {
      setBusy(null);
    }
  }

  async function showLogs(name: string) {
    if (logsFor === name) {
      setLogsFor(null);
      return;
    }
    setLogsFor(name);
    setLogs(null);
    try {
      setLogs(await api.containerLogs(serverId, name));
    } catch (e) {
      setLogs([(e as Error).message]);
    }
  }

  const running = list?.filter((c) => c.state === "running").length ?? 0;
  return (
    <section className="mt-6">
      <div className="mb-2 flex items-baseline justify-between">
        <h2 className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          <ContainerIcon className="size-3.5" />
          Containers {list && <span className="normal-case tracking-normal">· {running} running of {list.length}</span>}
        </h2>
        <span className="text-xs text-muted-foreground">Updates every 15s</span>
      </div>
      {error && (
        <Notice tone="error" className="mb-2">
          {error}
        </Notice>
      )}
      {notice && (
        <Notice tone={notice.tone} className="mb-2">
          {notice.text}
        </Notice>
      )}
      {list === null ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" />
          Loading…
        </p>
      ) : list.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border p-6 text-center text-sm text-muted-foreground">No containers on this server.</p>
      ) : (
        <Card className="gap-0 py-0">
          <ul className="divide-y divide-border">
            {list.map((c) => (
              <li key={c.name}>
                <div className="flex items-center gap-3 px-4 py-2.5">
                  <span
                    className={`size-2 shrink-0 rounded-full ${
                      c.state === "running" ? "bg-success" : c.state === "restarting" ? "bg-warning animate-pulse" : "bg-muted-foreground/40"
                    }`}
                    title={c.state}
                  />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="selectable truncate font-mono text-[13px]">{c.name}</span>
                      {c.project && (
                        <Badge variant="secondary" className="text-[10px] text-muted-foreground">
                          {c.project}
                        </Badge>
                      )}
                    </div>
                    <div className="truncate text-xs text-muted-foreground">
                      {c.image} · {c.status}
                      {c.ports && <span className="font-mono"> · {c.ports}</span>}
                    </div>
                  </div>
                  <div className="hidden w-36 shrink-0 text-right font-mono text-xs text-muted-foreground md:block">
                    {c.cpu && (
                      <>
                        <div>{c.cpu} CPU</div>
                        <div className="truncate">{c.memory.split(" / ")[0]}</div>
                      </>
                    )}
                  </div>
                  <div className="flex shrink-0 gap-1">
                    <IconAction
                      label={logsFor === c.name ? "Hide logs" : "Logs"}
                      icon={ScrollText}
                      active={logsFor === c.name}
                      onClick={() => void showLogs(c.name)}
                    />
                    {c.state === "running" ? (
                      <>
                        <IconAction
                          label="Open a shell"
                          icon={SquareTerminal}
                          disabled={busy !== null}
                          onClick={() => router.push(`/terminal/?server=${encodeURIComponent(serverId)}&container=${encodeURIComponent(c.name)}`)}
                        />
                        <IconAction
                          label="Restart"
                          icon={RotateCw}
                          busy={busy === c.name + "restart"}
                          disabled={busy !== null}
                          onClick={() => void act(c, "restart")}
                        />
                        <IconAction
                          label="Stop"
                          icon={Square}
                          busy={busy === c.name + "stop"}
                          disabled={busy !== null}
                          onClick={() => void act(c, "stop")}
                        />
                      </>
                    ) : (
                      <IconAction
                        label="Start"
                        icon={Play}
                        busy={busy === c.name + "start"}
                        disabled={busy !== null}
                        onClick={() => void act(c, "start")}
                      />
                    )}
                  </div>
                </div>
                {logsFor === c.name && (
                  <div className="border-t border-border p-3">
                    <LogConsole
                      entries={(logs ?? []).map((line) => ({ line }))}
                      running={logs === null}
                      title={`Last 300 lines · ${c.name}`}
                      height="h-72"
                    />
                  </div>
                )}
              </li>
            ))}
          </ul>
        </Card>
      )}
    </section>
  );
}

function IconAction({
  label,
  icon: Icon,
  onClick,
  disabled,
  active,
  busy,
}: {
  label: string;
  icon: LucideIcon;
  onClick: () => void;
  disabled?: boolean;
  active?: boolean;
  busy?: boolean;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="outline"
          size="icon-sm"
          onClick={onClick}
          disabled={disabled}
          aria-label={label}
          className={active ? "border-primary text-primary" : "text-muted-foreground hover:text-foreground"}
        >
          {busy ? <Loader2 className="animate-spin" /> : <Icon />}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function DockerDisk({ serverId, onCleaned }: { serverId: string; onCleaned: () => void }) {
  const confirm = useConfirm();
  const [usage, setUsage] = useState<DiskUsage[] | null>(null);
  const [cleaning, setCleaning] = useState(false);
  const [msg, setMsg] = useState<Msg | null>(null);

  const load = useCallback(() => api.dockerDiskUsage(serverId).then(setUsage, () => setUsage([])), [serverId]);
  useEffect(() => {
    const t = setTimeout(() => void load(), 0);
    return () => clearTimeout(t);
  }, [load]);

  async function clean() {
    const ok = await confirm({
      title: "Clean up Docker?",
      message: "Removes images no container uses and the build cache. Running apps, stopped containers and volumes (your data) are not touched.",
      confirmLabel: "Clean up",
    });
    if (!ok) return;
    setCleaning(true);
    setMsg(null);
    try {
      setMsg({ tone: "success", text: await api.pruneDocker(serverId) });
      await load();
      onCleaned();
    } catch (e) {
      setMsg({ tone: "error", text: (e as Error).message });
    } finally {
      setCleaning(false);
    }
  }

  if (!usage || usage.length === 0) return null;
  return (
    <section className="mt-6">
      <div className="mb-2 flex items-center justify-between">
        <h2 className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          <HardDrive className="size-3.5" />
          Docker disk usage
        </h2>
        <Button variant="link" size="xs" onClick={clean} disabled={cleaning}>
          {cleaning ? <Loader2 className="animate-spin" /> : <Sparkles />}
          {cleaning ? "Cleaning…" : "Clean up unused"}
        </Button>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {usage.map((u) => (
          <Card key={u.type} size="sm" className="gap-0 px-3 py-3">
            <div className="text-xs text-muted-foreground">
              {u.type} <span className="font-mono">({u.count})</span>
            </div>
            <div className="mt-1 font-mono text-base">{u.size}</div>
            <div className="text-xs text-muted-foreground">reclaimable {u.reclaimable}</div>
          </Card>
        ))}
      </div>
      {msg && (
        <Notice tone={msg.tone} className="mt-2">
          {msg.text}
        </Notice>
      )}
    </section>
  );
}
