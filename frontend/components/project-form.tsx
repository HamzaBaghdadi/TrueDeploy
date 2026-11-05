"use client";

import { useState } from "react";
import { api, type ProjectDetail, type ProjectView, type Server } from "@/lib/api";

const input =
  "w-full rounded-md border border-border bg-background px-3 py-2 text-sm outline-none placeholder:text-muted/60 focus:border-accent";
const label = "mb-1.5 block text-xs font-medium text-muted";

type EnvRow = { key: string; value: string };

function slug(name: string) {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40);
}

// Parses KEY=VALUE lines, ignoring comments, `export` and surrounding quotes.
function parseDotEnv(text: string): EnvRow[] {
  const rows: EnvRow[] = [];
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim().replace(/^export\s+/, "");
    if (!line || line.startsWith("#")) continue;
    const i = line.indexOf("=");
    if (i <= 0) continue;
    let value = line.slice(i + 1).trim();
    if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
      value = value.slice(1, -1);
    }
    rows.push({ key: line.slice(0, i).trim(), value });
  }
  return rows;
}

export function ProjectForm({
  servers,
  initial,
  onSaved,
  onCancel,
}: {
  servers: Server[];
  initial?: ProjectDetail;
  onSaved: (p: ProjectView) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(initial?.name ?? "");
  const [serverId, setServerId] = useState(initial?.serverId ?? servers[0]?.id ?? "");
  const [repoUrl, setRepoUrl] = useState(initial?.repoUrl ?? "");
  const [branch, setBranch] = useState(initial?.branch ?? "main");
  const [runtime, setRuntime] = useState<"compose" | "dockerfile">(
    (initial?.runtime as "compose" | "dockerfile") ?? "compose",
  );
  const [composeFile, setComposeFile] = useState(initial?.composeFile ?? "");
  const [hostPort, setHostPort] = useState(String(initial?.hostPort || 8080));
  const [containerPort, setContainerPort] = useState(String(initial?.containerPort || 3000));
  const [env, setEnv] = useState<EnvRow[]>(
    Object.entries(initial?.env ?? {}).map(([key, value]) => ({ key, value: value ?? "" })),
  );
  const [pasting, setPasting] = useState(false);
  const [paste, setPaste] = useState("");
  const [revealed, setRevealed] = useState<Set<number>>(new Set());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const p = await api.saveProject({
        id: initial?.id ?? "",
        name,
        serverId,
        repoUrl,
        branch,
        runtime,
        composeFile,
        hostPort: Number(hostPort) || 0,
        containerPort: Number(containerPort) || 0,
        env: Object.fromEntries(env.filter((r) => r.key.trim()).map((r) => [r.key.trim(), r.value])),
      });
      onSaved(p);
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  }

  function applyPaste() {
    const parsed = parseDotEnv(paste);
    setEnv((rows) => {
      const merged = new Map(rows.filter((r) => r.key).map((r) => [r.key, r.value]));
      parsed.forEach((r) => merged.set(r.key, r.value));
      return [...merged].map(([key, value]) => ({ key, value }));
    });
    setPaste("");
    setPasting(false);
  }

  const dir = slug(name);

  if (servers.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted">
        Add a server first. Projects are deployed to one of your servers.
      </div>
    );
  }

  return (
    <form onSubmit={save} className="space-y-6">
      <section className="grid gap-4 rounded-lg border border-border bg-surface p-5 sm:grid-cols-2">
        <div>
          <label className={label}>Project name</label>
          <input className={input} value={name} onChange={(e) => setName(e.target.value)} placeholder="my-api" required autoFocus />
          <p className="mt-1 font-mono text-[11px] text-muted">{dir ? `~/apps/${dir}` : " "}</p>
        </div>
        <div>
          <label className={label}>Server</label>
          <select className={input} value={serverId} onChange={(e) => setServerId(e.target.value)}>
            {servers.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name} ({s.user}@{s.host})
              </option>
            ))}
          </select>
        </div>
        <div className="sm:col-span-2 grid gap-4 sm:grid-cols-[1fr_180px]">
          <div>
            <label className={label}>Git repository</label>
            <input
              className={`${input} font-mono`}
              value={repoUrl}
              onChange={(e) => setRepoUrl(e.target.value)}
              placeholder="git@github.com:you/my-api.git"
              spellCheck={false}
              required
            />
            <p className="mt-1 text-[11px] text-muted">
              Private repos: use the SSH URL. You&apos;ll add the server&apos;s deploy key after saving.
            </p>
          </div>
          <div>
            <label className={label}>Branch</label>
            <input className={`${input} font-mono`} value={branch} onChange={(e) => setBranch(e.target.value)} spellCheck={false} />
          </div>
        </div>
      </section>

      <section className="rounded-lg border border-border bg-surface p-5">
        <label className={label}>How to run it</label>
        <div className="grid gap-3 sm:grid-cols-2">
          {(
            [
              ["compose", "Docker Compose", "Uses compose.yaml / docker-compose.yml from the repo. Best for apps with a database or several services."],
              ["dockerfile", "Dockerfile", "Builds the Dockerfile in the repo root and runs one container on a port."],
            ] as const
          ).map(([id, title, body]) => (
            <button
              type="button"
              key={id}
              onClick={() => setRuntime(id)}
              className={`rounded-lg border p-4 text-left transition-colors ${
                runtime === id ? "border-accent bg-accent/5" : "border-border hover:border-accent/60"
              }`}
            >
              <div className="text-sm font-medium">{title}</div>
              <div className="mt-1 text-[13px] text-muted">{body}</div>
            </button>
          ))}
        </div>
        <div className="mt-4">
          {runtime === "compose" ? (
            <div className="max-w-sm">
              <label className={label}>Compose file (optional)</label>
              <input
                className={`${input} font-mono`}
                value={composeFile}
                onChange={(e) => setComposeFile(e.target.value)}
                placeholder="auto-detect"
                spellCheck={false}
              />
            </div>
          ) : (
            <div className="grid max-w-sm grid-cols-2 gap-3">
              <div>
                <label className={label}>Public port</label>
                <input className={input} value={hostPort} onChange={(e) => setHostPort(e.target.value)} inputMode="numeric" />
              </div>
              <div>
                <label className={label}>Container port</label>
                <input className={input} value={containerPort} onChange={(e) => setContainerPort(e.target.value)} inputMode="numeric" />
              </div>
            </div>
          )}
        </div>
      </section>

      <section className="rounded-lg border border-border bg-surface p-5">
        <div className="mb-3 flex items-center justify-between">
          <div>
            <div className="text-sm font-medium">Environment variables</div>
            <div className="text-[12px] text-muted">Written to .env on the server. Stored encrypted on this computer.</div>
          </div>
          <div className="flex gap-3 text-xs">
            <button type="button" className="text-accent" onClick={() => setPasting(!pasting)}>
              {pasting ? "Close" : "Paste .env"}
            </button>
            <button type="button" className="text-accent" onClick={() => setEnv([...env, { key: "", value: "" }])}>
              + Add
            </button>
          </div>
        </div>
        {pasting && (
          <div className="mb-3">
            <textarea
              className={`${input} h-32 font-mono text-xs`}
              value={paste}
              onChange={(e) => setPaste(e.target.value)}
              placeholder={"DATABASE_URL=postgres://...\nSECRET_KEY=..."}
              spellCheck={false}
            />
            <button type="button" onClick={applyPaste} className="mt-2 rounded-md bg-surface-2 px-3 py-1.5 text-sm hover:bg-border">
              Add {parseDotEnv(paste).length || ""} variables
            </button>
          </div>
        )}
        {env.length === 0 ? (
          <p className="text-[13px] text-muted">No variables.</p>
        ) : (
          <div className="space-y-2">
            {env.map((row, i) => (
              <div key={i} className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)_auto_auto] gap-2">
                <input
                  className={`${input} font-mono text-xs`}
                  value={row.key}
                  placeholder="KEY"
                  spellCheck={false}
                  onChange={(e) => setEnv(env.map((r, j) => (j === i ? { ...r, key: e.target.value } : r)))}
                />
                <input
                  className={`${input} font-mono text-xs`}
                  type={revealed.has(i) ? "text" : "password"}
                  value={row.value}
                  placeholder="value"
                  spellCheck={false}
                  onChange={(e) => setEnv(env.map((r, j) => (j === i ? { ...r, value: e.target.value } : r)))}
                />
                <button
                  type="button"
                  className="px-2 text-xs text-muted hover:text-foreground"
                  onClick={() =>
                    setRevealed((s) => {
                      const n = new Set(s);
                      if (n.has(i)) n.delete(i);
                      else n.add(i);
                      return n;
                    })
                  }
                >
                  {revealed.has(i) ? "Hide" : "Show"}
                </button>
                <button
                  type="button"
                  className="px-2 text-muted hover:text-danger"
                  aria-label="Remove variable"
                  onClick={() => setEnv(env.filter((_, j) => j !== i))}
                >
                  ✕
                </button>
              </div>
            ))}
          </div>
        )}
      </section>

      {error && <p className="selectable rounded-md bg-danger/10 p-3 text-sm text-danger">{error}</p>}
      <div className="flex gap-2">
        <button type="submit" disabled={busy} className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-ink disabled:opacity-50">
          {busy ? "Saving…" : initial ? "Save changes" : "Create project"}
        </button>
        <button type="button" onClick={onCancel} className="px-3 py-2 text-sm text-muted hover:text-foreground">
          Cancel
        </button>
      </div>
    </form>
  );
}
