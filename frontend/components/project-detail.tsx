"use client";

import {
  ArrowRight,
  Box,
  CircleCheck,
  CircleX,
  ExternalLink,
  GitBranchPlus,
  Globe,
  KeyRound,
  Loader2,
  Pencil,
  Power,
  RefreshCw,
  Rocket,
  RotateCw,
  ScrollText,
  ShieldCheck,
  Square,
  SquareTerminal,
  Trash2,
  Undo2,
  X,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  api,
  onDeployEvents,
  type ContainerStatus,
  type AutoDeployRuns,
  type Deployment,
  type DomainCheck,
  type DomainsStatus,
  type ProjectDetail,
  type Server,
  type Template,
} from "@/lib/api";
import { AppIcon } from "@/components/app-icon";
import { BackButton } from "@/components/back-button";
import { useConfirm } from "@/components/confirm-dialog";
import { CopyButton } from "@/components/copy-button";
import { DeployStatus } from "@/components/deploy-status";
import { GitProviderIcon } from "@/components/git-provider-icon";
import { ciInfo } from "@/lib/git";
import { LogConsole, type LogEntry } from "@/components/log-console";
import { Notice } from "@/components/notice";
import { ProjectForm } from "@/components/project-form";
import { ProjectBackups } from "@/components/project-backups";
import { ProjectJobs } from "@/components/project-jobs";
import { ProjectPreviews } from "@/components/project-previews";
import { SiteHistoryCard } from "@/components/history-panels";
import { ProjectAccess } from "@/components/project-access";
import { ProjectLogs } from "@/components/project-logs";
import Link from "next/link";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { duration, isSet, timeAgo } from "@/lib/format";
import { cn } from "@/lib/utils";

