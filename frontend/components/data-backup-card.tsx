"use client";

import { Download, FileUp, Loader2, LockKeyhole, RotateCw, ShieldCheck, TriangleAlert, Upload } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { SnapshotSummary } from "@/bindings/truedeploy/internal/store";
import { Notice } from "@/components/notice";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { isSet, timeAgo } from "@/lib/format";
import { cn } from "@/lib/utils";

const MIN = 10;

/** Export everything TrueDeploy knows to one encrypted file, and import it back. */
export function DataBackupCard() {
  const [status, setStatus] = useState<{ lastExport?: string; changed: boolean; hasData: boolean } | null>(null);
  const loadStatus = useCallback(() => api.exportStatus().then(setStatus, () => {}), []);
  useEffect(() => {
    void loadStatus();
  }, [loadStatus]);

  const never = !status?.lastExport || !isSet(status.lastExport);
  const due = !!status?.hasData && (never || status.changed);

  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <LockKeyhole className="size-4 text-muted-foreground" />
          Backup of TrueDeploy
        </CardTitle>
        <CardDescription>
          Servers, keys and passwords, projects, environment variables, database passwords, Git provider tokens and alert channels live only on this
          computer. Export them to one encrypted file to move to another computer or recover after a reinstall.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        {status &&
          (due ? (
            <Notice tone="warning">
              {never
                ? "You haven't exported a backup yet. If this computer is lost, so are your server credentials and the generated database passwords."
                : `Your setup changed since the last export (${timeAgo(status.lastExport!)}). Export again to keep the backup current.`}
            </Notice>
          ) : status.hasData ? (
            <p className="flex items-center gap-2 text-[13px] text-success">
              <ShieldCheck className="size-4" /> Backed up {timeAgo(status.lastExport!)}, nothing changed since.
            </p>
          ) : null)}
        <ExportForm onDone={() => void loadStatus()} highlight={due} />
        <div className="border-t border-border" />
        <ImportFlow />
      </CardContent>
    </Card>
  );
}

function ExportForm({ onDone, highlight }: { onDone: () => void; highlight: boolean }) {
  const [pass, setPass] = useState("");
  const [again, setAgain] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const mismatch = again.length > 0 && pass !== again;
  const short = pass.length > 0 && pass.length < MIN;

  async function exportNow(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setMsg(null);
    try {
      const path = await api.exportData(pass);
      if (path) {
        setMsg({ ok: true, text: `Saved to ${path}. Keep the passphrase somewhere safe (a password manager): without it the file can't be opened.` });
        setPass("");
        setAgain("");
        onDone();
      }
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message });
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={exportNow} className="space-y-3">
      <div className="text-sm font-medium">Export</div>
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor="bk-pass" className="text-xs text-muted-foreground">
            Passphrase (at least {MIN} characters)
          </Label>
          <Input id="bk-pass" type="password" value={pass} onChange={(e) => setPass(e.target.value)} autoComplete="new-password" aria-invalid={short} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="bk-again" className="text-xs text-muted-foreground">
            Repeat it
          </Label>
          <Input id="bk-again" type="password" value={again} onChange={(e) => setAgain(e.target.value)} autoComplete="new-password" aria-invalid={mismatch} />
        </div>
      </div>
      <p className={cn("text-[11px]", mismatch || short ? "text-destructive" : "text-muted-foreground")}>
        {mismatch
          ? "The passphrases don't match."
          : short
            ? `${MIN - pass.length} more character(s).`
            : "Encrypted with Argon2id and AES-256-GCM: safe to keep in cloud storage. There is no way to recover a forgotten passphrase."}
      </p>
      <Button type="submit" variant={highlight ? "default" : "outline"} disabled={busy || pass.length < MIN || pass !== again}>
        {busy ? <Loader2 className="animate-spin" /> : <Download />}
        {busy ? "Encrypting…" : "Export backup…"}
      </Button>
      {msg && <Notice tone={msg.ok ? "success" : "error"}>{msg.text}</Notice>}
    </form>
  );
}

