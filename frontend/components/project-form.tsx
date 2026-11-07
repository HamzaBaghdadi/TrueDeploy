"use client";

import { ArrowRight, ClipboardPaste, Eye, EyeOff, GitBranch, Loader2, Plus, Save, ScanSearch, X } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";
import {
  api,
  type DatabaseView,
  type DBLink,
  type DetectResult,
  type EnvGroupView,
  type Preset,
  type ProjectDetail,
  type ProjectView,
  type Server,
  type Suggestion,
} from "@/lib/api";
import { DetectPanel } from "@/components/detect-panel";
import { OWN_DOCKERFILE, PresetPicker } from "@/components/preset-picker";
import { Switch } from "@/components/ui/switch";
import { GitRepoPicker } from "@/components/git-repo-picker";
import { Notice } from "@/components/notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

type EnvRow = { key: string; value: string };

/** The lowercase host of a domain or URL ("https://WWW.Shop.io/api" -> "www.shop.io"). */
function hostOf(d: string) {
  return d
    .trim()
    .replace(/^[a-z]+:\/\//i, "")
    .split(/[/?#]/)[0]
    .toLowerCase()
    .replace(/\.$/, "");
}

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

// Variables TrueDeploy sets itself for one-click apps (from port and domains).
export const MANAGED_APP_VARS = ["PORT", "BIND", "PUBLIC_URL", "PUBLIC_HOST", "HTTPS"];

const RUNTIMES = [
  ["compose", "Docker Compose", "Uses compose.yaml / docker-compose.yml from the repo. Best for apps with a database or several services."],
  ["dockerfile", "Dockerfile", "One container: your repo's Dockerfile, or one TrueDeploy generates for your framework (React, Angular, Next.js, NestJS, Django…)."],
] as const;

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
  const isApp = !!initial?.compose;
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
    Object.entries(initial?.env ?? {})
      .filter(([key]) => !isApp || !MANAGED_APP_VARS.includes(key))
      .map(([key, value]) => ({ key, value: value ?? "" })),
  );
  const [picking, setPicking] = useState(false);
  const [domains, setDomains] = useState<string[]>(initial?.domains ?? []);
  const [domainDraft, setDomainDraft] = useState("");
  const [redirects, setRedirects] = useState<string[]>(initial?.redirects ?? []);
  const [redirectDraft, setRedirectDraft] = useState("");
  const domainHosts = [...domains, domainDraft].map(hostOf).filter(Boolean);
  // Redirects go to the first domain's host.
  const primaryHost = domainHosts[0] ?? "";
  const wwwSuggestion = primaryHost.startsWith("www.") ? primaryHost.slice(4) : "www." + primaryHost;
  const [appPort, setAppPort] = useState(initial?.appPort ? String(initial.appPort) : "");
  const [links, setLinks] = useState<DBLink[]>(initial?.links ?? []);
  const [dbs, setDbs] = useState<DatabaseView[] | null>(null);
  const [stripPath, setStripPath] = useState(!!initial?.stripPath);
  const [healthOn, setHealthOn] = useState(!initial?.noHealthCheck);
  const [healthPath, setHealthPath] = useState(initial?.healthPath ?? "");
  const [healthTimeout, setHealthTimeout] = useState(initial?.healthTimeout ? String(initial.healthTimeout) : "");
  const [autoRollback, setAutoRollback] = useState(!!initial?.autoRollback);
  const [memoryMb, setMemoryMb] = useState(initial?.memoryMb ? String(initial.memoryMb) : "");
  const [cpus, setCpus] = useState(initial?.cpus ? String(initial.cpus) : "");
  // The port the health check requests (as the server sees it).
  const healthCheckPort = isApp ? Number(appPort) || 0 : runtime === "dockerfile" ? Number(hostPort) || 0 : Number(appPort) || 0;
  const [folder, setFolder] = useState(initial?.folder ?? "");
  const [preset, setPreset] = useState(initial?.preset || OWN_DOCKERFILE);
  const [presetOptions, setPresetOptions] = useState<Record<string, string>>(
    Object.fromEntries(Object.entries(initial?.presetOptions ?? {}).map(([k, v]) => [k, v ?? ""])),
  );
  const [presetList, setPresetList] = useState<Preset[]>([]);
  const [envGroups, setEnvGroups] = useState<string[]>(initial?.envGroups ?? []);
  const [groupList, setGroupList] = useState<EnvGroupView[] | null>(null);
  useEffect(() => {
    api.listDatabases().then(setDbs, () => setDbs([]));
    api.presets().then(setPresetList, () => {});
    api.envGroups().then(setGroupList, () => setGroupList([]));
  }, []);
  const chosenPreset = presetList.find((p) => p.id === preset);
  const staticPreset = chosenPreset?.kind === "Static site";
  const hasPathRoute = [...domains, domainDraft].some((d) => /^[^/\s]+\/\S/.test(d.trim().replace(/^https?:\/\//, "")));

  // "Detect framework": suggestions for this repository and branch.
  const [detecting, setDetecting] = useState<{ busy: boolean; result?: DetectResult; error?: string }>({ busy: false });
  const [applied, setApplied] = useState<Suggestion | null>(null);
  async function runDetect(url = repoUrl, br = branch, quiet = false) {
    setDetecting({ busy: true });
    setApplied(null);
    try {
      const result = await api.detectFramework({ serverId, repoUrl: url.trim(), branch: br.trim(), name });
      setDetecting({ busy: false, result });
      // One clear answer: apply it right away (it can still be changed).
      if (result.suggestions?.length === 1) applySuggestion(result.suggestions[0]);
    } catch (e) {
      setDetecting(quiet ? { busy: false } : { busy: false, error: (e as Error).message });
    }
  }
  function applySuggestion(s: Suggestion) {
    setApplied(s);
    if (s.kind === "compose") {
      setRuntime("compose");
      setPreset(OWN_DOCKERFILE);
      setFolder("");
      return;
    }
    setRuntime("dockerfile");
    setFolder(s.folder ?? "");
    if (s.kind === "dockerfile") {
      setPreset(OWN_DOCKERFILE);
      setPresetOptions({});
      return;
    }
    const p = presetList.find((x) => x.id === s.preset);
    setPreset(s.preset ?? OWN_DOCKERFILE);
    setPresetOptions(Object.fromEntries(Object.entries(s.options ?? {}).map(([k, v]) => [k, v ?? ""])));
    if (p && p.kind !== "Static site") setContainerPort(String(p.port));
  }
  const [pasting, setPasting] = useState(false);
  const [paste, setPaste] = useState("");
  const [revealed, setRevealed] = useState<Set<number>>(new Set());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    // A typo like "1.2.3" would otherwise be sent as 0 = no limit.
    if ((memoryMb && !(Number(memoryMb) >= 32)) || (cpus && !(Number(cpus) >= 0.1 && Number(cpus) <= 256))) {
      setError("Check the resource limits: memory needs at least 32 MB, CPU between 0.1 and 256.");
      return;
    }
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
        domains: [...domains, ...(domainDraft.trim() ? [domainDraft] : [])],
        redirects: [...redirects, ...(redirectDraft.trim() ? [hostOf(redirectDraft)] : [])],
        appPort: Number(appPort) || 0,
        // Only links to databases on the chosen server are kept.
        links: links.filter((l) => dbs?.some((d) => d.id === l.databaseId && d.serverId === serverId)),
        containerPort: staticPreset ? 80 : Number(containerPort) || 0,
        stripPath,
        envGroups,
        noHealthCheck: !healthOn,
        healthPath,
        healthTimeout: Number(healthTimeout) || 0,
        autoRollback: !isApp && autoRollback,
        memoryMb: Math.round(Number(memoryMb)) || 0,
        cpus: Number(cpus) || 0,
        folder,
        preset: preset === OWN_DOCKERFILE ? "" : preset,
        presetOptions,
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

  function addDomain() {
    // Keep a path (example.com/api); lowercase only the host.
    const raw = domainDraft.trim().replace(/^https?:\/\//i, "").replace(/[?#].*$/, "").replace(/\/+$/, "");
    const slash = raw.indexOf("/");
    const host = (slash < 0 ? raw : raw.slice(0, slash)).toLowerCase().replace(/\.$/, "");
    const d = slash < 0 ? host : host + raw.slice(slash);
    if (d && !domains.includes(d)) setDomains([...domains, d]);
    setDomainDraft("");
  }

  function addRedirect(raw: string) {
    // Redirects are whole hosts: drop the scheme and any path.
    const h = hostOf(raw);
    if (h && !redirects.includes(h) && !domainHosts.includes(h)) setRedirects([...redirects, h]);
    setRedirectDraft("");
  }

  const dir = slug(name);

  if (servers.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
        Add a server first. Projects are deployed to one of your servers.
      </div>
    );
  }

  return (
    <form onSubmit={save} className="space-y-6">
      <Card>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="project-name">Project name</Label>
            <Input id="project-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="my-api" required autoFocus />
            <p className="font-mono text-[11px] text-muted-foreground">{dir ? `~/apps/${dir}` : " "}</p>
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
          {!isApp && (
          <div className="grid gap-4 sm:col-span-2 sm:grid-cols-[1fr_180px]">
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <Label htmlFor="repo-url">Git repository</Label>
                {!picking && (
                  <Button type="button" variant="link" size="xs" onClick={() => setPicking(true)} className="h-auto px-0">
                    <GitBranch className="size-3.5" /> Choose or create a repository
                  </Button>
                )}
              </div>
              <Input
                id="repo-url"
                className="font-mono"
                value={repoUrl}
                onChange={(e) => setRepoUrl(e.target.value)}
                placeholder="git@github.com:you/my-api.git"
                spellCheck={false}
                required
              />
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="text-[11px] text-muted-foreground">
                  Private repos: use the SSH URL. You&apos;ll add the server&apos;s deploy key after saving.
                </p>
                <Button type="button" variant="outline" size="xs" disabled={!repoUrl.trim() || detecting.busy} onClick={() => void runDetect()}>
                  {detecting.busy ? <Loader2 className="animate-spin" /> : <ScanSearch />}
                  {detecting.busy ? "Detecting…" : "Detect framework"}
                </Button>
              </div>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="branch">Branch</Label>
              <Input id="branch" className="font-mono" value={branch} onChange={(e) => setBranch(e.target.value)} spellCheck={false} />
            </div>
          </div>
          )}
          {!isApp && picking && (
            <div className="-mt-2 sm:col-span-2">
              <GitRepoPicker
                suggestedName={slug(name)}
                onClose={() => setPicking(false)}
                onPicked={(r) => {
                  setRepoUrl(r.sshUrl);
                  setBranch(r.defaultBranch || "main");
                  if (!name.trim()) setName(r.fullName.split("/").pop() ?? "");
                  setPicking(false);
                  // Quiet: a just-created (empty) repo has nothing to detect yet.
                  void runDetect(r.sshUrl, r.defaultBranch || "main", true);
                }}
              />
            </div>
          )}
          {!isApp && (detecting.result || detecting.error) && (
            <div className="sm:col-span-2">
              {detecting.error ? (
                <Notice tone="error">{detecting.error}</Notice>
              ) : (
                detecting.result && <DetectPanel result={detecting.result} presets={presetList} applied={applied} onUse={applySuggestion} />
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {isApp ? (
        <Card>
          <CardHeader>
            <CardTitle>How to run it</CardTitle>
            <CardDescription>
              TrueDeploy manages this app&apos;s compose file. With a domain the app listens only on 127.0.0.1; without one it&apos;s
              published on this port.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="max-w-40 space-y-1.5">
              <Label htmlFor="app-port">Port on the server</Label>
              <Input id="app-port" value={appPort} onChange={(e) => setAppPort(e.target.value)} inputMode="numeric" required />
            </div>
          </CardContent>
        </Card>
      ) : (
      <Card>
        <CardHeader>
          <CardTitle>How to run it</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <RadioGroup
            value={runtime}
            onValueChange={(v) => setRuntime(v as "compose" | "dockerfile")}
            className="grid gap-3 sm:grid-cols-2"
          >
            {RUNTIMES.map(([id, title, body]) => (
              <Label
                key={id}
                htmlFor={`runtime-${id}`}
                className={cn(
                  "flex cursor-pointer items-start gap-3 rounded-lg border p-4 font-normal transition-colors",
                  runtime === id ? "border-primary bg-primary/5" : "border-border hover:border-primary/60",
                )}
              >
                <RadioGroupItem id={`runtime-${id}`} value={id} className="mt-0.5" />
                <span>
                  <span className="block text-sm font-medium">{title}</span>
                  <span className="mt-1 block text-[13px] leading-snug text-muted-foreground">{body}</span>
                </span>
              </Label>
            ))}
          </RadioGroup>
          {runtime === "compose" ? (
            <div className="max-w-sm space-y-1.5">
              <Label htmlFor="compose-file">Compose file (optional)</Label>
              <Input
                id="compose-file"
                className="font-mono"
                value={composeFile}
                onChange={(e) => setComposeFile(e.target.value)}
                placeholder="auto-detect"
                spellCheck={false}
              />
            </div>
          ) : (
            <div className="space-y-4">
              <PresetPicker
                presets={presetList}
                value={preset}
                options={presetOptions}
                port={Number(containerPort) || 0}
                onChange={(id, opts) => {
                  if (id !== preset) {
                    const next = presetList.find((p) => p.id === id);
                    if (next && next.kind !== "Static site") setContainerPort(String(next.port));
                  }
                  setPreset(id);
                  setPresetOptions(opts);
                }}
              />
              <div className="grid max-w-xl gap-3 sm:grid-cols-3">
                <div className="space-y-1.5">
                  <Label htmlFor="folder">App folder</Label>
                  <Input id="folder" className="font-mono" value={folder} onChange={(e) => setFolder(e.target.value)} placeholder="(repo root)" spellCheck={false} />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="host-port">Port on the server</Label>
                  <Input id="host-port" value={hostPort} onChange={(e) => setHostPort(e.target.value)} inputMode="numeric" />
                </div>
                {!staticPreset && (
                  <div className="space-y-1.5">
                    <Label htmlFor="container-port">Container port</Label>
                    <Input id="container-port" value={containerPort} onChange={(e) => setContainerPort(e.target.value)} inputMode="numeric" />
                  </div>
                )}
              </div>
              <p className="text-[11px] text-muted-foreground">
                App folder: for monorepos, the folder with the app (e.g. <span className="font-mono">frontend</span>). With a domain, the
                port is only reachable through it (bound to 127.0.0.1).
              </p>
            </div>
          )}
        </CardContent>
      </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Domains</CardTitle>
          <CardDescription>
            Served over HTTPS with free automatic certificates. Point each domain&apos;s DNS A record at the server first. Add a path to
            share a domain between projects: e.g. the frontend on <span className="font-mono">shop.com</span> and the backend on{" "}
            <span className="font-mono">shop.com/api</span>.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex flex-wrap items-center gap-2 rounded-lg border border-input bg-input/30 p-1.5 focus-within:border-ring">
            {domains.map((d) => (
              <Badge key={d} variant="secondary" className="h-6 gap-1 pr-0.5 font-mono">
                {d}
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  aria-label={`Remove ${d}`}
                  className="size-5 text-muted-foreground hover:text-destructive"
                  onClick={() => setDomains(domains.filter((x) => x !== d))}
                >
                  <X />
                </Button>
              </Badge>
            ))}
            <Input
              className="h-7 min-w-48 flex-1 border-0 bg-transparent px-1 font-mono shadow-none focus-visible:ring-0 dark:bg-transparent"
              value={domainDraft}
              onChange={(e) => setDomainDraft(e.target.value)}
              onKeyDown={(e) => {
                if ((e.key === "Enter" || e.key === "," || e.key === " ") && domainDraft.trim()) {
                  e.preventDefault();
                  addDomain();
                } else if (e.key === "Backspace" && !domainDraft && domains.length) {
                  setDomains(domains.slice(0, -1));
                }
              }}
              placeholder={domains.length ? "Add another…" : "example.com or example.com/api"}
              spellCheck={false}
            />
          </div>
          {hasPathRoute && (
            <div className="flex items-start justify-between gap-4 rounded-lg border border-border bg-background/40 px-3 py-2.5">
              <Label htmlFor="strip-path" className="flex-col items-start gap-0.5 font-normal">
                <span className="text-sm font-medium">Remove the path before forwarding</span>
                <span className="text-[12px] text-muted-foreground">
                  {stripPath ? (
                    <>
                      <span className="font-mono">/api/users</span> reaches the app as <span className="font-mono">/users</span>.
                    </>
                  ) : (
                    <>
                      The app receives <span className="font-mono">/api/users</span> as is (e.g. NestJS with setGlobalPrefix(&apos;api&apos;)).
                    </>
                  )}
                </span>
              </Label>
              <Switch id="strip-path" checked={stripPath} onCheckedChange={setStripPath} />
            </div>
          )}
          {!isApp && (domains.length > 0 || domainDraft) &&
            (runtime === "compose" ? (
              <div className="max-w-sm space-y-1.5">
                <Label htmlFor="app-port">App port on the server</Label>
                <Input id="app-port" value={appPort} onChange={(e) => setAppPort(e.target.value)} inputMode="numeric" placeholder="e.g. 3000" />
                <p className="text-[11px] text-muted-foreground">
                  The port your compose file publishes, e.g. <span className="font-mono">&quot;127.0.0.1:3000:3000&quot;</span>. Binding to
                  127.0.0.1 keeps it reachable only through the domain.
                </p>
              </div>
            ) : (
              <p className="text-[12px] text-muted-foreground">
                Forwards to the public port <span className="font-mono">{hostPort || "…"}</span>.
              </p>
            ))}
          {primaryHost && (
            <div className="space-y-2 border-t border-border pt-3">
              <div>
                <Label htmlFor="redirect-draft" className="text-sm font-medium">
                  Redirect other domains here
                </Label>
                <p className="text-[12px] text-muted-foreground">
                  Visitors to these are sent to <span className="font-mono">https://{primaryHost}</span>, on the same page they asked for. Point
                  their DNS at the server too; they get certificates as well.
                </p>
              </div>
              <div className="flex flex-wrap items-center gap-2 rounded-lg border border-input bg-input/30 p-1.5 focus-within:border-ring">
                {redirects.map((r) => (
                  <Badge key={r} variant="secondary" className="h-6 gap-1 pr-0.5 font-mono">
                    {r}
                    <ArrowRight className="size-3 text-muted-foreground" />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-xs"
                      aria-label={`Remove the redirect from ${r}`}
                      className="size-5 text-muted-foreground hover:text-destructive"
                      onClick={() => setRedirects(redirects.filter((x) => x !== r))}
                    >
                      <X />
                    </Button>
                  </Badge>
                ))}
                <Input
                  id="redirect-draft"
                  className="h-7 min-w-48 flex-1 border-0 bg-transparent px-1 font-mono shadow-none focus-visible:ring-0 dark:bg-transparent"
                  value={redirectDraft}
                  onChange={(e) => setRedirectDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if ((e.key === "Enter" || e.key === "," || e.key === " ") && redirectDraft.trim()) {
                      e.preventDefault();
                      addRedirect(redirectDraft);
                    } else if (e.key === "Backspace" && !redirectDraft && redirects.length) {
                      setRedirects(redirects.slice(0, -1));
                    }
                  }}
                  placeholder={redirects.length ? "Add another…" : `e.g. ${wwwSuggestion}`}
                  spellCheck={false}
                />
              </div>
              {!redirects.includes(wwwSuggestion) && !domainHosts.includes(wwwSuggestion) && (
                <Button type="button" variant="ghost" size="xs" className="text-primary" onClick={() => addRedirect(wwwSuggestion)}>
                  <Plus /> Redirect <span className="font-mono">{wwwSuggestion}</span>
                </Button>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>After each deploy</CardTitle>
          <CardDescription>Catch a broken release before your visitors do.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-start justify-between gap-4">
            <Label htmlFor="health-on" className="flex-col items-start gap-0.5 font-normal">
              <span className="text-sm font-medium">Check that the app is healthy</span>
              <span className="text-[12px] text-muted-foreground">
                {healthCheckPort ? (
                  <>
                    Requests <span className="font-mono">http://127.0.0.1:{healthCheckPort}{healthPath || "/"}</span> on the server until it
                    answers (any status below 500). The deploy fails if it doesn&apos;t, or if a container keeps restarting.
                  </>
                ) : (
                  "Without an app port, it checks that no container keeps restarting. Add a domain and app port for an HTTP check."
                )}
              </span>
            </Label>
            <Switch id="health-on" checked={healthOn} onCheckedChange={setHealthOn} />
          </div>
          {healthOn && (
            <div className="grid max-w-md grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label htmlFor="health-path" className="text-xs text-muted-foreground">
                  Path
                </Label>
                <Input id="health-path" className="font-mono" value={healthPath} onChange={(e) => setHealthPath(e.target.value)} placeholder="/" spellCheck={false} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="health-timeout" className="text-xs text-muted-foreground">
                  Wait up to (seconds)
                </Label>
                <Input id="health-timeout" value={healthTimeout} onChange={(e) => setHealthTimeout(e.target.value)} inputMode="numeric" placeholder={isApp ? "180" : "60"} />
              </div>
            </div>
          )}
          {healthOn && !isApp && (
            <div className="flex items-start justify-between gap-4 border-t border-border pt-4">
              <Label htmlFor="auto-rollback" className="flex-col items-start gap-0.5 font-normal">
                <span className="text-sm font-medium">Roll back automatically</span>
                <span className="text-[12px] text-muted-foreground">
                  When the new version fails its check, the server redeploys the last commit that passed it, so the site comes back on its
                  own. Works for auto-deploys from your Git host too. Database migrations aren&apos;t undone.
                </span>
              </Label>
              <Switch id="auto-rollback" checked={autoRollback} onCheckedChange={setAutoRollback} />
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Resources</CardTitle>
          <CardDescription>
            Cap how much of the server {runtime === "compose" || isApp ? "each of this project's containers" : "this project"} can use, so one app
            can&apos;t slow down the others. Leave empty for no limit.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-5 sm:grid-cols-2">
          <LimitField
            id="memory-limit"
            label="Memory"
            unit="MB"
            value={memoryMb}
            onChange={setMemoryMb}
            presets={[
              ["256", "256 MB"],
              ["512", "512 MB"],
              ["1024", "1 GB"],
              ["2048", "2 GB"],
            ]}
            hint={
              memoryMb && Number(memoryMb) < 32
                ? "At least 32 MB."
                : "An app that goes over is stopped (and restarted if it has a restart policy). No swap, so a leak can't slow the server."
            }
            invalid={!!memoryMb && (!(Number(memoryMb) >= 32) || !Number.isFinite(Number(memoryMb)))}
          />
          <LimitField
            id="cpu-limit"
            label="CPU"
            unit="CPUs"
            value={cpus}
            onChange={setCpus}
            presets={[
              ["0.5", "½"],
              ["1", "1"],
              ["2", "2"],
            ]}
            hint={cpus && !(Number(cpus) >= 0.1) ? "At least 0.1 CPU." : "0.5 is half of one core. Over the limit the app slows down; it isn't stopped."}
            invalid={!!cpus && !(Number(cpus) >= 0.1 && Number(cpus) <= 256)}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Databases</CardTitle>
          <CardDescription>Linked databases are added to the environment on deploy, and the app joins their private network.</CardDescription>
        </CardHeader>
        <CardContent>
          {(() => {
            const here = (dbs ?? []).filter((d) => d.serverId === serverId);
            if (dbs === null)
              return (
                <p className="flex items-center gap-2 text-[13px] text-muted-foreground">
                  <Loader2 className="size-4 animate-spin" /> Loading…
                </p>
              );
            if (here.length === 0)
              return (
                <p className="flex flex-wrap items-center gap-1 text-[13px] text-muted-foreground">
                  No databases on this server yet.
                  <Button asChild variant="link" size="xs" className="h-auto px-0">
                    <Link href="/databases/">
                      Create one <ArrowRight />
                    </Link>
                  </Button>
                </p>
              );
            return (
              <ul className="space-y-2">
                {here.map((d) => {
                  const link = links.find((l) => l.databaseId === d.id);
                  return (
                    <li key={d.id} className="flex flex-wrap items-center gap-3">
                      <div className="flex min-w-56 flex-1 items-center gap-2.5">
                        <Checkbox
                          id={`db-${d.id}`}
                          checked={!!link}
                          onCheckedChange={(v) =>
                            setLinks(
                              v === true
                                ? [...links, { databaseId: d.id, envVar: d.envVar }]
                                : links.filter((l) => l.databaseId !== d.id),
                            )
                          }
                        />
                        <Label htmlFor={`db-${d.id}`} className="gap-2.5">
                          <span className="font-medium">{d.name}</span>
                          <span className="text-xs font-normal text-muted-foreground">
                            {d.engineName} {d.version}
                          </span>
                        </Label>
                      </div>
                      {link && (
                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                          as
                          <Input
                            className="h-7 w-48 font-mono text-xs"
                            value={link.envVar}
                            spellCheck={false}
                            aria-label={`Environment variable for ${d.name}`}
                            onChange={(e) =>
                              setLinks(links.map((l) => (l.databaseId === d.id ? { ...l, envVar: e.target.value.trim() } : l)))
                            }
                          />
                        </div>
                      )}
                    </li>
                  );
                })}
              </ul>
            );
          })()}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Shared variables</CardTitle>
          <CardDescription>Variable groups this project uses. Its own variables (below) override group values with the same name.</CardDescription>
          <CardAction>
            <Button asChild variant="link" size="xs" className="h-auto px-0">
              <Link href="/variables/">
                Manage groups <ArrowRight />
              </Link>
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          {groupList === null ? (
            <p className="flex items-center gap-2 text-[13px] text-muted-foreground">
              <Loader2 className="size-4 animate-spin" /> Loading…
            </p>
          ) : groupList.length === 0 ? (
            <p className="text-[13px] text-muted-foreground">No variable groups yet. Create one on the Variables page to share values between projects.</p>
          ) : (
            <ul className="space-y-2">
              {groupList.map((g) => {
                const keys = Object.keys(g.vars ?? {}).sort();
                const overridden = keys.filter((k) => env.some((r) => r.key.trim() === k));
                return (
                  <li key={g.id} className="flex items-start gap-2.5">
                    <Checkbox
                      id={`grp-${g.id}`}
                      className="mt-0.5"
                      checked={envGroups.includes(g.id)}
                      onCheckedChange={(v) => setEnvGroups(v === true ? [...envGroups, g.id] : envGroups.filter((x) => x !== g.id))}
                    />
                    <Label htmlFor={`grp-${g.id}`} className="flex-col items-start gap-0.5 font-normal">
                      <span className="text-sm font-medium">{g.name}</span>
                      <span className="font-mono text-[11px] text-muted-foreground">{keys.join(" · ") || "no variables"}</span>
                      {envGroups.includes(g.id) && overridden.length > 0 && (
                        <span className="text-[11px] text-warning">Overridden by this project: {overridden.join(", ")}</span>
                      )}
                    </Label>
                  </li>
                );
              })}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Environment variables</CardTitle>
          <CardDescription>Written to .env on the server. Stored encrypted on this computer.</CardDescription>
          <CardAction className="flex gap-1">
            <Button type="button" variant="ghost" size="sm" onClick={() => setPasting(!pasting)}>
              {pasting ? <X /> : <ClipboardPaste />}
              {pasting ? "Close" : "Paste .env"}
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => setEnv([...env, { key: "", value: "" }])}>
              <Plus /> Add
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="space-y-3">
          {pasting && (
            <div className="space-y-2">
              <Textarea
                className="h-32 font-mono text-xs"
                value={paste}
                onChange={(e) => setPaste(e.target.value)}
                placeholder={"DATABASE_URL=postgres://...\nSECRET_KEY=..."}
                spellCheck={false}
              />
              <Button type="button" variant="secondary" size="sm" onClick={applyPaste}>
                <Plus /> Add {parseDotEnv(paste).length || ""} variables
              </Button>
            </div>
          )}
          {env.length === 0 ? (
            <p className="text-[13px] text-muted-foreground">No variables.</p>
          ) : (
            <div className="space-y-2">
              {env.map((row, i) => (
                <div key={i} className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)_auto_auto] gap-2">
                  <Input
                    className="font-mono text-xs"
                    value={row.key}
                    placeholder="KEY"
                    spellCheck={false}
                    aria-label="Variable name"
                    onChange={(e) => setEnv(env.map((r, j) => (j === i ? { ...r, key: e.target.value } : r)))}
                  />
                  <Input
                    className="font-mono text-xs"
                    type={revealed.has(i) ? "text" : "password"}
                    value={row.value}
                    placeholder="value"
                    spellCheck={false}
                    aria-label="Variable value"
                    onChange={(e) => setEnv(env.map((r, j) => (j === i ? { ...r, value: e.target.value } : r)))}
                  />
                  <Tooltip>
                    <TooltipTrigger asChild>
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
                    </TooltipTrigger>
                    <TooltipContent>{revealed.has(i) ? "Hide value" : "Show value"}</TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        aria-label="Remove variable"
                        className="text-muted-foreground hover:text-destructive"
                        onClick={() => setEnv(env.filter((_, j) => j !== i))}
                      >
                        <X />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>Remove</TooltipContent>
                  </Tooltip>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {error && <Notice tone="error">{error}</Notice>}
      <div className="flex gap-2">
        <Button type="submit" disabled={busy}>
          {busy ? <Loader2 className="animate-spin" /> : initial ? <Save /> : <Plus />}
          {busy ? "Saving…" : initial ? "Save changes" : "Create project"}
        </Button>
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

/** A number with a unit and quick picks; empty means no limit. */
function LimitField({
  id,
  label,
  unit,
  value,
  onChange,
  presets,
  hint,
  invalid,
}: {
  id: string;
  label: string;
  unit: string;
  value: string;
  onChange: (v: string) => void;
  presets: [string, string][];
  hint: string;
  invalid: boolean;
}) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      <div className="flex items-center gap-2">
        <div className="relative w-32">
          <Input
            id={id}
            value={value}
            onChange={(e) => onChange(e.target.value.replace(/[^0-9.]/g, ""))}
            inputMode="decimal"
            placeholder="No limit"
            aria-invalid={invalid}
            className="pr-12"
          />
          <span className="pointer-events-none absolute top-1/2 right-2.5 -translate-y-1/2 text-xs text-muted-foreground">{unit}</span>
        </div>
        <div className="flex flex-wrap gap-1">
          {presets.map(([v, l]) => (
            <Button key={v} type="button" variant={value === v ? "secondary" : "ghost"} size="xs" onClick={() => onChange(value === v ? "" : v)}>
              {l}
            </Button>
          ))}
        </div>
      </div>
      <p className={cn("text-[12px]", invalid ? "text-destructive" : "text-muted-foreground")}>{hint}</p>
    </div>
  );
}
