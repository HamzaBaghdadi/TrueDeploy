"use client";

import { KeyRound, Loader2, LockKeyhole, RefreshCw, ShieldCheck, ShieldOff } from "lucide-react";
import { useState } from "react";
import { api, type Server } from "@/lib/api";
import { useConfirm } from "@/components/confirm-dialog";
import { CopyButton } from "@/components/copy-button";
import { Notice } from "@/components/notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

/** How TrueDeploy logs in to the server, and switching it to an SSH key. */
export function ServerSecurity({ server }: { server: Server }) {
  const confirm = useConfirm();
  const [auth, setAuth] = useState(server.auth);
  const [busy, setBusy] = useState<"" | "key" | "pw">("");
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [admin, setAdmin] = useState("");
  const usesKey = auth === "key";

  async function newKey() {
    const ok = await confirm({
      title: usesKey ? "Replace TrueDeploy's login key?" : "Switch to an SSH key?",
      message: usesKey
        ? "A new key is created and installed, tested, and then saved. The previous key TrueDeploy created is removed from the server; keys you added yourself aren't touched."
        : "TrueDeploy creates its own SSH key, installs it with your password, checks that it works and then deletes the saved password. Your password keeps working for you. If anything fails, nothing changes.",
      confirmLabel: usesKey ? "Replace the key" : "Switch to a key",
    });
    if (!ok) return;
    setBusy("key");
    setMsg(null);
    setAdmin("");
    try {
      setMsg({ ok: true, text: await api.useNewKey(server.id) });
      setAuth("key");
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    } finally {
      setBusy("");
    }
  }

  async function passwordsOff() {
    const ok = await confirm({
      title: `Turn off password logins on ${server.name}?`,
      message:
        "This affects every account on the server: from now on SSH only accepts keys. Make sure everyone who logs in has a key first (or your hosting provider's console as a fallback).",
      confirmLabel: "Turn off password logins",
      tone: "danger",
    });
    if (!ok) return;
    setBusy("pw");
    setMsg(null);
    setAdmin("");
    try {
      const r = await api.disablePasswordLogin(server.id);
      if (r.adminCommands) {
        setAdmin(r.adminCommands);
        setMsg({ ok: false, text: `${server.user} can't use sudo, so an admin needs to run these commands on the server:` });
      } else {
        setMsg({ ok: true, text: r.message });
      }
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    } finally {
      setBusy("");
    }
  }

  return (
    <Card className="mt-6">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <LockKeyhole className="size-4 text-muted-foreground" />
          Login security
        </CardTitle>
        <CardDescription>How TrueDeploy logs in to {server.name}.</CardDescription>
        <CardAction>
          {usesKey ? (
            <Badge variant="outline" className="border-success/25 bg-success/10 text-success">
              <ShieldCheck /> SSH key
            </Badge>
          ) : (
            <Badge variant="outline" className="border-warning/25 bg-warning/5 text-warning">
              <KeyRound /> Password
            </Badge>
          )}
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-[13px] text-muted-foreground">
          {usesKey
            ? "TrueDeploy uses an SSH key. For the best protection, also turn off password logins so nobody can guess their way in."
            : "Passwords can be guessed by the bots that scan every server on the internet. An SSH key can't, and it's how TrueDeploy should log in."}
        </p>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant={usesKey ? "outline" : "default"} onClick={() => void newKey()} disabled={!!busy}>
            {busy === "key" ? <Loader2 className="animate-spin" /> : usesKey ? <RefreshCw /> : <KeyRound />}
            {busy === "key" ? "Installing and testing…" : usesKey ? "Replace the key" : "Switch to an SSH key"}
          </Button>
          {usesKey && (
            <Button size="sm" variant="outline" onClick={() => void passwordsOff()} disabled={!!busy}>
              {busy === "pw" ? <Loader2 className="animate-spin" /> : <ShieldOff />}
              Turn off password logins
            </Button>
          )}
        </div>
        {msg && <Notice tone={msg.ok ? "success" : admin ? "warning" : "error"}>{msg.text}</Notice>}
        {admin && (
          <div className="flex items-start gap-2 rounded-lg bg-terminal p-3">
            <pre className="selectable flex-1 overflow-x-auto font-mono text-[11px] leading-relaxed">{admin}</pre>
            <CopyButton text={admin} />
          </div>
        )}
      </CardContent>
    </Card>
  );
}
