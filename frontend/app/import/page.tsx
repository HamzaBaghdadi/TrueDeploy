"use client";

import {
  ArrowRight,
  Boxes,
  CircleCheck,
  Container,
  Database,
  FolderGit2,
  Globe,
  Loader2,
  ScanSearch,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";
import { api, type ImportCompose, type ImportDatabase, type ImportScan, type Server } from "@/lib/api";
import { CopyButton } from "@/components/copy-button";
import { DeepLink } from "@/components/deep-link";
import { Notice } from "@/components/notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

// Go sends empty lists as null; the page works with arrays.
type Scan = ImportScan & {
  compose: ImportCompose[];
  databases: ImportDatabase[];
  containers: NonNullable<ImportScan["containers"]>;
  sites: NonNullable<ImportScan["sites"]>;
};

export default function ImportPage() {
  const [servers, setServers] = useState<Server[]>([]);
  const [serverId, setServerId] = useState("");
  const [scan, setScan] = useState<Scan | null>(null);
  const [scanning, setScanning] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.listServers().then(
      (s) => {
        setServers(s);
        setServerId((cur) => cur || s[0]?.id || "");
      },
      () => {},
    );
  }, []);

  async function run(id = serverId) {
    if (!id) return;
    setScanning(true);
    setError(null);
    setScan(null);
    try {
      const s = await api.scanServer(id);
      setScan({ ...s, compose: s.compose ?? [], databases: s.databases ?? [], containers: s.containers ?? [], sites: s.sites ?? [] });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setScanning(false);
    }
  }

  const server = servers.find((s) => s.id === serverId);
  const nothing =
    scan && scan.compose.length === 0 && scan.databases.length === 0 && scan.containers.length === 0 && scan.sites.length === 0;

  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      <DeepLink
        onLink={(p) => {
          const id = p.get("server");
          if (id) {
            setServerId(id);
            void run(id);
          }
        }}
      />
      <header className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight">Import existing setup</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Bring apps and databases you set up before TrueDeploy under its management. Scanning only reads the server.
        </p>
      </header>

      <Card className="mb-6">
        <CardContent className="flex flex-wrap items-end gap-3">
          <div className="min-w-64 flex-1 space-y-1.5">
            <Label>Server</Label>
            <Select value={serverId} onValueChange={setServerId}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Add a server first" />
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
          <Button onClick={() => void run()} disabled={!serverId || scanning}>
            {scanning ? <Loader2 className="animate-spin" /> : <ScanSearch />}
            {scanning ? "Scanning…" : scan ? "Scan again" : "Scan server"}
          </Button>
        </CardContent>
      </Card>

      {error && <Notice tone="error" className="mb-4">{error}</Notice>}

      {scan && !scan.docker && (
        <Notice tone="warning" className="mb-4">
          Docker isn&apos;t reachable as this user, so containers couldn&apos;t be listed. Run the Docker steps in server Set up, or log in as
          a user in the docker group.
        </Notice>
      )}

      {scan?.webPorts && <WebPortsNotice holder={scan.webPorts} />}

      {nothing && (
        <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
          Nothing to import on {server?.name}: no Compose projects, database containers or nginx sites were found.
        </div>
      )}

      {scan && scan.compose.length > 0 && (
        <Section icon={FolderGit2} title="Docker Compose projects" body="Adopted in place: TrueDeploy links ~/apps/<name> to the existing folder and keeps the Docker project name, so containers, volumes and data stay as they are.">
          {scan.compose.map((c) => (
            <ComposeRow key={c.name} serverId={serverId} c={c} />
          ))}
        </Section>
      )}

      {scan && scan.databases.length > 0 && (
        <Section icon={Database} title="Databases in containers" body="Copied into a new TrueDeploy database (backups, private network, linking). The old container keeps running until you remove it.">
          {scan.databases.map((d) => (
            <DatabaseRow key={d.container} serverId={serverId} d={d} />
          ))}
        </Section>
      )}

      {scan && scan.containers.length > 0 && (
        <Section icon={Container} title="Other containers" body="Started with docker run, so TrueDeploy can't tell how to rebuild them. Create a project from each one's repository (with a framework preset if it has no Dockerfile), deploy it on a free port, then remove the old container.">
          <ul className="divide-y divide-border rounded-lg border border-border text-[13px]">
            {scan.containers.map((c) => (
              <li key={c.name} className="flex flex-wrap items-center gap-3 px-3 py-2">
                <span className="font-mono">{c.name}</span>
                <span className="font-mono text-xs text-muted-foreground">{scan.images?.[c.name]}</span>
                <Badge variant="outline" className="text-[10px]">
                  {c.state}
                </Badge>
                {c.ports?.length ? <span className="text-xs text-muted-foreground">ports {c.ports.join(", ")}</span> : null}
              </li>
            ))}
          </ul>
        </Section>
      )}

      {scan && scan.sites.length > 0 && (
        <Section icon={Globe} title="nginx sites" body="Their domains are suggested for the matching projects above. Once the projects are imported, TrueDeploy's proxy can serve these domains with automatic HTTPS.">
          <ul className="divide-y divide-border rounded-lg border border-border text-[13px]">
            {scan.sites.map((s) => (
              <li key={s.file} className="flex flex-wrap items-center gap-3 px-3 py-2">
                <span className="font-mono">{s.serverNames?.join(", ") || "(no server_name)"}</span>
                <ArrowRight className="size-3.5 text-muted-foreground" />
                <span className="font-mono text-xs text-muted-foreground">{s.ports?.length ? `localhost:${s.ports.join(", ")}` : "static files or other upstream"}</span>
                <span className="ml-auto font-mono text-[11px] text-muted-foreground">{s.file}</span>
              </li>
            ))}
          </ul>
        </Section>
      )}
    </div>
  );
}