export function ProjectDetailView({
  projectId,
  servers,
  onBack,
  onDeleted,
}: {
  projectId: string;
  servers: Server[];
  onBack: () => void;
  onDeleted: () => void;
}) {
  const confirm = useConfirm();
  const [project, setProject] = useState<ProjectDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);

  // Live deploy
  const [deployId, setDeployId] = useState<string | null>(null);
  const deployIdRef = useRef<string | null>(null);
  const [liveLog, setLiveLog] = useState<LogEntry[]>([]);
  const [deployError, setDeployError] = useState<string | null>(null);
  const [notice, setNotice] = useState<{ tone: "ok" | "err"; text: string } | null>(null);

  const reload = useCallback(
    () =>
      api.getProject(projectId).then(
        (p) => {
          setProject(p);
          setError(null);
          // Re-attach to a deploy that is still running (e.g. after navigating away).
          const running = (p.deployments ?? []).find((d) => d.status === "running");
          if (running && !deployIdRef.current) {
            deployIdRef.current = running.id;
            setDeployId(running.id);
          }
        },
        (e: Error) => setError(e.message),
      ),
    [projectId],
  );

  useEffect(() => {
    void reload();
  }, [reload]);

  useEffect(
    () =>
      onDeployEvents({
        log: (e) => {
          if (e.deploymentId !== deployIdRef.current) return;
          setLiveLog((l) => [...l.slice(-4000), { line: e.line, stream: e.stream }]);
        },
        finished: (e) => {
          if (e.deploymentId !== deployIdRef.current) return;
          deployIdRef.current = null;
          setDeployId(null);
          setNotice(
            e.status === "success"
              ? { tone: "ok", text: `Deployed ${e.commit || ""} successfully.` }
              : e.status === "cancelled"
                ? { tone: "err", text: "Deploy cancelled." }
                : { tone: "err", text: e.error || "Deploy failed." },
          );
          void reload();
        },
      }),
    [reload],
  );

  async function deploy(rollbackTo?: Deployment) {
    if (rollbackTo) {
      const ok = await confirm({
        title: `Roll back to ${rollbackTo.commit}?`,
        message: `Redeploys "${rollbackTo.message || rollbackTo.commit}" exactly as it was. ${
          project?.autoDeploy ? "Your next push will deploy the latest code again." : "Deploy normally to return to the latest code."
        }`,
        confirmLabel: "Roll back",
      });
      if (!ok) return;
    }
    setDeployError(null);
    setNotice(null);
    setLiveLog([]);
    try {
      const id = rollbackTo ? await api.rollback(projectId, rollbackTo.id) : await api.deploy(projectId);
      deployIdRef.current = id;
      setDeployId(id);
      void reload();
    } catch (e) {
      setDeployError((e as Error).message);
    }
  }

  async function stop() {
    const ok = await confirm({
      title: `Stop ${project?.name}?`,
      message: "Its containers are stopped and removed. Data volumes are kept, and the next deploy starts it again.",
      confirmLabel: "Stop containers",
      tone: "danger",
    });
    if (!ok) return;
    try {
      setNotice({ tone: "ok", text: await api.stopProject(projectId) });
    } catch (e) {
      setNotice({ tone: "err", text: (e as Error).message });
    }
  }

  async function remove() {
    const ok = await confirm({
      title: `Delete ${project?.name}?`,
      message:
        "TrueDeploy forgets this project, its environment variables and its deploy history. " +
        "Nothing on the server is touched. Stop the containers first if you want them gone.",
      confirmLabel: "Delete project",
      tone: "danger",
    });
    if (!ok) return;
    await api.deleteProject(projectId);
    onDeleted();
  }

  if (error && !project) {
    return (
      <Page onBack={onBack}>
        <Notice tone="error">{error}</Notice>
      </Page>
    );
  }
  if (!project) {
    return (
      <Page onBack={onBack}>
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" /> Loading…
        </p>
      </Page>
    );
  }

  if (editing) {
    return (
      <Page onBack={() => setEditing(false)} backLabel={project.name}>
        <h1 className="mb-6 text-xl font-semibold tracking-tight">Edit {project.name}</h1>
        <ProjectForm
          servers={servers}
          initial={project}
          onCancel={() => setEditing(false)}
          onSaved={() => {
            setEditing(false);
            void reload();
          }}
        />
      </Page>
    );
  }

  const deploying = deployId !== null;
  const history = project.deployments ?? [];

  return (
    <Page onBack={onBack}>
      <header className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-semibold tracking-tight">{project.name}</h1>
            {project.previewOf && (
              <Button asChild variant="outline" size="xs" className="h-6 gap-1 text-[11px] text-muted-foreground">
                <Link href={`/projects/?id=${encodeURIComponent(project.previewOf)}`}>
                  <GitBranchPlus /> Branch preview · open the main project <ArrowRight />
                </Link>
              </Button>
            )}
            {project.last && <DeployStatus status={project.last.status} />}
            {project.maintenance && (
              <Badge variant="outline" className="border-warning/25 bg-warning/5 text-warning">
                Maintenance
              </Badge>
            )}
            {project.authUser && (
              <Badge variant="outline" className="text-muted-foreground">
                <KeyRound /> Password
              </Badge>
            )}
          </div>
          <p className="selectable mt-1 flex items-center gap-1.5 truncate font-mono text-xs text-muted-foreground">
            <span className="truncate">
              {project.compose ? `App · ${project.template}` : `${project.repoUrl} · ${project.branch}`}
            </span>
            <ArrowRight className="size-3.5 shrink-0" />
            <span className="truncate">
              {project.serverName || "unknown server"}:{project.appDir}
            </span>
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => setEditing(true)} disabled={deploying}>
            <Pencil /> Edit
          </Button>
          <Button variant="outline" onClick={stop} disabled={deploying}>
            <Square /> Stop
          </Button>
          <Button variant="outline" onClick={remove} disabled={deploying} className="text-muted-foreground hover:text-destructive">
            <Trash2 /> Delete
          </Button>
          {deploying ? (
            <Button variant="destructive" onClick={() => deployId && api.cancelDeploy(deployId)}>
              <X /> Cancel deploy
            </Button>
          ) : (
            <Button onClick={() => void deploy()}>
              <Rocket /> Deploy
            </Button>
          )}
        </div>
      </header>

      {deployError && (
        <Notice tone="error" className="mb-4">
          {deployError}
        </Notice>
      )}
      {notice && (
        <Notice tone={notice.tone === "ok" ? "success" : "error"} className="mb-4">
          {notice.text}
        </Notice>
      )}

      {(deploying || liveLog.length > 0) && (
        <div className="mb-6">
          <LogConsole entries={liveLog} running={deploying} title={deploying ? "Deploying…" : "Last deploy output"} height="h-96" />
        </div>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        {project.compose ? <AppPanel project={project} /> : <RepoAccess project={project} />}
        <Containers projectId={project.id} serverId={project.serverId} memoryMb={project.memoryMb} cpus={project.cpus} refreshKey={history[0]?.id + (history[0]?.status ?? "")} />
      </div>

      <div className="mt-4">
        <Domains project={project} onEdit={() => setEditing(true)} />
      </div>
      <ProjectAccess key={project.id} project={project} onSaved={() => void reload()} />
      {(project.domains?.length ?? 0) > 0 && <SiteHistoryCard projectId={project.id} />}

      {!!project.gitKind && !project.compose && (
        <div className="mt-4">
          <AutoDeploy project={project} onChanged={() => void reload()} />
        </div>
      )}

      <div className="mt-4">
        <ProjectLogs projectId={project.id} />
      </div>

      {project.runtime === "compose" && project.last && <ProjectBackups project={project} />}
      {project.last && <ProjectJobs project={project} />}
      {!project.compose && !project.previewOf && <ProjectPreviews project={project} onChanged={() => void reload()} />}

      <History deployments={history} deploying={deploying} onRollback={(d) => void deploy(d)} />
    </Page>
  );
}

