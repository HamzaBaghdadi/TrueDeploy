"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  api,
  onDeployEvents,
  type ContainerStatus,
  type Deployment,
  type ProjectDetail,
  type Server,
} from "@/lib/api";
import { useConfirm } from "@/components/confirm-dialog";
import { LogConsole, type LogEntry } from "@/components/log-console";
import { DeployStatus } from "@/components/deploy-status";
import { ProjectForm } from "@/components/project-form";
import { duration, isSet, timeAgo } from "@/lib/format";

const btn = "rounded-md border border-border px-3 py-1.5 text-sm hover:border-accent disabled:opacity-50";

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

  async function deploy() {
    setDeployError(null);
    setNotice(null);
    setLiveLog([]);
    try {
      const id = await api.deploy(projectId);
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
        <p className="selectable rounded-md bg-danger/10 p-3 text-sm text-danger">{error}</p>
      </Page>
    );
  }
  if (!project) {
    return (
      <Page onBack={onBack}>
        <p className="text-sm text-muted">Loading…</p>
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
            {project.last && <DeployStatus status={project.last.status} />}
          </div>
          <p className="selectable mt-1 truncate font-mono text-xs text-muted">
            {project.repoUrl} · {project.branch} → {project.serverName || "unknown server"}:{project.appDir}
          </p>
        </div>
        <div className="flex gap-2">
          <button onClick={() => setEditing(true)} disabled={deploying} className={btn}>
            Edit
          </button>
          <button onClick={stop} disabled={deploying} className={btn}>
            Stop
          </button>
          <button onClick={remove} disabled={deploying} className={`${btn} text-muted hover:border-danger hover:text-danger`}>
            Delete
          </button>
          {deploying ? (
            <button
              onClick={() => deployId && api.cancelDeploy(deployId)}
              className="rounded-md border border-danger/60 px-4 py-1.5 text-sm text-danger hover:bg-danger/10"
            >
              Cancel deploy
            </button>
          ) : (
            <button onClick={deploy} className="rounded-md bg-accent px-4 py-1.5 text-sm font-medium text-accent-ink">
              Deploy
            </button>
          )}
        </div>
      </header>

      {deployError && <p className="selectable mb-4 rounded-md bg-danger/10 p-3 text-sm text-danger">{deployError}</p>}
      {notice && (
        <p
          className={`selectable mb-4 rounded-md p-3 text-sm ${
            notice.tone === "ok" ? "bg-accent/10 text-accent" : "bg-danger/10 text-danger"
          }`}
        >
          {notice.text}
        </p>
      )}

      {(deploying || liveLog.length > 0) && (
        <div className="mb-6">
          <LogConsole entries={liveLog} running={deploying} title={deploying ? "Deploying…" : "Last deploy output"} height="h-96" />
        </div>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        <RepoAccess project={project} />
        <Containers projectId={project.id} refreshKey={history[0]?.id + (history[0]?.status ?? "")} />
      </div>

      <History deployments={history} />
    </Page>
  );
}

function Page({ children, onBack, backLabel = "Projects" }: { children: React.ReactNode; onBack: () => void; backLabel?: string }) {
  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      <button onClick={onBack} className="mb-4 text-sm text-muted hover:text-foreground">
        ← {backLabel}
      </button>
      {children}
    </div>
  );
}

function Card({ title, action, children }: { title: string; action?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="rounded-lg border border-border bg-surface">
      <div className="flex items-center justify-between border-b border-border px-4 py-2.5">
        <h2 className="text-sm font-medium">{title}</h2>
        {action}
      </div>
      <div className="p-4">{children}</div>
    </section>
  );
}

function RepoAccess({ project }: { project: ProjectDetail }) {
  const [key, setKey] = useState<string | null>(null);
  const [loadingKey, setLoadingKey] = useState(false);
  const [copied, setCopied] = useState(false);
  const [check, setCheck] = useState<{ busy: boolean; ok?: boolean; text?: string }>({ busy: false });

  async function showKey() {
    setLoadingKey(true);
    try {
      setKey(await api.deployKey(project.serverId));
    } catch (e) {
      setCheck({ busy: false, ok: false, text: (e as Error).message });
    } finally {
      setLoadingKey(false);
    }
  }

  async function checkAccess() {
    setCheck({ busy: true });
    try {
      setCheck({ busy: false, ok: true, text: await api.checkRepoAccess(project.id) });
    } catch (e) {
      setCheck({ busy: false, ok: false, text: (e as Error).message });
    }
  }

  return (
    <Card
      title="Repository access"
      action={
        <button onClick={checkAccess} disabled={check.busy} className="text-xs text-accent disabled:opacity-50">
          {check.busy ? "Checking…" : "Check access"}
        </button>
      }
    >
      {project.usesSsh ? (
        <>
          <p className="text-[13px] text-muted">
            The server pulls this repo with its own SSH key. Add the key below to the repository as a{" "}
            <span className="text-foreground">read-only deploy key</span>.
          </p>
          {key ? (
            <div className="mt-3">
              <code className="selectable block break-all rounded-md border border-border bg-background p-2.5 font-mono text-[11px] leading-relaxed">
                {key}
              </code>
              <div className="mt-2 flex gap-3 text-xs">
                <button
                  className="text-accent"
                  onClick={() =>
                    void navigator.clipboard.writeText(key).then(() => {
                      setCopied(true);
                      setTimeout(() => setCopied(false), 1500);
                    })
                  }
                >
                  {copied ? "Copied" : "Copy key"}
                </button>
                {project.deployKeyUrl && (
                  <button className="text-accent" onClick={() => api.openURL(project.deployKeyUrl!)}>
                    Open the repo&apos;s deploy keys page ↗
                  </button>
                )}
              </div>
            </div>
          ) : (
            <button onClick={showKey} disabled={loadingKey} className={`${btn} mt-3`}>
              {loadingKey ? "Connecting…" : "Show deploy key"}
            </button>
          )}
        </>
      ) : (
        <p className="text-[13px] text-muted">
          HTTPS URL: works for public repositories. For a private repo, switch to the SSH URL (git@…) and add a deploy key.
        </p>
      )}
      {check.text && (
        <p
          className={`selectable mt-3 rounded-md p-2.5 text-[13px] ${
            check.ok ? "bg-accent/10 text-accent" : "bg-danger/10 text-danger"
          }`}
        >
          {check.text}
        </p>
      )}
    </Card>
  );
}

function Containers({ projectId, refreshKey }: { projectId: string; refreshKey: string }) {
  const [list, setList] = useState<ContainerStatus[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    setBusy(true);
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

  return (
    <Card
      title="Containers"
      action={
        <button onClick={() => void load()} disabled={busy} className="text-xs text-accent disabled:opacity-50">
          {busy ? "Refreshing…" : "Refresh"}
        </button>
      }
    >
      {error ? (
        <p className="selectable text-[13px] text-danger">{error}</p>
      ) : list === null ? (
        <p className="text-[13px] text-muted">Checking…</p>
      ) : list.length === 0 ? (
        <p className="text-[13px] text-muted">Nothing running. Deploy to start the project.</p>
      ) : (
        <ul className="space-y-2">
          {list.map((c) => (
            <li key={c.name} className="flex items-start gap-2.5 text-[13px]">
              <span
                className={`mt-1.5 size-2 shrink-0 rounded-full ${
                  c.state === "running" ? "bg-accent" : c.state === "restarting" ? "bg-warning" : "bg-danger"
                }`}
              />
              <div className="min-w-0">
                <div className="selectable font-mono">{c.name}</div>
                <div className="text-muted">
                  {c.status}
                  {c.ports && <span className="selectable font-mono"> · {c.ports}</span>}
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function History({ deployments }: { deployments: Deployment[] }) {
  const [open, setOpen] = useState<string | null>(null);
  return (
    <section className="mt-6">
      <h2 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted">Deploy history</h2>
      {deployments.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border p-6 text-center text-sm text-muted">No deploys yet.</p>
      ) : (
        <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border bg-surface">
          {deployments.map((d) => (
            <li key={d.id}>
              <button
                onClick={() => setOpen(open === d.id ? null : d.id)}
                className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-surface-2"
              >
                <DeployStatus status={d.status} />
                <span className="font-mono text-xs text-muted">{d.commit || "———"}</span>
                <span className="min-w-0 flex-1 truncate text-sm">{d.message || d.error || ""}</span>
                <span className="shrink-0 text-xs text-muted">
                  {timeAgo(d.startedAt)}
                  {isSet(d.finishedAt) && ` · ${duration(d.startedAt, d.finishedAt)}`}
                </span>
              </button>
              {open === d.id && (
                <div className="border-t border-border p-3">
                  {d.error && <p className="selectable mb-2 text-[13px] text-danger">{d.error}</p>}
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
