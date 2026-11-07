"use client";

import { Braces, ClipboardPaste, Eye, EyeOff, Loader2, Pencil, Plus, Rocket, Save, Trash2, X } from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { api, type EnvGroupView } from "@/lib/api";
import { useConfirm } from "@/components/confirm-dialog";
import { Notice } from "@/components/notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { timeAgo } from "@/lib/format";

type Row = { key: string; value: string };

// KEY=VALUE lines; comments, `export` and surrounding quotes are ignored.
function parseDotEnv(text: string): Row[] {
  const rows: Row[] = [];
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim().replace(/^export\s+/, "");
    if (!line || line.startsWith("#")) continue;
    const i = line.indexOf("=");
    if (i <= 0) continue;
    let value = line.slice(i + 1).trim();
    if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) value = value.slice(1, -1);
    rows.push({ key: line.slice(0, i).trim(), value });
  }
  return rows;
}

export default function VariablesPage() {
  const confirm = useConfirm();
  const [groups, setGroups] = useState<EnvGroupView[] | null>(null);
  const [editing, setEditing] = useState<EnvGroupView | "new" | null>(null);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const load = useCallback(() => api.envGroups().then(setGroups, (e: Error) => setMsg({ ok: false, text: e.message })), []);
  useEffect(() => {
    void load();
  }, [load]);

  async function remove(g: EnvGroupView) {
    const ok = await confirm({
      title: `Delete ${g.name}?`,
      message: "Its variables are forgotten. Projects already deployed keep their current values until their next deploy.",
      confirmLabel: "Delete group",
      tone: "danger",
    });
    if (!ok) return;
    setMsg(null);
    try {
      await api.deleteEnvGroup(g.id);
      await load();
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    }
  }

  async function redeploy(g: EnvGroupView) {
    setBusy(g.id);
    setMsg(null);
    try {
      setMsg({ ok: true, text: await api.redeployEnvGroup(g.id) });
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="mx-auto max-w-4xl px-8 py-8">
      <header className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Variables</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Groups of environment variables shared by several projects, like a JWT secret used by both a frontend and a backend. A
            project&apos;s own variables override group values.
          </p>
        </div>
        {editing === null && (
          <Button onClick={() => setEditing("new")}>
            <Plus /> New group
          </Button>
        )}
      </header>

      {msg && (
        <Notice tone={msg.ok ? "success" : "error"} className="mb-4">
          {msg.text}
        </Notice>
      )}

      {editing !== null && (
        <GroupEditor
          key={editing === "new" ? "new" : editing.id}
          group={editing === "new" ? null : editing}
          onCancel={() => setEditing(null)}
          onSaved={(g) => {
            setEditing(null);
            setMsg({
              ok: true,
              text: g.usedBy?.length
                ? `Saved. ${g.usedBy.length} project(s) use it: redeploy them so the new values reach the servers.`
                : "Saved. Pick it in a project's settings to use it.",
            });
            void load();
          }}
        />
      )}

      {groups === null ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" /> Loading…
        </p>
      ) : groups.length === 0 && editing === null ? (
        <div className="flex flex-col items-center rounded-lg border border-dashed border-border p-12 text-center">
          <Braces className="mb-3 size-8 text-muted-foreground" />
          <p className="text-sm text-muted-foreground">No variable groups yet.</p>
          <Button variant="link" onClick={() => setEditing("new")} className="mt-1">
            Create the first one
          </Button>
        </div>
      ) : (
        <ul className="space-y-3">
          {groups.map((g) => {
            const keys = Object.keys(g.vars ?? {}).sort();
            return (
              <li key={g.id}>
                <Card size="sm">
                  <CardHeader>
                    <CardTitle className="flex items-center gap-2">
                      <Braces className="size-4 text-muted-foreground" /> {g.name}
                    </CardTitle>
                    <CardDescription>
                      {keys.length} variable{keys.length === 1 ? "" : "s"} · updated {timeAgo(g.updatedAt)}
                    </CardDescription>
                    <CardAction className="flex gap-1">
                      {(g.usedBy?.length ?? 0) > 0 && (
                        <Button size="xs" variant="outline" onClick={() => void redeploy(g)} disabled={busy !== null}>
                          {busy === g.id ? <Loader2 className="animate-spin" /> : <Rocket />} Redeploy all
                        </Button>
                      )}
                      <Button size="icon-sm" variant="ghost" aria-label={`Edit ${g.name}`} onClick={() => setEditing(g)}>
                        <Pencil />
                      </Button>
                      <Button
                        size="icon-sm"
                        variant="ghost"
                        aria-label={`Delete ${g.name}`}
                        className="text-muted-foreground hover:text-destructive"
                        onClick={() => void remove(g)}
                      >
                        <Trash2 />
                      </Button>
                    </CardAction>
                  </CardHeader>
                  <CardContent className="space-y-2">
                    <div className="flex flex-wrap gap-1.5">
                      {keys.map((k) => (
                        <Badge key={k} variant="secondary" className="font-mono text-[10px]">
                          {k}
                        </Badge>
                      ))}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      {(g.usedBy?.length ?? 0) === 0 ? (
                        "Not used by any project yet."
                      ) : (
                        <>
                          Used by{" "}
                          {g.usedBy!.map((p, i) => (
                            <span key={p.id}>
                              {i > 0 && ", "}
                              <Link href={`/projects/?id=${encodeURIComponent(p.id)}`} className="text-foreground hover:text-primary">
                                {p.name}
                              </Link>
                            </span>
                          ))}
                        </>
                      )}
                    </div>
                  </CardContent>
                </Card>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

function GroupEditor({ group, onCancel, onSaved }: { group: EnvGroupView | null; onCancel: () => void; onSaved: (g: EnvGroupView) => void }) {
  const [name, setName] = useState(group?.name ?? "");
  const [rows, setRows] = useState<Row[]>(() => {
    const r = Object.entries(group?.vars ?? {}).map(([key, value]) => ({ key, value: value ?? "" }));
    return r.length ? r.sort((a, b) => a.key.localeCompare(b.key)) : [{ key: "", value: "" }];
  });
  const [revealed, setRevealed] = useState<Set<number>>(new Set());
  const [pasting, setPasting] = useState(false);
  const [paste, setPaste] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const vars = Object.fromEntries(rows.filter((r) => r.key.trim()).map((r) => [r.key.trim(), r.value]));
      onSaved(await api.saveEnvGroup({ id: group?.id ?? "", name, vars }));
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  }

  return (
    <Card className="mb-6">
      <CardHeader>
        <CardTitle>{group ? `Edit ${group.name}` : "New variable group"}</CardTitle>
        <CardDescription>Stored encrypted on this computer and included in TrueDeploy&apos;s encrypted backup.</CardDescription>
        <CardAction className="flex gap-1">
          <Button type="button" variant="ghost" size="sm" onClick={() => setPasting(!pasting)}>
            {pasting ? <X /> : <ClipboardPaste />}
            {pasting ? "Close" : "Paste .env"}
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        <form onSubmit={save} className="space-y-4">
          <div className="max-w-sm space-y-1.5">
            <Label htmlFor="group-name">Name</Label>
            <Input id="group-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Production secrets" required autoFocus />
          </div>
          {pasting && (
            <div className="space-y-2">
              <Textarea className="h-28 font-mono text-xs" value={paste} onChange={(e) => setPaste(e.target.value)} placeholder={"JWT_SECRET=...\nSMTP_HOST=..."} spellCheck={false} />
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={() => {
                  const parsed = parseDotEnv(paste);
                  const merged = new Map(rows.filter((r) => r.key).map((r) => [r.key, r.value]));
                  parsed.forEach((r) => merged.set(r.key, r.value));
                  setRows([...merged].map(([key, value]) => ({ key, value })));
                  setPaste("");
                  setPasting(false);
                }}
              >
                <Plus /> Add {parseDotEnv(paste).length || ""} variables
              </Button>
            </div>
          )}
          <div className="space-y-2">
            {rows.map((row, i) => (
              <div key={i} className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)_auto_auto] gap-2">
                <Input
                  className="font-mono text-xs"
                  value={row.key}
                  placeholder="KEY"
                  spellCheck={false}
                  aria-label="Variable name"
                  onChange={(e) => setRows(rows.map((r, j) => (j === i ? { ...r, key: e.target.value } : r)))}
                />
                <Input
                  className="font-mono text-xs"
                  type={revealed.has(i) ? "text" : "password"}
                  value={row.value}
                  placeholder="value"
                  spellCheck={false}
                  aria-label="Variable value"
                  onChange={(e) => setRows(rows.map((r, j) => (j === i ? { ...r, value: e.target.value } : r)))}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={revealed.has(i) ? "Hide value" : "Show value"}
                  className="text-muted-foreground"
                  onClick={() =>
                    setRevealed((s) => {
                      const n = new Set(s);
                      if (n.has(i)) n.delete(i);
                      else n.add(i);
                      return n;
                    })
                  }
                >
                  {revealed.has(i) ? <EyeOff /> : <Eye />}
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label="Remove variable"
                  className="text-muted-foreground hover:text-destructive"
                  onClick={() => setRows(rows.filter((_, j) => j !== i))}
                >
                  <X />
                </Button>
              </div>
            ))}
            <Button type="button" variant="outline" size="sm" onClick={() => setRows([...rows, { key: "", value: "" }])}>
              <Plus /> Add variable
            </Button>
          </div>
          {error && <Notice tone="error">{error}</Notice>}
          <div className="flex gap-2">
            <Button type="submit" disabled={busy}>
              {busy ? <Loader2 className="animate-spin" /> : <Save />}
              {group ? "Save changes" : "Create group"}
            </Button>
            <Button type="button" variant="ghost" onClick={onCancel}>
              Cancel
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