function Page({ children, onBack, backLabel = "Projects" }: { children: React.ReactNode; onBack: () => void; backLabel?: string }) {
  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      <BackButton label={backLabel} onClick={onBack} />
      {children}
    </div>
  );
}

/** A titled section card (shadcn Card) with an optional header action. */
function Panel({
  title,
  icon: Icon,
  action,
  children,
}: {
  title: string;
  icon: LucideIcon;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <Card size="sm" className="gap-0 py-0">
      <CardHeader className="border-b py-2.5">
        <CardTitle className="flex items-center gap-2">
          <Icon className="size-4 text-muted-foreground" />
          {title}
        </CardTitle>
        {action && <CardAction className="flex items-center gap-1 self-center">{action}</CardAction>}
      </CardHeader>
      <CardContent className="py-3">{children}</CardContent>
    </Card>
  );
}

function Loading({ text = "Loading…" }: { text?: string }) {
  return (
    <p className="flex items-center gap-2 text-[13px] text-muted-foreground">
      <Loader2 className="size-3.5 animate-spin" /> {text}
    </p>
  );
}

function RepoAccess({ project }: { project: ProjectDetail }) {
  const [key, setKey] = useState<string | null>(null);
  const [loadingKey, setLoadingKey] = useState(false);
  const [check, setCheck] = useState<{ busy: boolean; ok?: boolean; text?: string }>({ busy: false });

  async function showKey() {
    setLoadingKey(true);
    try {
      setKey(await api.deployKey(project.id));
    } catch (e) {
      setCheck({ busy: false, ok: false, text: (e as Error).message });
    } finally {
      setLoadingKey(false);
    }
  }

  async function checkAccess(prefix = "") {
    setCheck({ busy: true });
    try {
      setCheck({ busy: false, ok: true, text: prefix + (await api.checkRepoAccess(project.id)) });
    } catch (e) {
      setCheck({ busy: false, ok: false, text: prefix + (e as Error).message });
    }
  }

  const [adding, setAdding] = useState(false);
  async function addKey() {
    setAdding(true);
    setCheck({ busy: false });
    try {
      const msg = await api.addDeployKey(project.id);
      await checkAccess(msg + " ");
    } catch (e) {
      setCheck({ busy: false, ok: false, text: (e as Error).message });
    } finally {
      setAdding(false);
    }
  }

  return (
    <Panel
      title="Repository access"
      icon={KeyRound}
      action={
        <Button variant="ghost" size="xs" onClick={() => checkAccess()} disabled={check.busy} className="text-primary">
          {check.busy ? <Loader2 className="animate-spin" /> : <ShieldCheck />}
          {check.busy ? "Checking…" : "Check access"}
        </Button>
      }
    >
      {project.usesSsh ? (
        <>
          <p className="text-[13px] text-muted-foreground">
            The server pulls this repo with its own SSH key for this project. It needs to be on the repository as a{" "}
            <span className="text-foreground">read-only deploy key</span>.
          </p>
          {project.gitConnected && (
            <Button variant="secondary" size="sm" onClick={addKey} disabled={adding || check.busy} className="mt-3">
              {adding ? <Loader2 className="animate-spin" /> : <GitProviderIcon kind={project.gitKind} className="size-4" />}
              {adding ? "Adding key…" : `Add deploy key on ${ciInfo(project.gitKind, project.slug, project.branch, project.repoUrl).label}`}
            </Button>
          )}
          {key ? (
            <div className="mt-3">
              <code className="selectable block rounded-md border border-border bg-background p-2.5 font-mono text-[11px] leading-relaxed break-all">
                {key}
              </code>
              <div className="mt-2 flex flex-wrap gap-1">
                <CopyButton text={key} label="Copy key" size="xs" />
                {project.deployKeyUrl && (
                  <Button variant="ghost" size="xs" className="text-primary" onClick={() => api.openURL(project.deployKeyUrl!)}>
                    <ExternalLink /> Open the repo&apos;s deploy keys page
                  </Button>
                )}
              </div>
            </div>
          ) : project.gitConnected ? (
            <Button variant="link" size="xs" onClick={showKey} disabled={loadingKey} className="mt-2 block h-auto px-0 text-muted-foreground">
              {loadingKey ? "Connecting…" : "Or add it by hand: show the key"}
            </Button>
          ) : (
            <Button variant="outline" size="sm" onClick={showKey} disabled={loadingKey} className="mt-3">
              {loadingKey ? <Loader2 className="animate-spin" /> : <KeyRound />}
              {loadingKey ? "Connecting…" : "Show deploy key"}
            </Button>
          )}
        </>
      ) : (
        <p className="text-[13px] text-muted-foreground">
          HTTPS URL: works for public repositories. For a private repo, switch to the SSH URL (git@…) and add a deploy key.
        </p>
      )}
      {check.text && (
        <Notice tone={check.ok ? "success" : "error"} className="mt-3">
          {check.text}
        </Notice>
      )}
    </Panel>
  );
}