function Section({ icon: Icon, title, body, children }: { icon: LucideIcon; title: string; body: string; children: React.ReactNode }) {
  return (
    <Card className="mb-4">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Icon className="size-4 text-muted-foreground" />
          {title}
        </CardTitle>
        <CardDescription>{body}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">{children}</CardContent>
    </Card>
  );
}

function WebPortsNotice({ holder }: { holder: string }) {
  const cmd = "sudo systemctl disable --now nginx";
  return (
    <Notice tone="warning" className="mb-4">
      <div className="space-y-1.5">
        <div>
          Ports 80/443 are used by <b>{holder}</b>. TrueDeploy&apos;s HTTPS proxy needs them to serve domains. Import your projects first,
          check they run, then stop it and click Set up HTTPS on each project:
        </div>
        <div className="flex items-center gap-2 font-mono text-xs text-foreground">
          {cmd} <CopyButton text={cmd} />
        </div>
        <div className="text-xs">Expect a minute or two of downtime per domain while certificates are issued.</div>
      </div>
    </Notice>
  );
}

function ComposeRow({ serverId, c }: { serverId: string; c: ImportCompose }) {
  const [domains, setDomains] = useState((c.suggestedDomains ?? []).join(", "));
  const [port, setPort] = useState(c.suggestedPort ? String(c.suggestedPort) : "");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; text: string; id?: string } | null>(null);
  const blocked = (c.problems?.length ?? 0) > 0;
  const running = (c.containers ?? []).filter((x) => x.state === "running").length;

  async function adopt() {
    setBusy(true);
    setResult(null);
    try {
      const list = domains.split(/[\s,]+/).filter(Boolean);
      const p = await api.adoptCompose({ serverId, name: c.name, branch: "", domains: list, appPort: list.length ? Number(port) || 0 : 0 });
      setResult({ ok: true, text: `${p.name} is now a TrueDeploy project.`, id: p.id });
    } catch (e) {
      setResult({ ok: false, text: (e as Error).message });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="rounded-lg border border-border p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Boxes className="size-4 text-muted-foreground" />
        <span className="font-medium">{c.name}</span>
        <Badge variant="outline" className={running ? "border-success/25 bg-success/10 text-success" : "text-muted-foreground"}>
          {running}/{c.containers?.length ?? 0} running
        </Badge>
        {c.managedAs && (
          <Badge variant="secondary">
            <CircleCheck /> In TrueDeploy as {c.managedAs}
          </Badge>
        )}
      </div>
      <div className="mt-1 space-y-0.5 font-mono text-xs text-muted-foreground">
        <div className="selectable truncate">{c.workDir}</div>
        {c.repoUrl && (
          <div className="selectable truncate">
            {c.repoUrl} · {c.branch || "detached"}
            {c.composeFile ? ` · ${c.composeFile}` : ""}
          </div>
        )}
        {c.ports?.length ? <div>published ports {c.ports.join(", ")}</div> : null}
        {Object.keys(c.env ?? {}).length > 0 && <div>.env with {Object.keys(c.env ?? {}).length} variables (imported encrypted)</div>}
      </div>

      {(c.problems ?? []).map((p) => (
        <Notice key={p} tone="error" className="mt-2">
          {p}
        </Notice>
      ))}
      {(c.warnings ?? []).map((w) => (
        <Notice key={w} tone="warning" className="mt-2">
          {w}
        </Notice>
      ))}

      {!c.managedAs && !blocked && !result?.ok && (
        <div className="mt-3 grid gap-3 sm:grid-cols-[minmax(0,1fr)_140px_auto] sm:items-end">
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">Domains (optional)</Label>
            <Input className="font-mono text-xs" value={domains} onChange={(e) => setDomains(e.target.value)} placeholder="example.com, example.com/api" spellCheck={false} />
          </div>
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">App port</Label>
            <Input className="font-mono text-xs" value={port} onChange={(e) => setPort(e.target.value)} inputMode="numeric" placeholder="e.g. 8080" />
          </div>
          <Button onClick={() => void adopt()} disabled={busy}>
            {busy ? <Loader2 className="animate-spin" /> : <FolderGit2 />}
            {busy ? "Adopting…" : "Adopt project"}
          </Button>
        </div>
      )}
      {result && (
        <Notice tone={result.ok ? "success" : "error"} className="mt-3">
          <span>{result.text}</span>{" "}
          {result.id && (
            <Link href={`/projects/?id=${encodeURIComponent(result.id)}`} className="font-medium underline">
              Open project
            </Link>
          )}
        </Notice>
      )}
    </div>
  );
}

