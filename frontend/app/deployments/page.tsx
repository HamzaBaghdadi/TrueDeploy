"use client";

import { ArrowRight, ChevronRight, Loader2, Rocket } from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import { api, type DeploymentEntry } from "@/lib/api";
import { DeployStatus } from "@/components/deploy-status";
import { Notice } from "@/components/notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { duration, isSet, timeAgo } from "@/lib/format";

type Filter = "all" | "failed" | "running";

function dayLabel(t: string) {
  const d = new Date(t);
  const today = new Date();
  const yesterday = new Date(Date.now() - 86400000);
  if (d.toDateString() === today.toDateString()) return "Today";
  if (d.toDateString() === yesterday.toDateString()) return "Yesterday";
  return d.toLocaleDateString(undefined, { weekday: "long", month: "short", day: "numeric" });
}

export default function DeploymentsPage() {
  const [list, setList] = useState<DeploymentEntry[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<Filter>("all");

  const load = useCallback(
    () =>
      api.recentDeployments(200).then(
        (l) => {
          setList(l);
          setError(null);
        },
        (e: Error) => setError(e.message),
      ),
    [],
  );

  useEffect(() => {
    void load();
  }, [load]);

  // Keep the timeline fresh while something is deploying.
  const anyRunning = list?.some((d) => d.status === "running");
  useEffect(() => {
    if (!anyRunning) return;
    const t = setInterval(() => void load(), 5000);
    return () => clearInterval(t);
  }, [anyRunning, load]);

  // The newest successful deploy per project is the one that's live.
  const liveIds = useMemo(() => {
    const ids = new Set<string>();
    const seen = new Set<string>();
    for (const d of list ?? []) {
      if (d.status === "success" && !seen.has(d.projectId)) {
        seen.add(d.projectId);
        ids.add(d.id);
      }
    }
    return ids;
  }, [list]);

  const shown = useMemo(() => (list ?? []).filter((d) => filter === "all" || d.status === filter), [list, filter]);
  const groups = useMemo(() => {
    const out: { day: string; items: DeploymentEntry[] }[] = [];
    for (const d of shown) {
      const day = dayLabel(d.startedAt);
      if (out.at(-1)?.day !== day) out.push({ day, items: [] });
      out.at(-1)!.items.push(d);
    }
    return out;
  }, [shown]);

  const counts = {
    all: list?.length ?? 0,
    failed: list?.filter((d) => d.status === "failed").length ?? 0,
    running: list?.filter((d) => d.status === "running").length ?? 0,
  };

  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      <header className="mb-6 flex items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Deployments</h1>
          <p className="mt-1 text-sm text-muted-foreground">Every deploy started from TrueDeploy, across all projects.</p>
        </div>
        <Tabs value={filter} onValueChange={(v) => setFilter(v as Filter)}>
          <TabsList>
            {(["all", "failed", "running"] as const).map((f) => (
              <TabsTrigger key={f} value={f} className="capitalize">
                {f}
                <Badge variant="secondary" className="ml-1 h-4 min-w-4 px-1 text-[10px]">
                  {counts[f]}
                </Badge>
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </header>

      {error && (
        <Notice tone="error" className="mb-4">
          {error}
        </Notice>
      )}

      {list === null ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" /> Loading…
        </p>
      ) : shown.length === 0 ? (
        <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border p-12 text-center text-sm text-muted-foreground">
          <Rocket className="size-6 opacity-60" />
          {list.length === 0 ? (
            <>
              No deploys yet.
              <Button variant="link" size="sm" asChild>
                <Link href="/projects/">
                  Go to Projects <ArrowRight />
                </Link>
              </Button>
            </>
          ) : (
            `No ${filter} deploys.`
          )}
        </div>
      ) : (
        <div className="space-y-6">
          {groups.map((g) => (
            <section key={g.day}>
              <h2 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">{g.day}</h2>
              <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border bg-surface">
                {g.items.map((d) => (
                  <li key={d.id}>
                    <Link
                      href={`/projects/?id=${encodeURIComponent(d.projectId)}`}
                      className="group flex items-center gap-3 px-4 py-3 transition-colors hover:bg-accent"
                    >
                      <span className="w-20 shrink-0">
                        <DeployStatus status={d.status} live={liveIds.has(d.id)} />
                      </span>
                      <span className="w-40 shrink-0 truncate text-sm font-medium">{d.projectName}</span>
                      <span className="w-16 shrink-0 font-mono text-xs text-muted-foreground">{d.commit || "———"}</span>
                      <span className={`min-w-0 flex-1 truncate text-sm ${d.status === "failed" ? "text-destructive/90" : ""}`}>
                        {d.rollbackOf && (
                          <Badge variant="secondary" className="mr-2 h-4 px-1.5 text-[10px]">
                            rollback
                          </Badge>
                        )}
                        {d.status === "failed" ? d.error || d.message : d.message || d.error}
                      </span>
                      <span className="hidden shrink-0 text-xs text-muted-foreground md:inline">{d.serverName}</span>
                      <span className="w-28 shrink-0 text-right text-xs text-muted-foreground">
                        {timeAgo(d.startedAt)}
                        {isSet(d.finishedAt) && ` · ${duration(d.startedAt, d.finishedAt)}`}
                      </span>
                      <ChevronRight className="size-4 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      )}
    </div>
  );
}
