"use client";

import { ArrowRight, ChevronDown, ExternalLink, Info, KeyRound, Loader2, Rocket } from "lucide-react";
import { useEffect, useState } from "react";
import { api, type ProjectView, type Server, type Template } from "@/lib/api";
import { AppIcon } from "@/components/app-icon";
import { Notice } from "@/components/notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

/** Install form for one catalog app: creates the project, then deploys it. */
export function AppInstall({
  template: t,
  servers,
  onInstalled,
  onCancel,
}: {
  template: Template;
  servers: Server[];
  onInstalled: (p: ProjectView) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(t.name);
  const [serverId, setServerId] = useState(servers[0]?.id ?? "");
  const [domain, setDomain] = useState("");
  // The suggested port is remembered per server so a user's edit isn't overwritten.
  const [suggested, setSuggested] = useState<{ serverId: string; port: number } | null>(null);
  const [portEdit, setPortEdit] = useState<string | null>(null);
  const [vars, setVars] = useState<Record<string, string>>(() =>
    Object.fromEntries((t.vars ?? []).filter((v) => !v.secret).map((v) => [v.key, v.default ?? ""])),
  );
  const [showCompose, setShowCompose] = useState(false);
  const [busy, setBusy] = useState<"" | "saving" | "deploying" | "https">("");
  const [partial, setPartial] = useState<{ project: ProjectView; text: string } | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!serverId) return;
    let cancelled = false;
    api.suggestPort(serverId, t.id).then(
      (port) => !cancelled && setSuggested({ serverId, port }),
      () => !cancelled && setSuggested({ serverId, port: t.defaultPort }),
    );
    return () => {
      cancelled = true;
    };
  }, [serverId, t.id, t.defaultPort]);

  const checking = suggested?.serverId !== serverId;
  const port = portEdit ?? (checking ? "" : String(suggested?.port ?? ""));
  const host = domain.trim().toLowerCase().replace(/^https?:\/\//, "").replace(/[/?#].*$/, "");
  const server = servers.find((s) => s.id === serverId);
  const secrets = (t.vars ?? []).filter((v) => v.secret);
  const settings = (t.vars ?? []).filter((v) => !v.secret);

  async function install(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy("saving");
    let project: ProjectView;
    try {
      project = await api.createApp({
        template: t.id,
        name,
        serverId,
        domains: host ? [host] : [],
        port: Number(port) || 0,
        vars,
      });
    } catch (err) {
      setError((err as Error).message);
      setBusy("");
      return;
    }
    // Saved: from here on the project exists. Problems are shown here with a
    // way to open it, since the project page wouldn't explain them.
    setBusy("deploying");
    try {
      await api.deploy(project.id);
    } catch (err) {
      setPartial({ project, text: `${t.name} was created, but the deploy couldn't start: ${(err as Error).message}` });
      return;
    }
    if (host) {
      // The proxy can be configured before the app is up; it retries until it answers.
      setBusy("https");
      try {
        await api.applyDomains(project.id);
      } catch (err) {
        setPartial({
          project,
          text: `${t.name} is deploying, but HTTPS for ${host} isn't set up yet: ${(err as Error).message} You can retry from the project's Domains section.`,
        });
        return;
      }
    }
    onInstalled(project);
  }

  if (partial) {
    return (
      <div className="space-y-4">
        <Notice tone="warning">{partial.text}</Notice>
        <Button onClick={() => onInstalled(partial.project)}>
          Open {partial.project.name} <ArrowRight />
        </Button>
      </div>
    );
  }

  if (servers.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
        Add a server first. Apps are installed on one of your servers.
      </div>
    );
  }

  return (
    <form onSubmit={install} className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_300px]">
      <div className="space-y-6">
        <Card>
          <CardContent className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="app-name">Name</Label>
              <Input id="app-name" value={name} onChange={(e) => setName(e.target.value)} required autoFocus />
            </div>
            <div className="space-y-1.5">
              <Label>Server</Label>
              <Select value={serverId} onValueChange={setServerId}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {servers.map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {s.name} <span className="text-muted-foreground">({s.user}@{s.host})</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="app-domain">
                Domain <span className="font-normal text-muted-foreground">{t.wantsDomain ? "(recommended)" : "(optional)"}</span>
              </Label>
              <Input
                id="app-domain"
                className="font-mono"
                value={domain}
                onChange={(e) => setDomain(e.target.value)}
                placeholder={`${t.id.replace(/[^a-z]/g, "")}.example.com`}
                spellCheck={false}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="app-port">Port on the server</Label>
              <div className="relative">
                <Input
                  id="app-port"
                  className="font-mono"
                  value={port}
                  onChange={(e) => setPortEdit(e.target.value)}
                  inputMode="numeric"
                  placeholder={checking ? "Finding a free port…" : ""}
                  required
                />
                {checking && portEdit === null && (
                  <Loader2 className="absolute top-2.5 right-2.5 size-4 animate-spin text-muted-foreground" />
                )}
              </div>
            </div>
            <p className="text-[12px] text-muted-foreground sm:col-span-2">
              {host ? (
                <>
                  Served at <span className="font-mono text-foreground">https://{host}</span> with a free certificate. The app only
                  listens on 127.0.0.1, so the domain is the only way in. Point the domain&apos;s DNS A record at{" "}
                  <span className="font-mono">{server?.host}</span>.
                </>
              ) : (
                <>
                  Without a domain the app is published at{" "}
                  <span className="font-mono text-foreground">
                    http://{server?.host}:{port || "…"}
                  </span>{" "}
                  (open the port in the server&apos;s firewall if needed). You can add a domain later.
                </>
              )}
            </p>
            {t.wantsDomain && !host && (
              <Notice tone="warning" className="sm:col-span-2">
                {t.name} works best with a domain and HTTPS. You can add one later; check the notes first.
              </Notice>
            )}
          </CardContent>
        </Card>

        {(settings.length > 0 || secrets.length > 0) && (
          <Card>
            <CardHeader>
              <CardTitle>Settings</CardTitle>
              <CardDescription>Saved encrypted as the app&apos;s environment variables. You can change them later.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {settings.map((v) => (
                <div key={v.key} className="grid gap-1.5 sm:grid-cols-[200px_minmax(0,1fr)] sm:items-center sm:gap-4">
                  <Label htmlFor={`var-${v.key}`}>{v.label}</Label>
                  <div>
                    <Input
                      id={`var-${v.key}`}
                      className="font-mono"
                      value={vars[v.key] ?? ""}
                      onChange={(e) => setVars({ ...vars, [v.key]: e.target.value })}
                      spellCheck={false}
                    />
                    {v.hint && <p className="mt-1 text-[11px] text-muted-foreground">{v.hint}</p>}
                  </div>
                </div>
              ))}
              {secrets.length > 0 && (
                <ul className="space-y-2 rounded-lg border border-border bg-background/40 p-3">
                  {secrets.map((v) => (
                    <li key={v.key} className="flex items-start gap-2.5 text-[13px]">
                      <KeyRound className="mt-0.5 size-4 shrink-0 text-success" />
                      <span>
                        <span className="font-medium">{v.label}</span>
                        <span className="text-muted-foreground">: generated ({v.length || 32} random characters)</span>
                        {v.hint && <span className="block text-[12px] text-muted-foreground">{v.hint}</span>}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>
        )}

        {error && <Notice tone="error">{error}</Notice>}
        <div className="flex gap-2">
          <Button type="submit" disabled={!!busy || !serverId || !port}>
            {busy ? <Loader2 className="animate-spin" /> : <Rocket />}
            {busy === "saving" ? "Creating…" : busy === "deploying" ? "Starting deploy…" : busy === "https" ? "Setting up HTTPS…" : "Install and deploy"}
          </Button>
          <Button type="button" variant="ghost" onClick={onCancel} disabled={!!busy}>
            Cancel
          </Button>
        </div>
      </div>

      <aside className="space-y-4">
        <Card size="sm">
          <CardContent className="space-y-3">
            <div className="flex items-center gap-3">
              <AppIcon id={t.id} />
              <div className="min-w-0">
                <div className="font-medium">{t.name}</div>
                <Badge variant="secondary" className="mt-0.5 text-[10px]">
                  {t.category}
                </Badge>
              </div>
            </div>
            <p className="text-[13px] text-muted-foreground">{t.tagline}</p>
            <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
              <span className="font-mono text-muted-foreground">{t.image}</span>
              <Button type="button" variant="link" size="xs" className="h-auto px-0" onClick={() => api.openURL(t.website)}>
                Website <ExternalLink />
              </Button>
            </div>
          </CardContent>
        </Card>
        <Card size="sm">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Info className="size-4 text-muted-foreground" /> After installing
            </CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="space-y-2 text-[13px] text-muted-foreground">
              {(t.notes ?? []).map((n) => (
                <li key={n} className="flex gap-2">
                  <span className="mt-2 size-1 shrink-0 rounded-full bg-muted-foreground/60" />
                  <span>{n}</span>
                </li>
              ))}
              <li className="flex gap-2">
                <span className="mt-2 size-1 shrink-0 rounded-full bg-muted-foreground/60" />
                <span>Data lives in Docker volumes, kept across redeploys and when you stop the app.</span>
              </li>
            </ul>
          </CardContent>
        </Card>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="w-full justify-between text-muted-foreground"
          onClick={() => setShowCompose(!showCompose)}
        >
          Compose file
          <ChevronDown className={showCompose ? "rotate-180 transition-transform" : "transition-transform"} />
        </Button>
        {showCompose && (
          <pre className="selectable max-h-80 overflow-auto rounded-lg border border-border bg-background/60 p-3 font-mono text-[11px] leading-relaxed">
            {t.compose}
          </pre>
        )}
      </aside>
    </form>
  );
}
