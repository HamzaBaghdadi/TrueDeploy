"use client";

import { CalendarClock, CircleAlert, CircleCheck, Loader2, Pencil, Play, Plus, ScrollText, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { api, type Job, type JobView, type ProjectDetail } from "@/lib/api";
import { useConfirm } from "@/components/confirm-dialog";
import { Notice } from "@/components/notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

const pad = (n: number) => String(n).padStart(2, "0");
const DAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

/** Plain-language description of common cron schedules (UTC). */
export function describeSchedule(s: string): string {
  const t = s.trim();
  const named: Record<string, string> = {
    "@hourly": "Every hour",
    "@daily": "Every day at 00:00 UTC",
    "@midnight": "Every day at 00:00 UTC",
    "@weekly": "Every Sunday at 00:00 UTC",
    "@monthly": "On the 1st of every month",
    "@yearly": "Once a year",
    "@annually": "Once a year",
    "* * * * *": "Every minute",
  };
  if (named[t]) return named[t];
  let m: RegExpMatchArray | null;
  if ((m = t.match(/^\*\/(\d+) \* \* \* \*$/))) return `Every ${m[1]} minutes`;
  if ((m = t.match(/^(\d+) \* \* \* \*$/))) return Number(m[1]) === 0 ? "Every hour" : `Every hour at :${pad(Number(m[1]))}`;
  if ((m = t.match(/^(\d+) \*\/(\d+) \* \* \*$/))) return `Every ${m[2]} hours at :${pad(Number(m[1]))}`;
  if ((m = t.match(/^(\d+) (\d+) \* \* \*$/))) return `Every day at ${pad(Number(m[2]))}:${pad(Number(m[1]))} UTC`;
  if ((m = t.match(/^(\d+) (\d+) \* \* ([0-7])$/))) return `Every ${DAYS[Number(m[3]) % 7]} at ${pad(Number(m[2]))}:${pad(Number(m[1]))} UTC`;
  if ((m = t.match(/^(\d+) (\d+) (\d+) \* \*$/))) return `Monthly on day ${m[3]} at ${pad(Number(m[2]))}:${pad(Number(m[1]))} UTC`;
  return `Cron: ${t} (UTC)`;
}

function ago(unix: number) {
  const s = Math.max(0, Math.round(Date.now() / 1000 - unix));
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return `${Math.floor(s / 86400)} d ago`;
}

/** Scheduled jobs of a project: cron commands run inside its container. */
export function ProjectJobs({ project }: { project: ProjectDetail }) {
  const confirm = useConfirm();
  const [list, setList] = useState<JobView[] | null>(null);
  const [services, setServices] = useState<string[]>([]);
  const [statusError, setStatusError] = useState("");
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [editing, setEditing] = useState<Job | "new" | null>(null);
  const [output, setOutput] = useState<{ title: string; lines: string[] | null } | null>(null);
  const [outputOpen, setOutputOpen] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [now, setNow] = useState(0);
  const dockerfile = project.runtime === "dockerfile";

  const load = useCallback(
    () =>
      api.listJobs(project.id).then(
        (r) => {
          setList(r.jobs ?? []);
          setServices(r.services ?? []);
          setStatusError(r.statusError ?? "");
          setNow(Date.now());
        },
        (e: Error) => {
          setList([]);
          setMsg({ ok: false, text: e.message });
        },
      ),
    [project.id],
  );
  useEffect(() => {
    const t = setTimeout(() => void load(), 0);
    return () => clearTimeout(t);
  }, [load]);

  async function act(key: string, fn: () => Promise<string>) {
    setBusy(key);
    setMsg(null);
    try {
      const text = await fn();
      if (text) setMsg({ ok: true, text });
      await load();
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    } finally {
      setBusy(null);
    }
  }

  async function runNow(j: JobView) {
    setOutputOpen(true);
    setOutput({ title: `Running ${j.name}…`, lines: null });
    try {
      const lines = await api.runJob(project.id, j.id);
      setOutput({ title: j.name, lines });
    } catch (e) {
      setOutput({ title: j.name, lines: [(e as Error).message] });
    }
    void load();
  }

  async function showLog(j: JobView) {
    setOutputOpen(true);
    setOutput({ title: `${j.name}: recent runs`, lines: null });
    try {
      setOutput({ title: `${j.name}: recent runs`, lines: await api.jobLog(project.id, j.id) });
    } catch (e) {
      setOutput({ title: j.name, lines: [(e as Error).message] });
    }
  }

  async function remove(j: JobView) {
    const ok = await confirm({
      title: `Delete the job ${j.name}?`,
      message: "It's removed from the server's crontab and won't run again. Its log stays on the server.",
      confirmLabel: "Delete job",
      tone: "danger",
    });
    if (ok) await act("del:" + j.id, () => api.deleteJob(project.id, j.id));
  }

  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <CalendarClock className="size-4 text-muted-foreground" />
          Scheduled jobs
        </CardTitle>
        <CardDescription>
          Commands run on a schedule inside the app&apos;s container, from the server&apos;s crontab (they keep running while TrueDeploy is
          closed). Failed runs raise an alert.
        </CardDescription>
        <CardAction>
          <Button size="sm" variant="outline" onClick={() => setEditing("new")}>
            <Plus /> Add job
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-3">
        {statusError && <Notice tone="warning">Couldn&apos;t read the last runs from the server: {statusError}</Notice>}
        {list === null ? (
          <p className="flex items-center gap-2 text-[13px] text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" /> Loading…
          </p>
        ) : list.length === 0 ? (
          <p className="text-[13px] text-muted-foreground">
            No jobs yet. Examples: <span className="font-mono">php artisan schedule:run</span> every minute,{" "}
            <span className="font-mono">node dist/cli.js send-digest</span> every morning.
          </p>
        ) : (
          <ul className="divide-y divide-border rounded-lg border border-border">
            {list.map((j) => {
              const last = j.last;
              const ran = last && last.start > 0;
              return (
                <li key={j.id} className="flex flex-wrap items-center gap-3 px-3 py-2.5">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-sm font-medium">{j.name}</span>
                      {!dockerfile && j.service && (
                        <Badge variant="secondary" className="font-mono text-[10px]">
                          {j.service}
                        </Badge>
                      )}
                      {!j.enabled && (
                        <Badge variant="outline" className="text-[10px] text-muted-foreground">
                          paused
                        </Badge>
                      )}
                    </div>
                    <div className="truncate font-mono text-xs text-muted-foreground">{j.command}</div>
                    <div className="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
                      <span>{describeSchedule(j.schedule)}</span>
                      <span aria-hidden>·</span>
                      {last?.running ? (
                        <span className="flex items-center gap-1 text-info">
                          <Loader2 className="size-3 animate-spin" /> running
                        </span>
                      ) : ran ? (
                        last.exit === 0 ? (
                          <span className="flex items-center gap-1 text-success">
                            <CircleCheck className="size-3" /> ok {now ? ago(last.start) : ""} · {last.seconds}s
                          </span>
                        ) : (
                          <span className="flex items-center gap-1 text-destructive">
                            <CircleAlert className="size-3" /> {last.exit === 124 ? "timed out" : `failed (exit ${last.exit})`}{" "}
                            {now ? ago(last.start) : ""}
                          </span>
                        )
                      ) : (
                        <span>never ran</span>
                      )}
                    </div>
                  </div>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span>
                        <Switch
                          checked={j.enabled}
                          disabled={busy !== null}
                          onCheckedChange={(v) => void act("tog:" + j.id, () => api.saveJob(project.id, { ...j, enabled: v }))}
                          aria-label={`Run ${j.name} on schedule`}
                        />
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>{j.enabled ? "Runs on schedule" : "Paused"}</TooltipContent>
                  </Tooltip>
                  <Button size="xs" variant="outline" onClick={() => void runNow(j)} disabled={busy !== null || outputOpen}>
                    <Play /> Run now
                  </Button>
                  <Button size="icon-sm" variant="ghost" aria-label={`Log of ${j.name}`} onClick={() => void showLog(j)}>
                    <ScrollText />
                  </Button>
                  <Button size="icon-sm" variant="ghost" aria-label={`Edit ${j.name}`} onClick={() => setEditing(j)}>
                    <Pencil />
                  </Button>
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    aria-label={`Delete ${j.name}`}
                    className="text-muted-foreground hover:text-destructive"
                    onClick={() => void remove(j)}
                    disabled={busy !== null}
                  >
                    {busy === "del:" + j.id ? <Loader2 className="animate-spin" /> : <Trash2 />}
                  </Button>
                </li>
              );
            })}
          </ul>
        )}
        {msg && <Notice tone={msg.ok ? "success" : "error"}>{msg.text}</Notice>}
      </CardContent>

      <JobDialog
        key={editing === null ? "closed" : editing === "new" ? "new" : editing.id}
        job={editing}
        services={services}
        dockerfile={dockerfile}
        onClose={() => setEditing(null)}
        onSave={async (j) => {
          const text = await api.saveJob(project.id, j);
          setEditing(null);
          setMsg({ ok: true, text });
          await load();
        }}
      />

      <Dialog open={outputOpen} onOpenChange={setOutputOpen}>
        <DialogContent className="sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{output?.title}</DialogTitle>
            <DialogDescription>Times are UTC. Each run ends with its exit code.</DialogDescription>
          </DialogHeader>
          {output?.lines === null ? (
            <p className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" /> Waiting for the server…
            </p>
          ) : (
            <pre className="selectable max-h-[60vh] overflow-auto rounded-lg bg-terminal p-3 font-mono text-[11px] leading-relaxed">
              {(output?.lines ?? []).join("\n") || "No output."}
            </pre>
          )}
        </DialogContent>
      </Dialog>
    </Card>
  );
}

