"use client";

import { useCallback, useEffect, useState } from "react";
import { api, type ProjectView, type Server } from "@/lib/api";
import { ProjectForm } from "@/components/project-form";
import { ProjectDetailView } from "@/components/project-detail";
import { DeployStatus } from "@/components/deploy-status";
import { timeAgo } from "@/lib/format";

type View = { kind: "list" } | { kind: "new" } | { kind: "detail"; id: string };

export default function ProjectsPage() {
  const [projects, setProjects] = useState<ProjectView[] | null>(null);
  const [servers, setServers] = useState<Server[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [view, setView] = useState<View>({ kind: "list" });

  const load = useCallback(
    () =>
      Promise.all([api.listProjects(), api.listServers()]).then(
        ([p, s]) => {
          setProjects(p);
          setServers(s);
          setError(null);
        },
        (e: Error) => {
          setError(e.message);
          setProjects([]);
        },
      ),
    [],
  );

  useEffect(() => {
    void load();
  }, [load]);

  const toList = () => {
    setView({ kind: "list" });
    void load();
  };

  if (view.kind === "detail") {
    return <ProjectDetailView projectId={view.id} servers={servers} onBack={toList} onDeleted={toList} />;
  }

  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      {view.kind === "new" && (
        <button onClick={toList} className="mb-4 text-sm text-muted hover:text-foreground">
          ← Projects
        </button>
      )}
      <header className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">{view.kind === "new" ? "New project" : "Projects"}</h1>
          <p className="mt-1 text-sm text-muted">
            {view.kind === "new"
              ? "Link a Git repository to a server and choose how to run it."
              : "Git repositories deployed to your servers with Docker."}
          </p>
        </div>
        {view.kind === "list" && (
          <button onClick={() => setView({ kind: "new" })} className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-ink">
            + New project
          </button>
        )}
      </header>

      {error && <p className="mb-4 rounded-md bg-danger/10 p-3 text-sm text-danger">{error}</p>}

      {view.kind === "new" ? (
        <ProjectForm servers={servers} onCancel={toList} onSaved={(p) => setView({ kind: "detail", id: p.id })} />
      ) : projects === null ? (
        <p className="text-sm text-muted">Loading…</p>
      ) : projects.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-12 text-center">
          <p className="text-sm text-muted">No projects yet.</p>
          <button onClick={() => setView({ kind: "new" })} className="mt-3 text-sm text-accent">
            Create your first project →
          </button>
        </div>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2">
          {projects.map((p) => (
            <li key={p.id}>
              <button
                onClick={() => setView({ kind: "detail", id: p.id })}
                className="w-full rounded-lg border border-border bg-surface p-4 text-left transition-colors hover:border-accent/60"
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate font-medium">{p.name}</span>
                  {p.last ? <DeployStatus status={p.last.status} /> : <span className="text-[11px] text-muted">Not deployed</span>}
                </div>
                <div className="mt-1 truncate font-mono text-xs text-muted">
                  {p.repoUrl.replace(/^git@|^https:\/\//, "").replace(/\.git$/, "")} · {p.branch}
                </div>
                <div className="mt-3 flex items-center justify-between text-xs text-muted">
                  <span>
                    {p.serverName || "unknown server"} · {p.runtime === "compose" ? "Docker Compose" : `Dockerfile :${p.hostPort}`}
                  </span>
                  {p.last && (
                    <span>
                      {p.last.commit && <span className="font-mono">{p.last.commit} · </span>}
                      {timeAgo(p.last.startedAt)}
                    </span>
                  )}
                </div>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
