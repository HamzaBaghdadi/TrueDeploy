"use client";

import { ArrowRight, CircleCheck, Circle, Rocket, X } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";
import { api, type ProjectView, type Server } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { cn } from "@/lib/utils";

const HIDE_KEY = "td.gettingStarted.hidden";
export const SETUP_DONE_KEY = "td.setupDone";

function readFlag(key: string) {
  try {
    return localStorage.getItem(key) === "1";
  } catch {
    return false;
  }
}

/** Marks that a server setup run finished (used by the checklist). */
export function markSetupDone() {
  try {
    localStorage.setItem(SETUP_DONE_KEY, "1");
  } catch {
    /* storage unavailable: the step just stays unchecked */
  }
}

type Step = { title: string; body: string; done: boolean; href: string; action: string; optional?: boolean };

/** First-run checklist on the Servers page. Hides itself once you've deployed. */
export function GettingStarted({ servers }: { servers: Server[] }) {
  const [projects, setProjects] = useState<ProjectView[] | null>(null);
  const [github, setGithub] = useState(false);
  const [hidden, setHidden] = useState(true);
  const [setupDone, setSetupDone] = useState(false);

  useEffect(() => {
    const t = setTimeout(() => {
      setHidden(readFlag(HIDE_KEY));
      setSetupDone(readFlag(SETUP_DONE_KEY));
    }, 0);
    api.listProjects().then(setProjects, () => setProjects([]));
    api.gitAccounts().then((a) => setGithub(a.length > 0), () => {});
    return () => clearTimeout(t);
  }, []);

  const deployed = !!projects?.some((p) => p.last?.status === "success");
  const first = servers[0];
  const steps: Step[] = [
    {
      title: "Add a server",
      body: "Connect a VPS over SSH with a password or key.",
      done: servers.length > 0,
      href: "/?add=1",
      action: "Add server",
    },
    {
      title: "Set up the server",
      body: "Install Docker, a firewall and security updates, or prepare your account if you don't have sudo.",
      done: setupDone || deployed,
      href: first ? `/?id=${encodeURIComponent(first.id)}&view=setup` : "/?add=1",
      action: "Set up",
    },
    {
      title: "Connect your Git host",
      body: "GitHub, GitLab, Bitbucket or Gitea: pick or create repositories and add deploy keys in one click.",
      done: github,
      href: "/settings/",
      action: "Connect",
      optional: true,
    },
    {
      title: "Create a project",
      body: "Link a Git repository to your server and choose how to run it.",
      done: (projects?.length ?? 0) > 0,
      href: "/projects/?new=1",
      action: "New project",
    },
    {
      title: "Deploy it",
      body: "Build and start it with Docker, then add a domain with free HTTPS.",
      done: deployed,
      href: projects?.[0] ? `/projects/?id=${encodeURIComponent(projects[0].id)}` : "/projects/?new=1",
      action: "Open project",
    },
  ];

  const required = steps.filter((s) => !s.optional);
  const doneCount = steps.filter((s) => s.done).length;
  if (hidden || projects === null || required.every((s) => s.done)) return null;
  const next = steps.find((s) => !s.done && !s.optional) ?? steps.find((s) => !s.done);

  return (
    <Card className="mb-6 overflow-hidden">
      <div className="bg-brand-gradient h-0.5" />
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Rocket className="size-4 text-brand-cyan" />
          Getting started
        </CardTitle>
        <CardDescription>
          {doneCount} of {steps.length} done: from an empty server to a live app.
        </CardDescription>
        <CardAction>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Hide getting started"
            className="text-muted-foreground"
            onClick={() => {
              setHidden(true);
              try {
                localStorage.setItem(HIDE_KEY, "1");
              } catch {
                /* ignore */
              }
            }}
          >
            <X />
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        <Progress value={(100 * doneCount) / steps.length} className="mb-4 h-1.5" />
        <ol className="space-y-1">
          {steps.map((s) => {
            const isNext = s === next;
            return (
              <li
                key={s.title}
                className={cn("flex items-center gap-3 rounded-lg px-3 py-2.5", isNext && "bg-accent")}
              >
                {s.done ? (
                  <CircleCheck className="size-5 shrink-0 text-success" />
                ) : (
                  <Circle className={cn("size-5 shrink-0", isNext ? "text-primary" : "text-muted-foreground/50")} />
                )}
                <div className="min-w-0 flex-1">
                  <div className={cn("text-sm font-medium", s.done && "text-muted-foreground line-through")}>
                    {s.title}
                    {s.optional && <span className="ml-2 text-xs font-normal text-muted-foreground">optional</span>}
                  </div>
                  {!s.done && <div className="text-[13px] text-muted-foreground">{s.body}</div>}
                </div>
                {!s.done && (
                  <Button size="sm" variant={isNext ? "default" : "outline"} asChild>
                    <Link href={s.href}>
                      {s.action}
                      <ArrowRight />
                    </Link>
                  </Button>
                )}
              </li>
            );
          })}
        </ol>
      </CardContent>
    </Card>
  );
}