function Containers({
  projectId,
  serverId,
  memoryMb,
  cpus,
  refreshKey,
}: {
  projectId: string;
  serverId?: string;
  memoryMb?: number;
  cpus?: number;
  refreshKey: string;
}) {
  const [list, setList] = useState<ContainerStatus[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback((quiet = false) => {
    if (!quiet) setBusy(true);
    return api
      .projectStatus(projectId)
      .then(
        (l) => {
          setList(l);
          setError(null);
        },
        (e: Error) => setError(e.message),
      )
      .finally(() => setBusy(false));
  }, [projectId]);

  // Refresh when the latest deployment changes.
  useEffect(() => {
    const t = setTimeout(() => void load(), 0);
    return () => clearTimeout(t);
  }, [load, refreshKey]);

  // Keep the usage numbers fresh while the page is open and visible.
  useEffect(() => {
    const t = setInterval(() => {
      if (document.visibilityState === "visible") void load(true);
    }, 30_000);
    return () => clearInterval(t);
  }, [load]);

  const limits = [memoryMb ? formatMB(memoryMb) + " memory" : "", cpus ? `${cpus} CPU${cpus === 1 ? "" : "s"}` : ""].filter(Boolean);

  return (
    <Panel
      title="Containers"
      icon={Box}
      action={
        <Button variant="ghost" size="xs" onClick={() => void load()} disabled={busy} className="text-primary">
          <RefreshCw className={cn(busy && "animate-spin")} />
          {busy ? "Refreshing…" : "Refresh"}
        </Button>
      }
    >
      {error ? (
        <Notice tone="error">{error}</Notice>
      ) : list === null ? (
        <Loading text="Checking…" />
      ) : list.length === 0 ? (
        <p className="text-[13px] text-muted-foreground">Nothing running. Deploy to start the project.</p>
      ) : (
        <>
        {limits.length > 0 && (
          <p className="mb-3 text-[12px] text-muted-foreground">
            Limited to {limits.join(" and ")}{list.length > 1 ? " per container" : ""}.
          </p>
        )}
        <ul className="space-y-3">
          {list.map((c) => (
            <li key={c.name} className="flex items-start gap-2.5 text-[13px]">
              <span
                className={`mt-1.5 size-2 shrink-0 rounded-full ${
                  c.state === "running" ? "bg-success" : c.state === "restarting" ? "bg-warning" : "bg-destructive"
                }`}
              />
              <div className="min-w-0 flex-1">
                <div className="selectable font-mono">{c.name}</div>
                <div className="text-muted-foreground">
                  {c.status}
                  {c.ports && <span className="selectable font-mono"> · {c.ports}</span>}
                </div>
                {c.state === "running" && c.memory && <Usage cpu={c.cpu} memory={c.memory} memPerc={c.memPerc} />}
              </div>
              {c.state === "running" && serverId && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button asChild variant="ghost" size="icon-xs" className="text-muted-foreground" aria-label={`Open a shell in ${c.name}`}>
                      <Link href={`/terminal/?server=${encodeURIComponent(serverId)}&container=${encodeURIComponent(c.name)}`}>
                        <SquareTerminal />
                      </Link>
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>Open a shell</TooltipContent>
                </Tooltip>
              )}
            </li>
          ))}
        </ul>
        </>
      )}
    </Panel>
  );
}

