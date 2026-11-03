"use client";

import { useState } from "react";
import { api, type AuthMethod, type ServerInfo, type Target } from "@/lib/api";
import { ServerInfoPanel } from "@/components/server-info";

const input =
  "w-full rounded-md border border-border bg-background px-3 py-2 text-sm outline-none placeholder:text-muted/60 focus:border-accent";
const label = "mb-1.5 block text-xs font-medium text-muted";

export function AddServerForm({ onSaved, onCancel }: { onSaved: () => void; onCancel: () => void }) {
  const [name, setName] = useState("");
  const [host, setHost] = useState("");
  const [port, setPort] = useState("22");
  const [user, setUser] = useState("root");
  const [auth, setAuth] = useState<AuthMethod>("key");
  const [password, setPassword] = useState("");
  const [keyPath, setKeyPath] = useState("");
  const [privateKey, setPrivateKey] = useState("");
  const [passphrase, setPassphrase] = useState("");
  const [pasteKey, setPasteKey] = useState(false);

  const [busy, setBusy] = useState<"test" | "save" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [info, setInfo] = useState<ServerInfo | null>(null);

  const target = (): Target => ({
    host: host.trim(),
    port: Number(port) || 22,
    user: user.trim(),
    auth,
    ...(auth === "password"
      ? { password }
      : pasteKey
        ? { privateKey, passphrase }
        : { keyPath, passphrase }),
  });

  // Any edit invalidates the previous test (and the host key it pinned).
  const edit =
    <T,>(set: (v: T) => void) =>
    (v: T) => {
      set(v);
      setInfo(null);
      setError(null);
    };

  async function test() {
    setBusy("test");
    setError(null);
    setInfo(null);
    try {
      setInfo(await api.testConnection(target()));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(null);
    }
  }

  async function save() {
    if (!info) return;
    setBusy("save");
    try {
      await api.saveServer(name.trim() || host.trim(), {
        ...target(),
        hostKeyFingerprint: info.fingerprint,
      });
      onSaved();
    } catch (e) {
      setError((e as Error).message);
      setBusy(null);
    }
  }

  async function browse() {
    try {
      const p = await api.pickKeyFile();
      if (p) edit(setKeyPath)(p);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  return (
    <div className="grid gap-6 lg:grid-cols-[1fr_340px]">
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          void test();
        }}
      >
        <div>
          <label className={label}>Name</label>
          <input className={input} value={name} onChange={(e) => setName(e.target.value)} placeholder="production-1" />
        </div>
        <div className="grid grid-cols-[1fr_90px] gap-3">
          <div>
            <label className={label}>Host / IP</label>
            <input
              className={input}
              value={host}
              onChange={(e) => edit(setHost)(e.target.value)}
              placeholder="203.0.113.10"
              required
              autoFocus
            />
          </div>
          <div>
            <label className={label}>Port</label>
            <input className={input} value={port} onChange={(e) => edit(setPort)(e.target.value)} inputMode="numeric" />
          </div>
        </div>
        <div>
          <label className={label}>Username</label>
          <input className={input} value={user} onChange={(e) => edit(setUser)(e.target.value)} required />
        </div>

        <div>
          <label className={label}>Authentication</label>
          <div className="inline-flex rounded-md border border-border bg-background p-0.5 text-sm">
            {(["key", "password"] as const).map((m) => (
              <button
                key={m}
                type="button"
                onClick={() => edit(setAuth)(m)}
                className={`rounded px-3 py-1.5 ${auth === m ? "bg-surface-2 text-foreground" : "text-muted"}`}
              >
                {m === "key" ? "SSH key" : "Password"}
              </button>
            ))}
          </div>
        </div>

        {auth === "password" ? (
          <div>
            <label className={label}>Password</label>
            <input
              className={input}
              type="password"
              value={password}
              onChange={(e) => edit(setPassword)(e.target.value)}
              required
            />
          </div>
        ) : (
          <>
            <div>
              <div className="mb-1.5 flex items-center justify-between">
                <span className="text-xs font-medium text-muted">Private key</span>
                <button type="button" className="text-xs text-accent" onClick={() => setPasteKey(!pasteKey)}>
                  {pasteKey ? "Use a key file instead" : "Paste key instead"}
                </button>
              </div>
              {pasteKey ? (
                <textarea
                  className={`${input} h-32 font-mono text-xs`}
                  value={privateKey}
                  onChange={(e) => edit(setPrivateKey)(e.target.value)}
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                  required
                />
              ) : (
                <div className="flex gap-2">
                  <input
                    className={`${input} font-mono text-xs`}
                    value={keyPath}
                    onChange={(e) => edit(setKeyPath)(e.target.value)}
                    placeholder="C:\Users\you\.ssh\id_ed25519"
                    required
                  />
                  <button
                    type="button"
                    onClick={browse}
                    className="shrink-0 rounded-md border border-border px-3 text-sm hover:bg-surface-2"
                  >
                    Browse…
                  </button>
                </div>
              )}
            </div>
            <div>
              <label className={label}>Key passphrase (if any)</label>
              <input
                className={input}
                type="password"
                value={passphrase}
                onChange={(e) => edit(setPassphrase)(e.target.value)}
              />
            </div>
          </>
        )}

        <div className="flex gap-2 pt-2">
          <button
            type="submit"
            disabled={busy !== null}
            className="rounded-md border border-border bg-surface-2 px-4 py-2 text-sm font-medium hover:border-accent disabled:opacity-50"
          >
            {busy === "test" ? "Connecting…" : "Test connection"}
          </button>
          <button
            type="button"
            onClick={save}
            disabled={!info || busy !== null}
            title={info ? undefined : "Test the connection first"}
            className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-ink disabled:opacity-40"
          >
            {busy === "save" ? "Saving…" : "Save server"}
          </button>
          <button type="button" onClick={onCancel} className="ml-auto px-3 py-2 text-sm text-muted hover:text-foreground">
            Cancel
          </button>
        </div>
      </form>

      <div className="rounded-lg border border-border bg-surface p-4">
        <h3 className="mb-3 text-sm font-medium">Connection result</h3>
        {busy === "test" && <p className="text-sm text-muted">Connecting to {host}…</p>}
        {error && <p className="selectable rounded-md bg-danger/10 p-3 text-sm text-danger">{error}</p>}
        {info && (
          <>
            <p className="mb-3 rounded-md bg-accent/10 p-3 text-sm text-accent">Connected successfully.</p>
            <ServerInfoPanel info={info} />
            <p className="mt-3 text-xs text-muted">
              Saving pins this host key. TrueDeploy will refuse to connect if it changes.
            </p>
          </>
        )}
        {!busy && !error && !info && (
          <p className="text-sm text-muted">Fill in the details and press “Test connection”.</p>
        )}
      </div>
    </div>
  );
}