const PRESETS: [string, string][] = [
  ["*/5 * * * *", "Every 5 minutes"],
  ["*/15 * * * *", "Every 15 minutes"],
  ["0 * * * *", "Every hour"],
  ["daily", "Every day at…"],
  ["weekly", "Every week on…"],
  ["* * * * *", "Every minute"],
  ["custom", "Custom (cron expression)"],
];

function parseInitial(s: string): { mode: string; hour: number; minute: number; day: number; custom: string } {
  const base = { mode: "custom", hour: 3, minute: 0, day: 1, custom: s };
  if (PRESETS.some(([v]) => v === s)) return { ...base, mode: s };
  let m: RegExpMatchArray | null;
  if ((m = s.match(/^(\d+) (\d+) \* \* \*$/))) return { ...base, mode: "daily", minute: Number(m[1]), hour: Number(m[2]) };
  if ((m = s.match(/^(\d+) (\d+) \* \* ([0-6])$/))) return { ...base, mode: "weekly", minute: Number(m[1]), hour: Number(m[2]), day: Number(m[3]) };
  return base;
}

function JobDialog({
  job,
  services,
  dockerfile,
  onClose,
  onSave,
}: {
  job: Job | "new" | null;
  services: string[];
  dockerfile: boolean;
  onClose: () => void;
  onSave: (j: Job) => Promise<void>;
}) {
  const existing = job && job !== "new" ? job : null;
  const init = parseInitial(existing?.schedule ?? "*/5 * * * *");
  const [name, setName] = useState(existing?.name ?? "");
  const [service, setService] = useState(existing?.service || services[0] || "");
  const [command, setCommand] = useState(existing?.command ?? "");
  const [mode, setMode] = useState(init.mode);
  const [hour, setHour] = useState(init.hour);
  const [minute, setMinute] = useState(init.minute);
  const [day, setDay] = useState(init.day);
  const [custom, setCustom] = useState(init.custom);
  const [timeout, setTimeoutMin] = useState(existing?.timeoutMinutes ? String(existing.timeoutMinutes) : "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const schedule =
    mode === "daily" ? `${minute} ${hour} * * *` : mode === "weekly" ? `${minute} ${hour} * * ${day}` : mode === "custom" ? custom : mode;

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await onSave({
        id: existing?.id ?? "",
        name,
        schedule,
        service: dockerfile ? "" : service,
        command,
        timeoutMinutes: Number(timeout) || 0,
        enabled: existing ? existing.enabled : true,
      });
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  }

  return (
    <Dialog open={job !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{existing ? `Edit ${existing.name}` : "Add scheduled job"}</DialogTitle>
          <DialogDescription>Runs inside the app&apos;s running container, with its environment variables. Times are UTC.</DialogDescription>
        </DialogHeader>
        <form onSubmit={save} className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="job-name">Name</Label>
              <Input id="job-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Send daily digest" required autoFocus />
            </div>
            {!dockerfile && (
              <div className="space-y-1.5">
                <Label>Service</Label>
                {services.length > 0 ? (
                  <Select value={service} onValueChange={setService}>
                    <SelectTrigger className="w-full">
                      <SelectValue placeholder="Choose" />
                    </SelectTrigger>
                    <SelectContent>
                      {services.map((s) => (
                        <SelectItem key={s} value={s}>
                          {s}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                ) : (
                  <Input className="font-mono" value={service} onChange={(e) => setService(e.target.value)} placeholder="e.g. web" spellCheck={false} />
                )}
              </div>
            )}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="job-cmd">Command</Label>
            <Input
              id="job-cmd"
              className="font-mono"
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              placeholder="php artisan schedule:run"
              spellCheck={false}
              required
            />
            <p className="text-[11px] text-muted-foreground">Runs with sh -c, so you can chain with &amp;&amp;.</p>
          </div>
          <div className="space-y-1.5">
            <Label>When</Label>
            <div className="flex flex-wrap items-center gap-2">
              <Select value={mode} onValueChange={setMode}>
                <SelectTrigger className="w-56">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PRESETS.map(([v, label]) => (
                    <SelectItem key={v} value={v}>
                      {label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {mode === "weekly" && (
                <Select value={String(day)} onValueChange={(v) => setDay(Number(v))}>
                  <SelectTrigger className="w-36">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {DAYS.map((d, i) => (
                      <SelectItem key={d} value={String(i)}>
                        {d}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
              {(mode === "daily" || mode === "weekly") && (
                <Input
                  type="time"
                  className="w-32"
                  value={`${pad(hour)}:${pad(minute)}`}
                  onChange={(e) => {
                    const [h, m] = e.target.value.split(":").map(Number);
                    if (!Number.isNaN(h)) setHour(h);
                    if (!Number.isNaN(m)) setMinute(m);
                  }}
                  aria-label="Time (UTC)"
                />
              )}
              {mode === "custom" && (
                <Input className="w-48 font-mono" value={custom} onChange={(e) => setCustom(e.target.value)} placeholder="*/10 * * * *" spellCheck={false} />
              )}
            </div>
            <p className="text-[11px] text-muted-foreground">{describeSchedule(schedule)}</p>
          </div>
          <div className="max-w-40 space-y-1.5">
            <Label htmlFor="job-timeout" className="text-xs text-muted-foreground">
              Stop after (minutes)
            </Label>
            <Input id="job-timeout" value={timeout} onChange={(e) => setTimeoutMin(e.target.value)} inputMode="numeric" placeholder="60" />
          </div>
          {error && <Notice tone="error">{error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || (!dockerfile && !service)}>
              {busy && <Loader2 className="animate-spin" />}
              {existing ? "Save job" : "Add job"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