function formatMB(mb: number) {
  return mb >= 1024 && mb % 1024 === 0 ? `${mb / 1024} GB` : mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${mb} MB`;
}

/** Live CPU and memory of a running container, from docker stats. */
function Usage({ cpu, memory, memPerc }: { cpu?: string; memory: string; memPerc?: string }) {
  const pct = Math.min(100, parseFloat(memPerc ?? "") || 0);
  const [used, of] = memory.split(" / ");
  return (
    <div className="mt-1.5 grid max-w-sm grid-cols-[auto_1fr] items-center gap-x-2.5 gap-y-1 text-[12px]">
      <span className="text-muted-foreground">Memory</span>
      <div className="flex items-center gap-2">
        <div
          className="h-1.5 flex-1 overflow-hidden rounded-full bg-surface-2"
          role="meter"
          aria-label="Memory used"
          aria-valuenow={pct}
          aria-valuemin={0}
          aria-valuemax={100}
        >
          <div className={cn("h-full rounded-full", pct >= 90 ? "bg-destructive" : pct >= 75 ? "bg-warning" : "bg-primary")} style={{ width: `${pct}%` }} />
        </div>
        <span className="font-mono tabular-nums">
          {used}
          {of && <span className="text-muted-foreground"> of {of}</span>}
        </span>
      </div>
      {cpu && (
        <>
          <span className="text-muted-foreground">CPU</span>
          <span className="font-mono tabular-nums">{cpu}</span>
        </>
      )}
    </div>
  );
}

function History({
  deployments,
  deploying,
  onRollback,
}: {
  deployments: Deployment[];
  deploying: boolean;
  onRollback: (d: Deployment) => void;
}) {
  const [open, setOpen] = useState<string | null>(null);
  // The newest successful deploy is what's live; rolling back to it is a no-op.
  const liveId = deployments.find((d) => d.status === "success")?.id;
  return (
    <section className="mt-6">
      <h2 className="mb-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">Deploy history</h2>
      {deployments.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border p-6 text-center text-sm text-muted-foreground">No deploys yet.</p>
      ) : (
        <ul className="divide-y divide-border overflow-hidden rounded-xl bg-card ring-1 ring-foreground/10">
          {deployments.map((d) => (
            <li key={d.id}>
              <Button
                variant="ghost"
                onClick={() => setOpen(open === d.id ? null : d.id)}
                className="h-auto w-full justify-start gap-3 rounded-none px-4 py-3 text-left font-normal"
              >
                <DeployStatus status={d.status} live={d.id === liveId} />
                <span className="font-mono text-xs text-muted-foreground">{d.commit || "———"}</span>
                {d.rollbackOf && (
                  <Badge variant="secondary" className="text-[10px]">
                    <Undo2 /> rollback
                  </Badge>
                )}
                {d.rolledBackTo ? (
                  <Badge variant="outline" className="border-warning/25 bg-warning/5 text-[10px] text-warning">
                    <Undo2 /> auto-rolled back to {d.rolledBackTo}
                  </Badge>
                ) : d.unhealthy ? (
                  <Badge variant="outline" className="border-destructive/25 bg-destructive/5 text-[10px] text-destructive">
                    unhealthy
                  </Badge>
                ) : null}
                <span className="min-w-0 flex-1 truncate text-sm">{d.message || d.error || ""}</span>
                <span className="shrink-0 text-xs text-muted-foreground">
                  {timeAgo(d.startedAt)}
                  {isSet(d.finishedAt) && ` · ${duration(d.startedAt, d.finishedAt)}`}
                </span>
              </Button>
              {open === d.id && (
                <div className="border-t border-border p-3">
                  {d.status === "success" && d.sha && d.id !== liveId && (
                    <div className="mb-3 flex items-center justify-between gap-3 rounded-md bg-muted px-3 py-2 text-[13px]">
                      <span className="text-muted-foreground">Something wrong with the latest version? Redeploy this one exactly as it was.</span>
                      <Button variant="outline" size="sm" onClick={() => onRollback(d)} disabled={deploying} className="shrink-0">
                        <Undo2 /> Roll back to {d.commit}
                      </Button>
                    </div>
                  )}
                  {d.error && (
                    <Notice tone="error" className="mb-2">
                      {d.error}
                    </Notice>
                  )}
                  <LogConsole entries={(d.log ?? []).map((line) => ({ line }))} title="Output" height="h-64" />
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function Domains({ project, onEdit }: { project: ProjectDetail; onEdit: () => void }) {
  const domains = project.domains ?? [];
  const redirects = project.redirects ?? [];
  const target = (domains[0] ?? "").split("/")[0];
  const [status, setStatus] = useState<DomainsStatus | null>(null);
  const [checking, setChecking] = useState(false);
  const [applying, setApplying] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);

  const check = useCallback(() => {
    setChecking(true);
    return api
      .checkDomains(project.id)
      .then(setStatus, (e: Error) => setMsg({ ok: false, text: e.message }))
      .finally(() => setChecking(false));
  }, [project.id]);

  const key = [...domains, ...redirects].join(",");
  useEffect(() => {
    if (!key) return;
    const t = setTimeout(() => void check(), 0);
    return () => clearTimeout(t);
  }, [check, key]);

  async function apply() {
    setApplying(true);
    setMsg(null);
    try {
      setMsg({ ok: true, text: await api.applyDomains(project.id) });
      await check();
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    } finally {
      setApplying(false);
    }
  }

  if (domains.length === 0) {
    return (
      <Panel title="Domains" icon={Globe}>
        <p className="flex flex-wrap items-center gap-1 text-[13px] text-muted-foreground">
          No domain yet. Add one to serve this project over HTTPS with a free certificate.
          <Button variant="link" size="xs" onClick={onEdit} className="h-auto px-0">
            Add a domain <ArrowRight />
          </Button>
        </p>
      </Panel>
    );
  }

  const proxyRunning = status?.proxy.startsWith("running");
  return (
    <Panel
      title="Domains"
      icon={Globe}
      action={
        <>
          <Button variant="ghost" size="xs" onClick={() => void check()} disabled={checking || applying} className="text-primary">
            <RefreshCw className={cn(checking && "animate-spin")} />
            {checking ? "Checking…" : "Re-check"}
          </Button>
          <Button size="xs" onClick={apply} disabled={applying || checking}>
            {applying ? <Loader2 className="animate-spin" /> : <ShieldCheck />}
            {applying ? "Applying…" : proxyRunning ? "Apply changes" : "Set up HTTPS"}
          </Button>
        </>
      }
    >
      <ul className="space-y-3">
        {[...domains, ...redirects].map((d, i) => {
          const isRedirect = i >= domains.length;
          const c = status?.checks?.find((x) => x.domain === d);
          const good = c?.dns === "ok" && c?.https === "ok" && c.httpStatus !== 502;
          return (
            <li key={d} className="flex items-start gap-3">
              <span
                className={`mt-2 size-2 shrink-0 rounded-full ${
                  !c ? "bg-muted-foreground/40" : good ? "bg-success" : c.dns === "ok" ? "bg-warning" : "bg-destructive"
                }`}
              />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <Button
                    variant="link"
                    size="xs"
                    onClick={() => api.openURL(`https://${d}`)}
                    className="h-auto px-0 font-mono text-sm text-foreground hover:text-primary"
                  >
                    {d} <ExternalLink />
                  </Button>
                  {isRedirect && (
                    <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                      <ArrowRight className="size-3" /> redirects to <span className="font-mono">{target}</span>
                    </span>
                  )}
                  {c && (
                    <>
                      <Pill ok={c.dns === "ok"}>
                        DNS {c.dns === "ok" ? <CircleCheck /> : c.dns}
                      </Pill>
                      {c.https !== "skipped" && (
                        <Pill ok={c.https === "ok"}>
                          HTTPS {c.https === "ok" ? c.httpStatus === 502 ? "502" : <CircleCheck /> : <CircleX />}
                        </Pill>
                      )}
                    </>
                  )}
                </div>
                {c?.https === "ok" && <SiteFacts check={c} />}
                {c?.detail && <p className="selectable mt-0.5 text-[13px] text-muted-foreground">{c.detail}</p>}
              </div>
            </li>
          );
        })}
      </ul>
      <p className="mt-4 border-t border-border pt-3 text-xs text-muted-foreground">
        Forwards to <span className="font-mono">localhost:{project.proxyPort}</span>
        {status && (
          <>
            {" "}
            · Proxy: <span className={proxyRunning ? "text-success" : ""}>{status.proxy}</span>
            {status.serverIps?.length ? <> · Server IP {status.serverIps.join(", ")}</> : null}
          </>
        )}
      </p>
      {msg && (
        <Notice tone={msg.ok ? "success" : "error"} className="mt-3">
          {msg.text}
        </Notice>
      )}
    </Panel>
  );
}