function DatabaseRow({ serverId, d }: { serverId: string; d: ImportDatabase }) {
  const [name, setName] = useState(d.container.replace(/[^a-z0-9-]+/gi, "-").toLowerCase());
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null);
  const needsPassword = !d.hasPassword && d.engine !== "mongo";

  async function copy() {
    setBusy(true);
    setResult(null);
    try {
      const r = await api.importDatabase({ serverId, container: d.container, name, password });
      setResult({ ok: true, text: `${r.database.name} created. ${r.message}` });
    } catch (e) {
      setResult({ ok: false, text: (e as Error).message });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="rounded-lg border border-border p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Database className="size-4 text-muted-foreground" />
        <span className="font-mono font-medium">{d.container}</span>
        <span className="font-mono text-xs text-muted-foreground">{d.image}</span>
        <Badge variant="outline" className="text-[10px]">
          {d.state}
        </Badge>
        <ArrowRight className="size-3.5 text-muted-foreground" />
        <span className="text-xs">
          {d.engine} {d.targetVersion}
        </span>
      </div>
      <div className="mt-1 font-mono text-xs text-muted-foreground">
        database {d.database || "?"} · user {d.user || "?"}
      </div>
      {(d.warnings ?? []).map((w) => (
        <Notice key={w} tone="warning" className="mt-2">
          {w}
        </Notice>
      ))}
      {!result?.ok && (
        <div className="mt-3 grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">New database name</Label>
            <Input className="font-mono text-xs" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          {needsPassword ? (
            <div className="space-y-1">
              <Label className="text-xs text-muted-foreground">Password of {d.user}</Label>
              <Input type="password" className="font-mono text-xs" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="off" />
            </div>
          ) : (
            <p className="text-[12px] text-muted-foreground">Credentials were read from the container.</p>
          )}
          <Button onClick={() => void copy()} disabled={busy || !name.trim() || (needsPassword && !password)}>
            {busy ? <Loader2 className="animate-spin" /> : <Database />}
            {busy ? "Copying…" : "Copy into TrueDeploy"}
          </Button>
        </div>
      )}
      {result && (
        <Notice tone={result.ok ? "success" : "error"} className="mt-3">
          {result.text}
          {result.ok && (
            <>
              {" "}
              Link it to your projects (Edit → Databases), redeploy them, then remove {d.container}.{" "}
              <Link href="/databases/" className="font-medium underline">
                Open databases
              </Link>
            </>
          )}
        </Notice>
      )}
      {!result && (
        <p className="mt-2 flex items-center gap-1.5 text-[11px] text-muted-foreground">
          <TriangleAlert className="size-3" /> Writes made to {d.container} after the copy aren&apos;t carried over: copy during a quiet moment.
        </p>
      )}
    </div>
  );
}
