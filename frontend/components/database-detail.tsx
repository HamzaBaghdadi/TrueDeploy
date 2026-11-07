"use client";

import { ArrowRight, Eye, EyeOff, FolderGit2, Loader2, Play, RotateCw, ScrollText, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { api, type Connection, type DatabaseStatus, type DatabaseView } from "@/lib/api";
import { BackButton } from "@/components/back-button";
import { DatabaseBackups } from "@/components/database-backups";
import { useConfirm } from "@/components/confirm-dialog";
import { CopyButton } from "@/components/copy-button";
import { LogConsole } from "@/components/log-console";
import { Notice } from "@/components/notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";

export function DatabaseDetail({ db, onBack, onDeleted }: { db: DatabaseView; onBack: () => void; onDeleted: () => void }) {
  const confirm = useConfirm();
  const [status, setStatus] = useState<DatabaseStatus | null>(null);
  const [conn, setConn] = useState<Connection | null>(null);
  const [reveal, setReveal] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [deleteData, setDeleteData] = useState(false);

  const loadStatus = useCallback(
    () => api.dbStatus(db.id).then(setStatus, (e: Error) => setStatus({ state: "error", health: "", status: e.message, size: "" })),
    [db.id],
  );
  useEffect(() => {
    const t = setTimeout(() => {
      void loadStatus();
      api.dbConnection(db.id).then(setConn, () => {});
    }, 0);
    return () => clearTimeout(t);
  }, [db.id, loadStatus]);

  async function run(key: string, fn: () => Promise<string>) {
    setBusy(key);
    setMsg(null);
    try {
      setMsg({ ok: true, text: await fn() });
      await loadStatus();
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    } finally {
      setBusy(null);
    }
  }

  async function remove() {
    const ok = await confirm({
      title: `Delete ${db.name}?`,
      message: deleteData
        ? "The container AND its data volume are deleted from the server. This can't be undone. Backups in ~/truedeploy/backups are kept."
        : "The container is removed, but its data volume stays on the server (you can recreate the database later with the same name).",
      confirmLabel: deleteData ? "Delete everything" : "Delete database",
      tone: "danger",
    });
    if (!ok) return;
    try {
      await api.deleteDatabase(db.id, deleteData);
      onDeleted();
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    }
  }

  const running = status?.state === "running";
  const mask = (s: string) => (reveal ? s : s.replace(conn?.password ?? "\u0000", "••••••••"));

  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      <BackButton label="Databases" onClick={onBack} />
      <header className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-semibold tracking-tight">{db.name}</h1>
            {status && (
              <Badge className={running ? "bg-success/10 text-success" : "bg-destructive/10 text-destructive"}>
                {status.state}
                {status.health && ` · ${status.health}`}
              </Badge>
            )}
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            {db.engineName} {db.version} on {db.serverName}
            {status?.status && ` · ${status.status}`}
            {status?.size && ` · data ${status.size}`}
          </p>
        </div>
        <Button variant="outline" onClick={() => void run("restart", () => api.dbRestart(db.id))} disabled={busy !== null}>
          {busy === "restart" ? <Loader2 className="animate-spin" /> : running ? <RotateCw /> : <Play />}
          {busy === "restart" ? "Restarting…" : running ? "Restart" : "Start"}
        </Button>
      </header>

      {msg && (
        <Notice tone={msg.ok ? "success" : "error"} className="mb-4">
          {msg.text}
        </Notice>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Connection</CardTitle>
            <CardAction>
              <Button variant="ghost" size="xs" onClick={() => setReveal(!reveal)} className="text-primary">
                {reveal ? <EyeOff /> : <Eye />}
                {reveal ? "Hide password" : "Reveal password"}
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            {conn ? (
              <div className="space-y-1.5 text-[13px]">
                <Row label="URL" value={mask(conn.url)} copy={conn.url} />
                <Row label="Host" value={`${conn.host}:${conn.port}`} copy={conn.host} />
                {conn.user && <Row label="User" value={conn.user} />}
                {conn.database && <Row label="Database" value={conn.database} />}
                <Row label="Password" value={reveal ? conn.password : "••••••••••••"} copy={conn.password} />
                <p className="pt-2 text-xs text-muted-foreground">
                  Reachable only by apps on {db.serverName}&apos;s private TrueDeploy network. Link it in a project&apos;s settings to get{" "}
                  <span className="font-mono">{db.envVar}</span> injected on deploy.
                </p>
              </div>
            ) : (
              <p className="flex items-center gap-2 text-[13px] text-muted-foreground">
                <Loader2 className="size-3.5 animate-spin" /> Loading…
              </p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Used by</CardTitle>
          </CardHeader>
          <CardContent>
            {db.linkedTo?.length ? (
              <ul className="space-y-1.5 text-sm">
                {db.linkedTo.map((p) => (
                  <li key={p} className="flex items-center gap-2">
                    <FolderGit2 className="size-4 text-muted-foreground" />
                    {p}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="flex flex-wrap items-center gap-1 text-[13px] text-muted-foreground">
                No project is linked yet. Open a project <ArrowRight className="size-3.5" /> Edit <ArrowRight className="size-3.5" /> Databases.
              </p>
            )}
          </CardContent>
        </Card>
      </div>

      {db.canBackup && <DatabaseBackups db={db} />}
      <Logs id={db.id} />

      <Card className="mt-6 ring-destructive/30">
        <CardHeader>
          <CardTitle className="text-destructive">Delete database</CardTitle>
          <CardDescription>Removes the database container from {db.serverName}.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex items-center gap-2">
            <Checkbox id="delete-data" checked={deleteData} onCheckedChange={(v) => setDeleteData(v === true)} />
            <Label htmlFor="delete-data" className="font-normal text-muted-foreground">
              Also delete all its data (the Docker volume). This can&apos;t be undone.
            </Label>
          </div>
          <Button variant="destructive" onClick={remove}>
            <Trash2 /> Delete {db.name}
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}

function Row({ label, value, copy }: { label: string; value: string; copy?: string }) {
  return (
    <div className="flex min-h-7 items-center gap-3">
      <span className="w-20 shrink-0 text-muted-foreground">{label}</span>
      <code className="selectable min-w-0 flex-1 truncate font-mono text-xs">{value}</code>
      {copy && <CopyButton text={copy} />}
    </div>
  );
}

function Logs({ id }: { id: string }) {
  const [lines, setLines] = useState<string[] | null>(null);
  const [open, setOpen] = useState(false);
  async function toggle() {
    if (open) return setOpen(false);
    setOpen(true);
    setLines(null);
    setLines(await api.dbLogs(id).catch((e: Error) => [e.message]));
  }
  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle>Logs</CardTitle>
        <CardAction>
          <Button variant="ghost" size="xs" onClick={() => void toggle()} className="text-primary">
            <ScrollText />
            {open ? "Hide" : "Show recent logs"}
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {open ? (
          <LogConsole entries={(lines ?? []).map((line) => ({ line }))} running={lines === null} title="Last 200 lines" height="h-64" />
        ) : (
          <p className="text-[13px] text-muted-foreground">The database container&apos;s recent output, useful when it won&apos;t start.</p>
        )}
      </CardContent>
    </Card>
  );
}