function Pill({ ok, children }: { ok: boolean; children: React.ReactNode }) {
  return (
    <Badge
      variant="outline"
      className={cn("border-transparent", ok ? "bg-success/10 text-success" : "bg-warning/10 text-warning")}
    >
      {children}
    </Badge>
  );
}

function AutoDeploy({ project, onChanged }: { project: ProjectDetail; onChanged: () => void }) {
  const confirm = useConfirm();
  const [busy, setBusy] = useState<"on" | "off" | null>(null);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [runs, setRuns] = useState<AutoDeployRuns | null>(null);
  const [loadingRuns, setLoadingRuns] = useState(false);
  const [showLog, setShowLog] = useState(false);
  const enabled = !!project.autoDeploy;
  const ci = ciInfo(project.gitKind, project.slug, project.branch, project.repoUrl);

  const loadRuns = useCallback(() => {
    setLoadingRuns(true);
    return api
      .autoDeployHistory(project.id)
      .then(setRuns, () => setRuns({ runs: [], lastLog: [] }))
      .finally(() => setLoadingRuns(false));
  }, [project.id]);

  useEffect(() => {
    if (!enabled) return;
    const t = setTimeout(() => void loadRuns(), 0);
    return () => clearTimeout(t);
  }, [enabled, loadRuns]);

  async function turnOn(refresh = false) {
    const ok = await confirm({
      title: refresh ? "Update auto-deploy?" : "Turn on auto-deploy?",
      message: refresh
        ? "TrueDeploy refreshes the server hook and the pipeline for the new settings, and rotates the CI key."
        : `Every push to ${project.branch} will deploy ${project.name}. TrueDeploy will:`,
      items: refresh
        ? undefined
        : [
            "Install a deploy hook on the server, with an SSH key that can only run it",
            `Save that key and the server's identity as ${ci.secrets}`,
            ...ci.commits.map((c) => `Commit ${c} to ${project.branch}`),
          ],
      confirmLabel: refresh ? "Update" : "Turn on",
    });
    if (!ok) return;
    setBusy("on");
    setMsg(null);
    try {
      setMsg({ ok: true, text: await api.enableAutoDeploy(project.id) });
      onChanged();
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    } finally {
      setBusy(null);
    }
  }

  async function turnOff() {
    const ok = await confirm({
      title: "Turn off auto-deploy?",
      message: `The CI key is removed from the server first (that's what revokes access), then the pipeline and secrets from ${ci.label}.`,
      confirmLabel: "Turn off",
      tone: "danger",
    });
    if (!ok) return;
    setBusy("off");
    setMsg(null);
    try {
      setMsg({ ok: true, text: await api.disableAutoDeploy(project.id) });
      setRuns(null);
      onChanged();
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    } finally {
      setBusy(null);
    }
  }

  return (
    <Panel
      title="Auto-deploy on push"
      icon={Zap}
      action={
        enabled ? (
          <>
            {ci.pipelinesUrl && (
              <Button variant="ghost" size="xs" className="text-primary" onClick={() => api.openURL(ci.pipelinesUrl!)}>
                <ExternalLink /> {ci.ci}
              </Button>
            )}
            <Button variant="ghost" size="xs" onClick={turnOff} disabled={busy !== null} className="text-muted-foreground hover:text-destructive">
              {busy === "off" ? <Loader2 className="animate-spin" /> : <Power />}
              {busy === "off" ? "Turning off…" : "Turn off"}
            </Button>
          </>
        ) : undefined
      }
    >
      {!enabled ? (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="max-w-xl text-[13px] text-muted-foreground">
            Deploy automatically every time you push to <span className="font-mono text-foreground">{project.branch}</span>, using{" "}
            {ci.ci}. Secrets stay on the server; {ci.label} only gets a key that can run this one deploy.
            {!project.gitConnected && <> Connect {ci.label} in Settings first.</>}
          </p>
          <Button variant="secondary" onClick={() => turnOn()} disabled={busy !== null}>
            {busy === "on" ? <Loader2 className="animate-spin" /> : <GitProviderIcon kind={project.gitKind} className="size-4" />}
            {busy === "on" ? "Setting up…" : "Turn on"}
          </Button>
        </div>
      ) : (
        <>
          <div className="flex items-center gap-2 text-[13px]">
            <span className="size-2 rounded-full bg-success" />
            <span>
              On: every push to <span className="font-mono">{project.branch}</span> deploys this project.
            </span>
          </div>
          {project.autoDeployStale && (
            <Notice
              tone="warning"
              className="mt-3 items-center"
              action={
                <Button variant="outline" size="xs" onClick={() => turnOn(true)} disabled={busy !== null}>
                  {busy === "on" ? <Loader2 className="animate-spin" /> : <RotateCw />}
                  {busy === "on" ? "Updating…" : "Update auto-deploy"}
                </Button>
              }
            >
              Project settings changed since auto-deploy was set up.
            </Notice>
          )}

          <div className="mt-4 flex items-center justify-between">
            <span className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Recent auto-deploys</span>
            <Button variant="ghost" size="xs" onClick={() => void loadRuns()} disabled={loadingRuns} className="text-primary">
              <RefreshCw className={cn(loadingRuns && "animate-spin")} />
              {loadingRuns ? "Loading…" : "Refresh"}
            </Button>
          </div>
          {!runs ? (
            <div className="mt-2">
              <Loading />
            </div>
          ) : runs.runs?.length ? (
            <ul className="mt-2 divide-y divide-border rounded-md border border-border">
              {runs.runs.slice(0, 8).map((r, i) => (
                <li key={i} className="flex items-center gap-3 px-3 py-2 text-[13px]">
                  <DeployStatus status={r.status} />
                  <span className="font-mono text-xs text-muted-foreground">{r.commit || "———"}</span>
                  <span className="min-w-0 flex-1 truncate">{r.message}</span>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {timeAgo(r.startedAt)} · {duration(r.startedAt, r.finishedAt)}
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <p className="mt-2 text-[13px] text-muted-foreground">No pushes deployed yet.</p>
          )}
          {(runs?.lastLog?.length ?? 0) > 0 && (
            <div className="mt-3">
              <Button variant="ghost" size="xs" onClick={() => setShowLog(!showLog)} className="text-primary">
                <ScrollText />
                {showLog ? "Hide" : "Show"} output of the last auto-deploy
              </Button>
              {showLog && (
                <div className="mt-2">
                  <LogConsole entries={(runs?.lastLog ?? []).map((line) => ({ line }))} title="Last auto-deploy" height="h-64" />
                </div>
              )}
            </div>
          )}
        </>
      )}
      {msg && (
        <Notice tone={msg.ok ? "success" : "error"} className="mt-3">
          {msg.text}
        </Notice>
      )}
    </Panel>
  );
}

/** For one-click apps: where to open it and what to do first. */
function AppPanel({ project }: { project: ProjectDetail }) {
  const [template, setTemplate] = useState<Template | null>(null);
  useEffect(() => {
    api.templates().then((all) => setTemplate(all.find((t) => t.id === project.template) ?? null), () => {});
  }, [project.template]);
  const url = project.env?.PUBLIC_URL;
  const deployed = project.last?.status === "success";
  return (
    <Panel
      title="App"
      icon={Box}
      action={
        url && (
          <Button variant="ghost" size="xs" className="text-primary" onClick={() => api.openURL(url)} disabled={!deployed}>
            Open <ExternalLink />
          </Button>
        )
      }
    >
      <div className="flex items-center gap-3">
        <AppIcon id={project.template ?? ""} className="size-9" />
        <div className="min-w-0">
          <div className="text-sm font-medium">{template?.name ?? project.template}</div>
          {url && <div className="selectable truncate font-mono text-xs text-muted-foreground">{url}</div>}
        </div>
      </div>
      {template?.notes?.length ? (
        <ul className="mt-3 space-y-1.5 border-t border-border pt-3 text-[13px] text-muted-foreground">
          {template.notes.map((n) => (
            <li key={n} className="flex gap-2">
              <span className="mt-2 size-1 shrink-0 rounded-full bg-muted-foreground/60" />
              <span>{n}</span>
            </li>
          ))}
        </ul>
      ) : null}
      <p className="mt-3 text-[12px] text-muted-foreground">
        Changed its domain or port? Redeploy so the app uses the new address.
      </p>
    </Panel>
  );
}

/** Status code, response time and certificate expiry of a domain that answers. */
function SiteFacts({ check: c }: { check: DomainCheck }) {
  const days = c.certDays >= 0 ? c.certDays : null;
  return (
    <p className="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
      <span className={cn("font-mono", (c.httpStatus ?? 0) >= 500 ? "text-destructive" : "")}>{c.httpStatus}</span>
      {c.latencyMs ? (
        <>
          <span aria-hidden>·</span>
          <span className={cn(c.latencyMs > 2000 && "text-warning")}>{c.latencyMs} ms</span>
        </>
      ) : null}
      {days !== null && (
        <>
          <span aria-hidden>·</span>
          <span className={cn(days < 3 ? "text-destructive" : days < 14 ? "text-warning" : "")}>
            {days === 0 ? "certificate expires today" : `certificate valid ${days === 1 ? "1 more day" : `${days} more days`}`}
          </span>
        </>
      )}
    </p>
  );
}
