"use client";

import { ArrowRight, Box, FolderGit2, GitBranch, LayoutGrid, Loader2, Plus, Server as ServerIcon } from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { api, type ProjectView, type Server } from "@/lib/api";
import { BackButton } from "@/components/back-button";
import { DeepLink } from "@/components/deep-link";
import { DeployStatus } from "@/components/deploy-status";
import { Notice } from "@/components/notice";
import { ProjectDetailView } from "@/components/project-detail";
import { ProjectForm } from "@/components/project-form";
import { Button } from "@/components/ui/button";
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

  // /projects/?id=<project> opens it; /projects/?new=1 opens the form.
  const link = (
    <DeepLink
      onLink={(p) => {
        const id = p.get("id");
        if (id) setView({ kind: "detail", id });
        else if (p.has("new")) setView({ kind: "new" });
      }}
    />
  );

  if (view.kind === "detail") {
    return (
      <>
        {link}
        <ProjectDetailView key={view.id} projectId={view.id} servers={servers} onBack={toList} onDeleted={toList} />
      </>
    );
  }

  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      {link}
      {view.kind === "new" && <BackButton label="Projects" onClick={toList} />}
      <header className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">{view.kind === "new" ? "New project" : "Projects"}</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {view.kind === "new"
              ? "Link a Git repository to a server and choose how to run it."
              : "Git repositories deployed to your servers with Docker."}
          </p>
        </div>
        {view.kind === "list" && (
          <div className="flex gap-2">
            <Button variant="outline" asChild>
              <Link href="/apps/">
                <LayoutGrid /> Install an app
              </Link>
            </Button>
            <Button onClick={() => setView({ kind: "new" })}>
              <Plus /> New project
            </Button>
          </div>
        )}
      </header>

      {error && <Notice tone="error" className="mb-4">{error}</Notice>}

      {view.kind === "new" ? (
        <ProjectForm servers={servers} onCancel={toList} onSaved={(p) => setView({ kind: "detail", id: p.id })} />
      ) : projects === null ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" /> Loading…
        </p>
      ) : projects.length === 0 ? (
        <div className="flex flex-col items-center rounded-lg border border-dashed border-border p-12 text-center">
          <FolderGit2 className="mb-3 size-8 text-muted-foreground" />
          <p className="text-sm text-muted-foreground">No projects yet.</p>
          <Button variant="link" onClick={() => setView({ kind: "new" })} className="mt-1">
            Create your first project <ArrowRight />
          </Button>
        </div>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2">
          {projects.map((p) => (
            <li key={p.id}>
              <Button
                variant="outline"
                onClick={() => setView({ kind: "detail", id: p.id })}
                className="h-auto w-full flex-col items-stretch gap-0 bg-card p-4 text-left font-normal whitespace-normal hover:border-primary/60 hover:bg-card dark:bg-card dark:hover:bg-card"
              >
                <span className="flex items-center justify-between gap-2">
                  <span className="flex min-w-0 items-center gap-2">
                    <span className="truncate font-medium">{p.name}</span>
                    {p.previewOf && (
                      <span className="shrink-0 rounded border border-border px-1.5 text-[10px] text-muted-foreground">preview</span>
                    )}
                  </span>
                  {p.last ? <DeployStatus status={p.last.status} /> : <span className="text-[11px] text-muted-foreground">Not deployed</span>}
                </span>
                <span className="mt-1 flex items-center gap-1.5 truncate font-mono text-xs text-muted-foreground">
                  {p.compose ? <Box className="size-3.5 shrink-0" /> : <GitBranch className="size-3.5 shrink-0" />}
                  <span className="truncate">
                    {p.compose
                      ? `App · ${p.template} · port ${p.appPort}`
                      : `${p.repoUrl.replace(/^git@|^https:\/\//, "").replace(/\.git$/, "")} · ${p.branch}`}
                  </span>
                </span>
                <span className="mt-3 flex items-center justify-between gap-2 text-xs text-muted-foreground">
                  <span className="flex items-center gap-1.5">
                    <ServerIcon className="size-3.5" />
                    {p.serverName || "unknown server"} · {p.runtime === "compose" ? "Docker Compose" : `Dockerfile :${p.hostPort}`}
                  </span>
                  {p.last && (
                    <span>
                      {p.last.commit && <span className="font-mono">{p.last.commit} · </span>}
                      {timeAgo(p.last.startedAt)}
                    </span>
                  )}
                </span>
              </Button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
