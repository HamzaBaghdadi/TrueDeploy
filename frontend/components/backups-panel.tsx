"use client";

import {
  Archive,
  CalendarClock,
  Cloud,
  CloudUpload,
  Download,
  History,
  Loader2,
  MoreHorizontal,
  RefreshCw,
  RotateCcw,
  Server as ServerIcon,
  ShieldCheck,
  Trash2,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { api, type Backup, type BackupScheduleStatus, type OffsiteCopy, type OffsiteKind, type Schedule } from "@/lib/api";
import { useConfirm } from "@/components/confirm-dialog";
import { Notice } from "@/components/notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { bytes, timeAgo } from "@/lib/format";

type Msg = { tone: "success" | "error"; text: string } | null;

/** Where backups come from: a database or a project. */
export type BackupSource = {
  /** Shown in the restore confirmation, e.g. the database name. */
  name: string;
  description: React.ReactNode;
  /** Explains what restoring replaces. */
  replaces: string;
  backupNowNote?: string;
  load: () => Promise<{ backups: Backup[]; schedule: BackupScheduleStatus }>;
  backupNow: () => Promise<string>;
  setSchedule: (s: Schedule) => Promise<string>;
  restore: (name: string) => Promise<string>;
  remove: (name: string) => Promise<string>;
  download: (name: string) => Promise<string>;
  /** Where copies live in the offsite storage. */
  offsite: { kind: OffsiteKind; id: string };
};

const draftOf = (s: BackupScheduleStatus): Schedule => ({ enabled: s.enabled, frequency: s.frequency, hour: s.hour, keep: s.keep, offsite: !!s.offsite });

/** Backups: run now, schedule (cron), restore, download, delete. */
export function BackupsPanel({ source: src, className = "mt-4" }: { source: BackupSource; className?: string }) {
  const confirm = useConfirm();
  const [list, setList] = useState<Backup[] | null>(null);
  const [sched, setSched] = useState<BackupScheduleStatus | null>(null);
  const [draft, setDraft] = useState<Schedule | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [msg, setMsg] = useState<Msg>(null);
  // Offsite storage: null while loading; copies are null until listed.
  const [offsiteOn, setOffsiteOn] = useState<boolean | null>(null);
  const [copies, setCopies] = useState<OffsiteCopy[] | null>(null);
  const [copiesErr, setCopiesErr] = useState<string | null>(null);

  const loadCopies = useCallback(
    () =>
      api.offsite().then(
        async (v) => {
          setOffsiteOn(!!v?.configured);
          if (!v?.configured) return;
          try {
            setCopies(await api.offsiteCopies(src.offsite.kind, src.offsite.id));
            setCopiesErr(null);
          } catch (e) {
            setCopiesErr((e as Error).message);
          }
        },
        () => setOffsiteOn(false),
      ),
    [src.offsite.kind, src.offsite.id],
  );

  const load = useCallback(
    () =>
      src.load().then(
        ({ backups, schedule: s }) => {
          setList(backups);
          setSched(s);
          setDraft(draftOf(s));
        },
        (e: Error) => {
          setList([]);
          setMsg({ tone: "error", text: e.message });
        },
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- the source is recreated each render; its identity is the name
    [src.name],
  );

  useEffect(() => {
    const t = setTimeout(() => {
      void load();
      void loadCopies();
    }, 0);
    return () => clearTimeout(t);
  }, [load, loadCopies]);

  const reload = () => Promise.all([load(), loadCopies()]);

  async function run(key: string, fn: () => Promise<string>, after?: () => Promise<unknown>) {
    setBusy(key);
    setMsg(null);
    try {
      const text = await fn();
      if (text) setMsg({ tone: "success", text });
      await after?.();
    } catch (e) {
      setMsg({ tone: "error", text: (e as Error).message });
    } finally {
      setBusy(null);
    }
  }

  async function restore(b: Backup) {
    const ok = await confirm({
      title: `Restore ${src.name} from this backup?`,
      message: `${src.replaces} replaced with the backup from ${new Date(b.created).toLocaleString()}. A safety backup of the current data is taken first, so you can undo this.`,
      items: [b.name],
      confirmLabel: "Restore",
      tone: "danger",
    });
    if (ok) await run("restore:" + b.name, () => src.restore(b.name), reload);
  }

  async function remove(b: Backup) {
    const ok = await confirm({
      title: "Delete this backup?",
      message: "It is removed from the server for good.",
      items: [b.name],
      confirmLabel: "Delete backup",
      tone: "danger",
    });
    if (ok) await run("delete:" + b.name, () => src.remove(b.name), load);
  }

  const download = (b: Backup) =>
    run("download:" + b.name, async () => {
      const path = await src.download(b.name);
      return path ? `Saved to ${path}` : "";
    });

  const dirty =
    !!sched &&
    !!draft &&
    (draft.enabled !== sched.enabled ||
      (draft.enabled &&
        (draft.frequency !== sched.frequency || draft.hour !== sched.hour || draft.keep !== sched.keep || !!draft.offsite !== !!sched.offsite)));

  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Archive className="size-4 text-muted-foreground" />
          Backups
        </CardTitle>
        <CardDescription>{src.description}</CardDescription>
        <CardAction>
          <Button size="sm" onClick={() => void run("backup", () => src.backupNow(), reload)} disabled={busy !== null}>
            {busy === "backup" ? <Loader2 className="animate-spin" /> : <Archive />}
            {busy === "backup" ? "Backing up…" : "Back up now"}
          </Button>
        </CardAction>
      </CardHeader>

      <CardContent className="space-y-4">
        {/* Schedule */}
        <div className="rounded-lg border border-border bg-background/40 p-3">
          <div className="flex items-center gap-3">
            <CalendarClock className="size-4 text-muted-foreground" />
            <div className="flex-1">
              <div className="text-sm font-medium">Scheduled backups</div>
              <div className="text-xs text-muted-foreground">
                Runs from your own crontab on the server, so it works even when TrueDeploy is closed.
              </div>
            </div>
            {sched?.installed && (
              <Badge variant="outline" className="border-success/25 bg-success/10 text-success">
                <ShieldCheck className="size-3" /> Active
              </Badge>
            )}
            <Switch
              checked={!!draft?.enabled}
              disabled={!draft || busy !== null}
              onCheckedChange={(v) => draft && setDraft({ ...draft, enabled: v })}
              aria-label="Scheduled backups"
            />
          </div>

          {draft?.enabled && (
            <div className="mt-3 grid gap-3 sm:grid-cols-3">
              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground">How often</Label>
                <Select value={draft.frequency} onValueChange={(v) => setDraft({ ...draft, frequency: v })}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="daily">Daily</SelectItem>
                    <SelectItem value="12h">Every 12 hours</SelectItem>
                    <SelectItem value="6h">Every 6 hours</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              {draft.frequency === "daily" && (
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">At (UTC)</Label>
                  <Select value={String(draft.hour)} onValueChange={(v) => setDraft({ ...draft, hour: Number(v) })}>
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="max-h-64">
                      {Array.from({ length: 24 }, (_, h) => (
                        <SelectItem key={h} value={String(h)}>
                          {String(h).padStart(2, "0")}:15
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )}
              <div className="space-y-1.5">
                <Label className="text-xs text-muted-foreground">Keep the last</Label>
                <Input
                  type="number"
                  min={1}
                  max={365}
                  value={draft.keep}
                  onChange={(e) => setDraft({ ...draft, keep: Math.max(1, Math.min(365, Number(e.target.value) || 1)) })}
                />
              </div>
            </div>
          )}

          {draft?.enabled && (
            <div className="mt-3 flex items-start justify-between gap-4 border-t border-border pt-3">
              <Label htmlFor={`offsite-${src.offsite.id}`} className="flex-col items-start gap-0.5 font-normal">
                <span className="flex items-center gap-1.5 text-sm font-medium">
                  <CloudUpload className="size-3.5 text-muted-foreground" /> Also copy each backup offsite
                </span>
                <span className="text-[12px] text-muted-foreground">
                  {offsiteOn === false ? (
                    <>
                      So your backups survive if this server is lost.{" "}
                      <Link href="/settings/#offsite" className="text-primary hover:underline">
                        Set up offsite storage
                      </Link>{" "}
                      first.
                    </>
                  ) : (
                    <>Uploads to your offsite storage right after each backup, keeping the same number of copies there.</>
                  )}
                </span>
              </Label>
              <Switch
                id={`offsite-${src.offsite.id}`}
                checked={!!draft.offsite}
                disabled={busy !== null || (offsiteOn !== true && !draft.offsite)}
                onCheckedChange={(v) => setDraft({ ...draft, offsite: v })}
              />
            </div>
          )}

          {dirty && draft && (
            <div className="mt-3 flex justify-end gap-2">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => sched && setDraft(draftOf(sched))}
              >
                Discard
              </Button>
              <Button size="sm" disabled={busy !== null} onClick={() => void run("schedule", () => src.setSchedule(draft), reload)}>
                {busy === "schedule" && <Loader2 className="animate-spin" />}
                {draft.enabled ? "Save schedule" : "Turn off"}
              </Button>
            </div>
          )}

          {sched && sched.lastLog && sched.lastLog.length > 0 && (
            <div className="mt-3 rounded-md bg-terminal px-2.5 py-2 font-mono text-[11px] leading-relaxed text-muted-foreground">
              {sched.lastLog.map((l, i) => (
                <div key={i} className="selectable truncate">
                  {l}
                </div>
              ))}
            </div>
          )}
        </div>

        {src.backupNowNote && <p className="text-[12px] text-muted-foreground">{src.backupNowNote}</p>}

        {msg && <Notice tone={msg.tone}>{msg.text}</Notice>}

        <Separator />

        {/* List */}
        {list === null ? (
          <p className="flex items-center gap-2 text-[13px] text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" /> Loading…
          </p>
        ) : list.length === 0 ? (
          <p className="text-[13px] text-muted-foreground">No backups yet.</p>
        ) : (
          <ul className="divide-y divide-border text-[13px]">
            {list.map((b) => {
              const safety = b.name.includes("prerestore-");
              const rowBusy = busy?.endsWith(":" + b.name);
              return (
                <li key={b.name} className="flex items-center gap-3 py-2">
                  {safety ? <History className="size-3.5 shrink-0 text-warning" /> : <Archive className="size-3.5 shrink-0 text-muted-foreground" />}
                  <span className="selectable min-w-0 flex-1 truncate font-mono text-xs">{b.name}</span>
                  {safety && (
                    <Badge variant="outline" className="border-warning/25 bg-warning/5 text-warning">
                      before restore
                    </Badge>
                  )}
                  <span className="text-xs text-muted-foreground">{bytes(b.bytes)}</span>
                  <span className="w-20 text-right text-xs text-muted-foreground">{timeAgo(b.created)}</span>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button variant="ghost" size="icon-xs" disabled={busy !== null} aria-label="Backup actions">
                        {rowBusy ? <Loader2 className="animate-spin" /> : <MoreHorizontal />}
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="min-w-52">
                      <DropdownMenuItem onSelect={() => void download(b)}>
                        <Download /> Download to this PC
                      </DropdownMenuItem>
                      <DropdownMenuItem onSelect={() => void restore(b)}>
                        <RotateCcw /> Restore this backup
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem variant="destructive" onSelect={() => void remove(b)}>
                        <Trash2 /> Delete
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </li>
              );
            })}
          </ul>
        )}

        {offsiteOn && (
          <OffsiteCopies
            copies={copies}
            error={copiesErr}
            onServer={new Set((list ?? []).map((b) => b.name))}
            busy={busy}
            onRefresh={() => void run("offsite-refresh", async () => (await loadCopies(), ""))}
            onDownload={(c) =>
              void run("odownload:" + c.name, async () => {
                const path = await api.downloadOffsite(src.offsite.kind, src.offsite.id, c.name);
                return path ? `Saved to ${path}` : "";
              })
            }
            onFetch={(c) => void run("fetch:" + c.name, () => api.fetchOffsite(src.offsite.kind, src.offsite.id, c.name), load)}
          />
        )}
      </CardContent>
    </Card>
  );
}

/** Backups in the offsite storage: download straight to this PC (works even
 * if the server is gone), or copy back to the server to restore. */
function OffsiteCopies({
  copies,
  error,
  onServer,
  busy,
  onRefresh,
  onDownload,
  onFetch,
}: {
  copies: OffsiteCopy[] | null;
  error: string | null;
  onServer: Set<string>;
  busy: string | null;
  onRefresh: () => void;
  onDownload: (c: OffsiteCopy) => void;
  onFetch: (c: OffsiteCopy) => void;
}) {
  return (
    <div className="space-y-2 border-t border-border pt-4">
      <div className="flex items-center gap-2">
        <Cloud className="size-4 text-muted-foreground" />
        <span className="flex-1 text-sm font-medium">Offsite copies</span>
        <Button variant="ghost" size="xs" className="text-primary" onClick={onRefresh} disabled={busy !== null}>
          <RefreshCw className={busy === "offsite-refresh" ? "animate-spin" : ""} /> Refresh
        </Button>
      </div>
      {error ? (
        <Notice tone="error">Couldn&apos;t list the offsite storage: {error}</Notice>
      ) : copies === null ? (
        <p className="flex items-center gap-2 text-[13px] text-muted-foreground">
          <Loader2 className="size-3.5 animate-spin" /> Loading…
        </p>
      ) : copies.length === 0 ? (
        <p className="text-[13px] text-muted-foreground">None yet. Turn on “Also copy each backup offsite” in the schedule above.</p>
      ) : (
        <ul className="divide-y divide-border text-[13px]">
          {copies.map((c) => {
            const here = onServer.has(c.name);
            const rowBusy = busy === "odownload:" + c.name || busy === "fetch:" + c.name;
            return (
              <li key={c.name} className="flex items-center gap-3 py-2">
                <Cloud className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="selectable min-w-0 flex-1 truncate font-mono text-xs">{c.name}</span>
                {here && (
                  <Badge variant="outline" className="text-muted-foreground">
                    also on server
                  </Badge>
                )}
                <span className="text-xs text-muted-foreground">{bytes(c.bytes)}</span>
                <span className="w-20 text-right text-xs text-muted-foreground">{timeAgo(c.modified)}</span>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="ghost" size="icon-xs" disabled={busy !== null} aria-label="Offsite copy actions">
                      {rowBusy ? <Loader2 className="animate-spin" /> : <MoreHorizontal />}
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="min-w-56">
                    <DropdownMenuItem onSelect={() => onDownload(c)}>
                      <Download /> Download to this PC
                    </DropdownMenuItem>
                    <DropdownMenuItem disabled={here} onSelect={() => onFetch(c)}>
                      <ServerIcon /> {here ? "Already on the server" : "Copy to the server to restore"}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
