"use client";

import { FileKey, FolderOpen, KeyRound, Loader2, Plug, Save, ShieldCheck } from "lucide-react";
import { useState } from "react";
import { api, type AuthMethod, type ServerInfo, type Target } from "@/lib/api";
import { ServerInfoPanel } from "@/components/server-info";
import { Notice } from "@/components/notice";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

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
        <div className="space-y-1.5">
          <Label htmlFor="srv-name">Name</Label>
          <Input id="srv-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="production-1" />
        </div>
        <div className="grid grid-cols-[1fr_90px] gap-3">
          <div className="space-y-1.5">
            <Label htmlFor="srv-host">Host / IP</Label>
            <Input
              id="srv-host"
              value={host}
              onChange={(e) => edit(setHost)(e.target.value)}
              placeholder="203.0.113.10"
              required
              autoFocus
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="srv-port">Port</Label>
            <Input id="srv-port" value={port} onChange={(e) => edit(setPort)(e.target.value)} inputMode="numeric" />
          </div>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="srv-user">Username</Label>
          <Input id="srv-user" value={user} onChange={(e) => edit(setUser)(e.target.value)} required />
        </div>

        <div className="space-y-1.5">
          <Label>Authentication</Label>
          <ToggleGroup
            type="single"
            variant="outline"
            spacing={0}
            value={auth}
            onValueChange={(v) => v && edit(setAuth)(v as AuthMethod)}
          >
            <ToggleGroupItem value="key">
              <KeyRound />
              SSH key
            </ToggleGroupItem>
            <ToggleGroupItem value="password">
              <ShieldCheck />
              Password
            </ToggleGroupItem>
          </ToggleGroup>
        </div>

        {auth === "password" ? (
          <div className="space-y-1.5">
            <Label htmlFor="srv-password">Password</Label>
            <Input
              id="srv-password"
              type="password"
              value={password}
              onChange={(e) => edit(setPassword)(e.target.value)}
              required
            />
          </div>
        ) : (
          <>
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <Label htmlFor="srv-key">Private key</Label>
                <Button type="button" variant="link" size="xs" onClick={() => setPasteKey(!pasteKey)}>
                  {pasteKey ? <FolderOpen /> : <FileKey />}
                  {pasteKey ? "Use a key file instead" : "Paste key instead"}
                </Button>
              </div>
              {pasteKey ? (
                <Textarea
                  id="srv-key"
                  className="h-32 font-mono text-xs"
                  value={privateKey}
                  onChange={(e) => edit(setPrivateKey)(e.target.value)}
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                  required
                />
              ) : (
                <div className="flex gap-2">
                  <Input
                    id="srv-key"
                    className="font-mono text-xs"
                    value={keyPath}
                    onChange={(e) => edit(setKeyPath)(e.target.value)}
                    placeholder="C:\Users\you\.ssh\id_ed25519"
                    required
                  />
                  <Button type="button" variant="outline" onClick={browse}>
                    <FolderOpen />
                    Browse…
                  </Button>
                </div>
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="srv-passphrase">Key passphrase (if any)</Label>
              <Input
                id="srv-passphrase"
                type="password"
                value={passphrase}
                onChange={(e) => edit(setPassphrase)(e.target.value)}
              />
            </div>
          </>
        )}

        <div className="flex gap-2 pt-2">
          <Button type="submit" variant="outline" disabled={busy !== null}>
            {busy === "test" ? <Loader2 className="animate-spin" /> : <Plug />}
            {busy === "test" ? "Connecting…" : "Test connection"}
          </Button>
          <Button
            type="button"
            onClick={save}
            disabled={!info || busy !== null}
            title={info ? undefined : "Test the connection first"}
          >
            {busy === "save" ? <Loader2 className="animate-spin" /> : <Save />}
            {busy === "save" ? "Saving…" : "Save server"}
          </Button>
          <Button type="button" variant="ghost" onClick={onCancel} className="ml-auto">
            Cancel
          </Button>
        </div>
      </form>

      <Card size="sm" className="h-fit">
        <CardHeader>
          <CardTitle>Connection result</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {busy === "test" && (
            <p className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" />
              Connecting to {host}…
            </p>
          )}
          {error && <Notice tone="error">{error}</Notice>}
          {info && (
            <>
              <Notice tone="success">Connected successfully.</Notice>
              <ServerInfoPanel info={info} />
              <p className="text-xs text-muted-foreground">
                Saving pins this host key. TrueDeploy will refuse to connect if it changes.
              </p>
            </>
          )}
          {!busy && !error && !info && (
            <p className="text-sm text-muted-foreground">Fill in the details and press “Test connection”.</p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