function ImportFlow() {
  const [path, setPath] = useState("");
  const [pass, setPass] = useState("");
  const [summary, setSummary] = useState<SnapshotSummary | null>(null);
  const [mode, setMode] = useState<"merge" | "replace">("merge");
  const [busy, setBusy] = useState<"" | "open" | "import">("");
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [done, setDone] = useState(false);

  async function pick() {
    setMsg(null);
    try {
      const p = await api.pickBackup();
      if (p) {
        setPath(p);
        setSummary(null);
        setPass("");
        setDone(false);
      }
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    }
  }

  async function open(e: React.FormEvent) {
    e.preventDefault();
    setBusy("open");
    setMsg(null);
    try {
      setSummary(await api.previewImport(path, pass));
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message });
    } finally {
      setBusy("");
    }
  }

  async function importNow() {
    setBusy("import");
    setMsg(null);
    try {
      setMsg({ ok: true, text: await api.importData(path, pass, mode === "replace") });
      setDone(true);
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message });
    } finally {
      setBusy("");
    }
  }

  const list = (label: string, items?: string[] | null) =>
    items && items.length > 0 ? (
      <li>
        <span className="text-muted-foreground">{label}:</span> {items.join(", ")}
      </li>
    ) : null;

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-sm font-medium">Import</div>
        <Button type="button" variant="outline" size="sm" onClick={() => void pick()} disabled={!!busy}>
          <FileUp /> {path ? "Choose another file…" : "Choose a backup file…"}
        </Button>
      </div>
      {path && !summary && (
        <form onSubmit={open} className="flex flex-wrap items-end gap-2">
          <div className="min-w-64 flex-1 space-y-1.5">
            <Label htmlFor="imp-pass" className="text-xs text-muted-foreground">
              Passphrase of <span className="font-mono">{path.split(/[\\/]/).pop()}</span>
            </Label>
            <Input id="imp-pass" type="password" value={pass} onChange={(e) => setPass(e.target.value)} autoComplete="off" autoFocus />
          </div>
          <Button type="submit" disabled={!pass || !!busy}>
            {busy === "open" ? <Loader2 className="animate-spin" /> : <LockKeyhole />}
            {busy === "open" ? "Decrypting…" : "Open"}
          </Button>
        </form>
      )}
      {summary && !done && (
        <div className="space-y-3 rounded-lg border border-border bg-background/40 p-3">
          <div className="text-[13px]">
            Exported {timeAgo(summary.exportedAt)}. It contains:
            <ul className="mt-1 space-y-0.5 text-[13px]">
              {list("Servers", summary.servers)}
              {list("Projects", summary.projects)}
              {list("Databases", summary.databases)}
              {summary.channels > 0 && (
                <li>
                  <span className="text-muted-foreground">Alert channels:</span> {summary.channels}
                </li>
              )}
              {summary.github && (
                <li>
                  <span className="text-muted-foreground">GitHub:</span> @{summary.github}
                </li>
              )}
            </ul>
          </div>
          <RadioGroup value={mode} onValueChange={(v) => setMode(v as "merge" | "replace")} className="gap-2">
            <Label className="flex items-start gap-2.5 font-normal">
              <RadioGroupItem value="merge" className="mt-0.5" />
              <span>
                <span className="block text-sm">Merge</span>
                <span className="block text-[12px] text-muted-foreground">
                  Add what&apos;s missing on this computer. Anything already here stays as it is.
                </span>
              </span>
            </Label>
            <Label className="flex items-start gap-2.5 font-normal">
              <RadioGroupItem value="replace" className="mt-0.5" />
              <span>
                <span className="block text-sm">Replace</span>
                <span className="block text-[12px] text-muted-foreground">
                  This computer&apos;s data becomes exactly the backup&apos;s. A safety copy of the current data is saved first.
                </span>
              </span>
            </Label>
          </RadioGroup>
          {mode === "replace" && (
            <p className="flex items-center gap-1.5 text-[12px] text-warning">
              <TriangleAlert className="size-3.5" /> Servers and projects that aren&apos;t in the backup disappear from this computer (nothing
              changes on the servers).
            </p>
          )}
          <Button onClick={() => void importNow()} disabled={!!busy} variant={mode === "replace" ? "destructive" : "default"}>
            {busy === "import" ? <Loader2 className="animate-spin" /> : <Upload />}
            {mode === "replace" ? "Replace with this backup" : "Merge into this computer"}
          </Button>
        </div>
      )}
      {msg && <Notice tone={msg.ok ? "success" : "error"}>{msg.text}</Notice>}
      {done && (
        <Button variant="outline" size="sm" onClick={() => window.location.reload()}>
          <RotateCw /> Reload to see the imported data
        </Button>
      )}
    </div>
  );
}
