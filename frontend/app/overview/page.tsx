"use client";

import {
  ArrowRight,
  Bell,
  Box,
  CircleAlert,
  CircleCheck,
  FolderGit2,
  GitBranchPlus,
  Loader2,
  RefreshCw,
  Rocket,
  Server as ServerIcon,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";
import { api, type Overview, type Server } from "@/lib/api";
import { useMonitorStatus } from "@/lib/alerts";
import { Sparkline } from "@/components/charts";
import { DeployStatus } from "@/components/deploy-status";
import { GettingStarted } from "@/components/getting-started";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { isSet, timeAgo } from "@/lib/format";
import { cn } from "@/lib/utils";

// Go sends empty lists as null; the page works with arrays.
type Data = Overview & {
  servers: NonNullable<Overview["servers"]>;
  projects: NonNullable<Overview["projects"]>;
  alerts: NonNullable<Overview["alerts"]>;
  deploys: NonNullable<Overview["deploys"]>;
};

export default function OverviewPage() {
  const monitor = useMonitorStatus();
  const [data, setData] = useState<Data | null>(null);
  const [servers, setServers] = useState<Server[]>([]);
  const round = monitor?.lastCheck ?? "";

  // Fresh on open and after every monitoring round.
  useEffect(() => {
    let cancelled = false;
    Promise.all([api.overview(), api.listServers()]).then(
      ([o, s]) => {
        if (cancelled) return;
        setData({ ...o, servers: o.servers ?? [], projects: o.projects ?? [], alerts: o.alerts ?? [], deploys: o.deploys ?? [] });
        setServers(s);
      },
      () => {},
    );
    return () => {
      cancelled = true;
    };
  }, [round]);

  if (!data) {
    return (
      <div className="mx-auto max-w-5xl px-8 py-8">
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" /> Loading…
        </p>
      </div>
    );
  }

  const reachable = data.servers.filter((s) => s.last?.up).length;
  const checked = data.servers.filter((s) => s.last).length;
  const main = data.projects.filter((p) => !p.preview);
  const failing = main.filter((p) => p.lastStatus === "failed" || p.critical).length;
  const deployed = main.filter((p) => p.lastStatus === "success").length;
  const uptimes = data.projects.map((p) => p.uptime24h).filter((u) => u >= 0);
  const avgUptime = uptimes.length ? uptimes.reduce((a, b) => a + b, 0) / uptimes.length : -1;
  const critical = data.alerts.filter((a) => a.severity === "critical").length;

  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      <header className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Overview</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {!data.enabled
              ? "Monitoring is off (Settings), so this shows deploys only."
              : isSet(data.lastCheck)
                ? `Last check ${timeAgo(data.lastCheck)}.`
                : "The first monitoring check runs shortly after TrueDeploy starts."}
          </p>
        </div>
        {data.enabled && (
          <Button variant="outline" size="sm" onClick={() => void api.checkNow()} disabled={monitor?.checking}>
            <RefreshCw className={cn(monitor?.checking && "animate-spin")} />
            {monitor?.checking ? "Checking…" : "Check now"}
          </Button>
        )}
      </header>

      {data.servers.length === 0 || main.length === 0 ? <GettingStarted servers={servers} /> : null}

      {data.servers.length > 0 && (
        <div className="mb-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Tile
            icon={ServerIcon}
            label="Servers reachable"
            value={checked ? `${reachable} of ${data.servers.length}` : `${data.servers.length}`}
            tone={checked && reachable < checked ? "bad" : "ok"}
            hint={checked ? undefined : "not checked yet"}
          />
          <Tile
            icon={FolderGit2}
            label="Projects deployed"
            value={`${deployed} of ${main.length}`}
            tone={failing ? "bad" : "ok"}
            hint={failing ? `${failing} ${failing === 1 ? "needs" : "need"} attention` : undefined}
          />
          <Tile icon={Bell} label="Active alerts" value={String(data.alerts.length)} tone={critical ? "bad" : data.alerts.length ? "warn" : "ok"} hint={critical ? `${critical} critical` : undefined} />
          <Tile
            icon={CircleCheck}
            label="Uptime, last 24 h"
            value={avgUptime < 0 ? "–" : `${avgUptime.toFixed(avgUptime === 100 ? 0 : 1)}%`}
            tone={avgUptime < 0 || avgUptime >= 99 ? "ok" : "warn"}
            hint={avgUptime < 0 ? "no domains checked yet" : "average of all domains"}
          />
        </div>
      )}

      <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <div className="space-y-4">
          {data.servers.length > 0 && (
            <Card size="sm" className="gap-0 py-0">
              <CardHeader className="border-b py-2.5">
                <CardTitle className="flex items-center gap-2">
                  <ServerIcon className="size-4 text-muted-foreground" /> Servers
                </CardTitle>
              </CardHeader>
              <CardContent className="px-0">
                <ul className="divide-y divide-border">
                  {data.servers.map((s) => (
                    <li key={s.id}>
                      <Link href={`/?id=${encodeURIComponent(s.id)}&view=dashboard`} className="flex items-center gap-3 px-4 py-2.5 hover:bg-accent/50">
                        <Dot state={!s.last ? "unknown" : !s.last.up || s.critical ? "bad" : s.alerts ? "warn" : "ok"} />
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-sm font-medium">{s.name}</div>
                          <div className="truncate font-mono text-[11px] text-muted-foreground">
                            {!s.last
                              ? s.host
                              : !s.last.up
                                ? "unreachable"
                                : `CPU ${Math.round(s.last.cpu ?? 0)}% · memory ${Math.round(s.last.mem ?? 0)}% · disk ${Math.round(s.last.disk ?? 0)}%`}
                          </div>
                        </div>
                        <Sparkline values={s.cpu ?? []} label={`${s.name} CPU, last 24 hours`} />
                      </Link>
                    </li>
                  ))}
                </ul>
              </CardContent>
            </Card>
          )}

          {data.projects.length > 0 && (
            <Card size="sm" className="gap-0 py-0">
              <CardHeader className="border-b py-2.5">
                <CardTitle className="flex items-center gap-2">
                  <FolderGit2 className="size-4 text-muted-foreground" /> Projects
                </CardTitle>
              </CardHeader>
              <CardContent className="px-0">
                <ul className="divide-y divide-border">
                  {data.projects.map((p) => (
                    <li key={p.id}>
                      <Link href={`/projects/?id=${encodeURIComponent(p.id)}`} className="flex items-center gap-3 px-4 py-2.5 hover:bg-accent/50">
                        <Dot state={p.critical || p.lastStatus === "failed" ? "bad" : p.alerts ? "warn" : p.lastStatus === "success" ? "ok" : "unknown"} />
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-1.5 truncate text-sm font-medium">
                            {p.app && <Box className="size-3.5 shrink-0 text-muted-foreground" />}
                            {p.preview && <GitBranchPlus className="size-3.5 shrink-0 text-muted-foreground" />}
                            <span className="truncate">{p.name}</span>
                          </div>
                          <div className="truncate text-[11px] text-muted-foreground">
                            {p.serverName}
                            {p.domain && <span className="font-mono"> · {p.domain}</span>}
                            {p.lastStatus && isSet(p.lastAt) && <> · deployed {timeAgo(p.lastAt!)}</>}
                          </div>
                        </div>
                        {p.uptime24h >= 0 && (
                          <span className={cn("text-xs tabular-nums", p.uptime24h < 99 ? "text-warning" : "text-muted-foreground")}>
                            {p.uptime24h.toFixed(p.uptime24h === 100 ? 0 : 1)}%
                          </span>
                        )}
                        {p.lastStatus ? <DeployStatus status={p.lastStatus} /> : <span className="text-[11px] text-muted-foreground">not deployed</span>}
                      </Link>
                    </li>
                  ))}
                </ul>
              </CardContent>
            </Card>
          )}
        </div>

        <div className="space-y-4">
          <Card size="sm" className="gap-0 py-0">
            <CardHeader className="border-b py-2.5">
              <CardTitle className="flex items-center gap-2">
                <Bell className="size-4 text-muted-foreground" /> Alerts
              </CardTitle>
              <CardAction className="self-center">
                <Button asChild variant="ghost" size="xs" className="text-primary">
                  <Link href="/alerts/">
                    All <ArrowRight />
                  </Link>
                </Button>
              </CardAction>
            </CardHeader>
            <CardContent className="py-3">
              {data.alerts.length === 0 ? (
                <p className="flex items-center gap-2 text-[13px] text-success">
                  <CircleCheck className="size-4" /> {data.enabled ? "No problems found." : "Monitoring is off."}
                </p>
              ) : (
                <ul className="space-y-2.5">
                  {data.alerts.slice(0, 4).map((a) => (
                    <li key={a.key} className="flex gap-2 text-[13px]">
                      {a.severity === "critical" ? (
                        <CircleAlert className="mt-0.5 size-4 shrink-0 text-destructive" />
                      ) : (
                        <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warning" />
                      )}
                      <div className="min-w-0">
                        <div className="font-medium">{a.title}</div>
                        <div className="line-clamp-2 text-[12px] text-muted-foreground">{a.detail}</div>
                      </div>
                    </li>
                  ))}
                  {data.alerts.length > 4 && <li className="text-[12px] text-muted-foreground">and {data.alerts.length - 4} more</li>}
                </ul>
              )}
            </CardContent>
          </Card>

          <Card size="sm" className="gap-0 py-0">
            <CardHeader className="border-b py-2.5">
              <CardTitle className="flex items-center gap-2">
                <Rocket className="size-4 text-muted-foreground" /> Recent deploys
              </CardTitle>
              <CardAction className="self-center">
                <Button asChild variant="ghost" size="xs" className="text-primary">
                  <Link href="/deployments/">
                    All <ArrowRight />
                  </Link>
                </Button>
              </CardAction>
            </CardHeader>
            <CardContent className="px-0">
              {data.deploys.length === 0 ? (
                <p className="px-4 py-3 text-[13px] text-muted-foreground">No deploys yet.</p>
              ) : (
                <ul className="divide-y divide-border">
                  {data.deploys.map((d) => (
                    <li key={d.id}>
                      <Link href={`/projects/?id=${encodeURIComponent(d.projectId)}`} className="flex items-center gap-2 px-4 py-2 hover:bg-accent/50">
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-[13px] font-medium">{d.projectName}</div>
                          <div className="truncate text-[11px] text-muted-foreground">
                            {d.commit && <span className="font-mono">{d.commit} · </span>}
                            {timeAgo(d.startedAt)}
                          </div>
                        </div>
                        <DeployStatus status={d.status} />
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

function Dot({ state }: { state: "ok" | "warn" | "bad" | "unknown" }) {
  const label = { ok: "OK", warn: "Warning", bad: "Problem", unknown: "Not checked yet" }[state];
  return (
    <span
      role="img"
      aria-label={label}
      title={label}
      className={cn(
        "size-2 shrink-0 rounded-full",
        state === "ok" && "bg-success",
        state === "warn" && "bg-warning",
        state === "bad" && "bg-destructive",
        state === "unknown" && "bg-muted-foreground/40",
      )}
    />
  );
}

function Tile({ icon: Icon, label, value, tone, hint }: { icon: LucideIcon; label: string; value: string; tone: "ok" | "warn" | "bad"; hint?: string }) {
  return (
    <Card size="sm" className="gap-1 px-4 py-3">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <Icon className="size-3.5" /> {label}
      </div>
      <div className="text-2xl font-semibold tracking-tight tabular-nums">{value}</div>
      {hint && (
        <div className={cn("flex items-center gap-1 text-[11px]", tone === "bad" ? "text-destructive" : tone === "warn" ? "text-warning" : "text-muted-foreground")}>
          {tone === "bad" && <CircleAlert className="size-3" />}
          {tone === "warn" && <TriangleAlert className="size-3" />}
          {hint}
        </div>
      )}
    </Card>
  );
}
