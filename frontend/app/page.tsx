"use client";

import { useCallback, useEffect, useState } from "react";
import { api, type Server, type ServerInfo } from "@/lib/api";
import { AddServerForm } from "@/components/add-server-form";
import { ServerInfoPanel } from "@/components/server-info";

type Check = { busy: boolean; info?: ServerInfo; error?: string };

export default function ServersPage() {
  const [servers, setServers] = useState<Server[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [checks, setChecks] = useState<Record<string, Check>>({});

  const load = useCallback(async () => {
    try {
      setServers(await api.listServers());
      setLoadError(null);
    } catch (e) {
      setLoadError((e as Error).message);
      setServers([]);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function check(id: string) {
    setChecks((c) => ({ ...c, [id]: { busy: true } }));
    try {
      const info = await api.testSavedServer(id);
      setChecks((c) => ({ ...c, [id]: { busy: false, info } }));
    } catch (e) {
      setChecks((c) => ({ ...c, [id]: { busy: false, error: (e as Error).message } }));
    }
  }

  async function remove(s: Server) {
    if (!confirm(`Remove "${s.name}"? Its saved credentials will be deleted.`)) return;
    await api.deleteServer(s.id);
    void load();
  }

  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      <header className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">{adding ? "Add server" : "Servers"}</h1>
          <p className="mt-1 text-sm text-muted">
            {adding
              ? "Connect over SSH. Credentials are encrypted and stored in your OS keychain."
              : "Machines TrueDeploy can set up and deploy to."}
          </p>
        </div>
        {!adding && (
          <button
            onClick={() => setAdding(true)}
            className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-ink"
          >
            + Add server
          </button>
        )}
      </header>

      {loadError && !adding && <p className="mb-4 rounded-md bg-danger/10 p-3 text-sm text-danger">{loadError}</p>}

      {adding ? (
        <AddServerForm
          onCancel={() => setAdding(false)}
          onSaved={() => {
            setAdding(false);
            void load();
          }}
        />
      ) : servers === null ? (
        <p className="text-sm text-muted">Loading…</p>
      ) : servers.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-12 text-center">
          <p className="text-sm text-muted">No servers yet.</p>
          <button onClick={() => setAdding(true)} className="mt-3 text-sm text-accent">
            Add your first server →
          </button>
        </div>
      ) : (
        <ul className="space-y-3">
          {servers.map((s) => {
            const c = checks[s.id];
            return (
              <li key={s.id} className="rounded-lg border border-border bg-surface p-4">
                <div className="flex items-center gap-4">
                  <span
                    className={`size-2 rounded-full ${
                      c?.info ? "bg-accent" : c?.error ? "bg-danger" : "bg-muted/40"
                    }`}
                  />
                  <div className="min-w-0 flex-1">
                    <div className="font-medium">{s.name}</div>
                    <div className="selectable font-mono text-xs text-muted">
                      {s.user}@{s.host}:{s.port} · {s.auth === "key" ? "SSH key" : "password"}
                    </div>
                  </div>
                  <button
                    onClick={() => check(s.id)}
                    disabled={c?.busy}
                    className="rounded-md border border-border px-3 py-1.5 text-sm hover:border-accent disabled:opacity-50"
                  >
                    {c?.busy ? "Checking…" : "Check"}
                  </button>
                  <button onClick={() => remove(s)} className="px-2 py-1.5 text-sm text-muted hover:text-danger">
                    Remove
                  </button>
                </div>
                {c?.error && <p className="selectable mt-3 rounded-md bg-danger/10 p-3 text-sm text-danger">{c.error}</p>}
                {c?.info && (
                  <div className="mt-3 border-t border-border pt-2">
                    <ServerInfoPanel info={c.info} />
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
