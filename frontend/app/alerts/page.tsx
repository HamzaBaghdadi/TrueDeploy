"use client";

import { BellOff, CircleAlert, CircleCheck, FolderGit2, Loader2, RefreshCw, Server, Settings, TriangleAlert, X } from "lucide-react";
import Link from "next/link";
import { api, type Alert } from "@/lib/api";
import { setMonitorStatus, useMonitorStatus } from "@/lib/alerts";
import { timeAgo } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

export default function AlertsPage() {
  const status = useMonitorStatus();
  const alerts = status?.alerts ?? [];

  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      <header className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Alerts</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            TrueDeploy checks your servers and projects in the background, even from the system tray.
            {status?.lastCheck && !status.lastCheck.startsWith("0001") && <> Last check {timeAgo(status.lastCheck)}.</>}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" asChild>
            <Link href="/settings/">
              <Settings /> Settings &amp; channels
            </Link>
          </Button>
          <Button size="sm" onClick={() => void api.checkNow()} disabled={!status?.enabled || status?.checking}>
            {status?.checking ? <Loader2 className="animate-spin" /> : <RefreshCw />}
            {status?.checking ? "Checking…" : "Check now"}
          </Button>
        </div>
      </header>

      {status === null ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" /> Loading…
        </p>
      ) : !status.enabled ? (
        <Empty icon={BellOff} title="Monitoring is off">
          Turn it on in{" "}
          <Link href="/settings/" className="text-primary hover:underline">
            Settings
          </Link>{" "}
          to get alerted when a server or app has a problem.
        </Empty>
      ) : alerts.length === 0 ? (
        <Empty icon={CircleCheck} title="All good" tone="success">
          {status.lastCheck && !status.lastCheck.startsWith("0001")
            ? "No problems found on your servers and projects."
            : "The first check runs shortly after TrueDeploy starts."}
        </Empty>
      ) : (
        <ul className="space-y-3">
          {alerts.map((a) => (
            <AlertRow key={a.key} alert={a} />
          ))}
        </ul>
      )}
    </div>
  );
}

function AlertRow({ alert: a }: { alert: Alert }) {
  const critical = a.severity === "critical";
  const Icon = critical ? CircleAlert : TriangleAlert;
  return (
    <li>
      <Card className={cn("gap-0 py-0", critical ? "ring-destructive/30" : "ring-warning/25")}>
        <CardContent className="flex items-start gap-3 p-4">
          <Icon className={cn("mt-0.5 size-5 shrink-0", critical ? "text-destructive" : "text-warning")} />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{a.title}</span>
              <Badge
                variant="outline"
                className={critical ? "border-destructive/25 bg-destructive/10 text-destructive" : "border-warning/25 bg-warning/5 text-warning"}
              >
                {critical ? "Critical" : "Warning"}
              </Badge>
              <span className="text-xs text-muted-foreground">since {timeAgo(a.since)}</span>
            </div>
            <p className="selectable mt-1 text-[13px] text-muted-foreground">{a.detail}</p>
            <div className="mt-2 flex gap-1">
              {a.projectId ? (
                <Button variant="ghost" size="xs" className="text-primary" asChild>
                  <Link href={`/projects/?id=${encodeURIComponent(a.projectId)}`}>
                    <FolderGit2 /> Open project
                  </Link>
                </Button>
              ) : (
                <Button variant="ghost" size="xs" className="text-primary" asChild>
                  <Link href="/">
                    <Server /> Open servers
                  </Link>
                </Button>
              )}
            </div>
          </div>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground"
                aria-label="Dismiss"
                onClick={() => void api.dismissAlert(a.key).then(setMonitorStatus)}
              >
                <X />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Dismiss until it recovers</TooltipContent>
          </Tooltip>
        </CardContent>
      </Card>
    </li>
  );
}

function Empty({
  icon: Icon,
  title,
  tone,
  children,
}: {
  icon: typeof CircleCheck;
  title: string;
  tone?: "success";
  children: React.ReactNode;
}) {
  return (
    <div className="rounded-lg border border-dashed border-border p-12 text-center">
      <Icon className={cn("mx-auto mb-3 size-10", tone === "success" ? "text-success" : "text-muted-foreground/60")} />
      <p className="font-medium">{title}</p>
      <p className="mt-1 text-sm text-muted-foreground">{children}</p>
    </div>
  );
}
