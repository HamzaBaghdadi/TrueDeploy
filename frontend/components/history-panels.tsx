"use client";

import { Activity, ChartLine } from "lucide-react";
import { useEffect, useState } from "react";
import { api, type SiteHistory } from "@/lib/api";
import type { ServerPoint } from "@/bindings/truedeploy/internal/history";
import { useMonitorStatus } from "@/lib/alerts";
import { TimeSeriesChart, UptimeStrip, type Point } from "@/components/charts";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";

function RangeToggle({ value, onChange }: { value: number; onChange: (h: number) => void }) {
  return (
    <ToggleGroup type="single" variant="outline" size="sm" value={String(value)} onValueChange={(v) => v && onChange(Number(v))}>
      <ToggleGroupItem value="24">24 h</ToggleGroupItem>
      <ToggleGroupItem value="168">7 days</ToggleGroupItem>
    </ToggleGroup>
  );
}

/** Reloads whenever a monitoring round finishes. */
function useRoundKey() {
  const monitor = useMonitorStatus();
  return monitor?.lastCheck ?? "";
}

const pctFmt = (v: number) => `${Math.round(v)}%`;

/** CPU, memory, disk and load over time, one small chart each. */
export function ServerHistoryCard({ serverId }: { serverId: string }) {
  const [hours, setHours] = useState(24);
  const [points, setPoints] = useState<ServerPoint[] | null>(null);
  const round = useRoundKey();
  useEffect(() => {
    let cancelled = false;
    api.serverHistory(serverId, hours).then(
      (p) => !cancelled && setPoints(p),
      () => !cancelled && setPoints([]),
    );
    return () => {
      cancelled = true;
    };
  }, [serverId, hours, round]);

  const series = (pick: (p: ServerPoint) => number): Point[] => (points ?? []).map((p) => ({ t: p.t, v: p.up ? pick(p) : null }));
  const last = points?.length ? points[points.length - 1] : null;
  const down = (points ?? []).filter((p) => !p.up).length;
  const long = hours > 24;
  const tiles: { label: string; pick: (p: ServerPoint) => number; max?: number; fmt: (v: number) => string }[] = [
    { label: "CPU", pick: (p) => p.cpu ?? 0, max: 100, fmt: pctFmt },
    { label: "Memory", pick: (p) => p.mem ?? 0, max: 100, fmt: pctFmt },
    { label: "Disk", pick: (p) => p.disk ?? 0, max: 100, fmt: pctFmt },
    { label: "Load (1 min)", pick: (p) => p.load ?? 0, fmt: (v) => (v < 10 ? v.toFixed(1) : String(Math.round(v))) },
  ];

  return (
    <Card className="mt-6">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <ChartLine className="size-4 text-muted-foreground" />
          History
        </CardTitle>
        <CardDescription>From each monitoring check while TrueDeploy runs (kept for 7 days on this computer).</CardDescription>
        <CardAction>
          <RangeToggle value={hours} onChange={setHours} />
        </CardAction>
      </CardHeader>
      <CardContent>
        {points === null ? (
          <div className="h-40" />
        ) : points.length < 2 ? (
          <p className="text-[13px] text-muted-foreground">
            Not enough checks yet. Charts fill in as monitoring runs (every few minutes; see Settings → Monitoring).
          </p>
        ) : (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              {tiles.map((t) => (
                <div key={t.label}>
                  <div className="mb-1 flex items-baseline justify-between text-xs">
                    <span className="font-medium">{t.label}</span>
                    <span className="text-muted-foreground">
                      now <span className="font-medium text-foreground">{last?.up ? t.fmt(t.pick(last)) : "unreachable"}</span>
                    </span>
                  </div>
                  <TimeSeriesChart points={series(t.pick)} max={t.max} format={t.fmt} long={long} label={t.label} />
                </div>
              ))}
            </div>
            {down > 0 && (
              <p className="mt-3 flex items-center gap-2 text-[12px] text-muted-foreground">
                <span className="inline-block h-1.5 w-3 rounded-[1px] bg-destructive" /> Server unreachable ({down} check{down === 1 ? "" : "s"} in this range)
              </p>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}

function uptimeText(u: number) {
  return u < 0 ? "–" : `${u.toFixed(u === 100 ? 0 : 1)}%`;
}

/** Uptime and response time of a project's domains. */
export function SiteHistoryCard({ projectId }: { projectId: string }) {
  const [hours, setHours] = useState(24);
  const [sites, setSites] = useState<SiteHistory[] | null>(null);
  const round = useRoundKey();
  useEffect(() => {
    let cancelled = false;
    api.siteHistory(projectId, hours).then(
      (s) => !cancelled && setSites(s),
      () => !cancelled && setSites([]),
    );
    return () => {
      cancelled = true;
    };
  }, [projectId, hours, round]);

  if (sites === null || sites.length === 0) return null;
  return (
    <Card size="sm" className="mt-4 gap-0 py-0">
      <CardHeader className="border-b py-2.5">
        <CardTitle className="flex items-center gap-2">
          <Activity className="size-4 text-muted-foreground" />
          Uptime &amp; response time
        </CardTitle>
        <CardAction className="self-center">
          <RangeToggle value={hours} onChange={setHours} />
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-5 py-3">
        {sites.map((s) => (
          <div key={s.domain} className="space-y-2">
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <span className="font-mono text-sm">{s.domain}</span>
              <span className="flex flex-wrap gap-x-3 text-xs text-muted-foreground">
                <span>
                  24 h <b className={cn("font-medium", s.uptime24h >= 0 && s.uptime24h < 99 ? "text-warning" : "text-foreground")}>{uptimeText(s.uptime24h)}</b>
                </span>
                <span>
                  7 days <b className={cn("font-medium", s.uptime7d >= 0 && s.uptime7d < 99 ? "text-warning" : "text-foreground")}>{uptimeText(s.uptime7d)}</b>
                </span>
                {s.avgMs24h > 0 && (
                  <span>
                    avg <b className="font-medium text-foreground">{s.avgMs24h} ms</b>
                  </span>
                )}
              </span>
            </div>
            <UptimeStrip points={s.points ?? []} />
            {(s.points?.length ?? 0) > 1 && (
              <TimeSeriesChart
                points={(s.points ?? []).map((p) => ({ t: p.t, v: p.up && p.ms ? p.ms : null }))}
                format={(v) => `${Math.round(v)} ms`}
                height={80}
                long={hours > 24}
                label="Response time"
              />
            )}
          </div>
        ))}
        <p className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="inline-block size-2 rounded-[2px] bg-success/70" /> Up
          </span>
          <span className="flex items-center gap-1.5">
            <span className="inline-block size-2 rounded-[2px] bg-destructive/80" /> Down (any failed check in that slice)
          </span>
          <span>Checked from this computer, only while TrueDeploy runs.</span>
        </p>
      </CardContent>
    </Card>
  );
}
