"use client";

import { ArrowRight, Globe, Loader2, Search } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { api, type Server, type Template } from "@/lib/api";
import { AppIcon } from "@/components/app-icon";
import { AppInstall } from "@/components/app-install";
import { BackButton } from "@/components/back-button";
import { DeepLink } from "@/components/deep-link";
import { Notice } from "@/components/notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

export default function AppsPage() {
  const router = useRouter();
  const [catalog, setCatalog] = useState<Template[] | null>(null);
  const [servers, setServers] = useState<Server[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [picked, setPicked] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState("all");

  useEffect(() => {
    Promise.all([api.templates(), api.listServers()]).then(
      ([t, s]) => {
        setCatalog(t);
        setServers(s);
      },
      (e: Error) => {
        setError(e.message);
        setCatalog([]);
      },
    );
  }, []);

  const categories = useMemo(() => [...new Set((catalog ?? []).map((t) => t.category))], [catalog]);
  const q = query.trim().toLowerCase();
  const shown = (catalog ?? []).filter(
    (t) =>
      (category === "all" || t.category === category) &&
      (!q || `${t.name} ${t.tagline} ${t.category}`.toLowerCase().includes(q)),
  );
  const template = catalog?.find((t) => t.id === picked);

  // /apps/?id=<template> opens its install form.
  const link = <DeepLink onLink={(p) => setPicked(p.get("id"))} />;

  if (template) {
    return (
      <div className="mx-auto max-w-5xl px-8 py-8">
        {link}
        <BackButton label="Apps" onClick={() => setPicked(null)} />
        <header className="mb-6">
          <h1 className="text-xl font-semibold tracking-tight">Install {template.name}</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Passwords and keys are generated for you. It becomes a project you can redeploy, stop and watch like any other.
          </p>
        </header>
        <AppInstall
          template={template}
          servers={servers}
          onCancel={() => setPicked(null)}
          // The project page follows the deploy that was just started.
          onInstalled={(p) => router.push(`/projects/?id=${encodeURIComponent(p.id)}`)}
        />
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      {link}
      <header className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight">Apps</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Popular self-hosted software, ready to run. No repository or compose file needed.
        </p>
      </header>

      {error && <Notice tone="error" className="mb-4">{error}</Notice>}

      <div className="mb-5 flex flex-wrap items-center gap-3">
        <div className="relative w-64">
          <Search className="absolute top-2.5 left-2.5 size-4 text-muted-foreground" />
          <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search apps…" className="pl-8" />
        </div>
        <ToggleGroup
          type="single"
          variant="outline"
          size="sm"
          value={category}
          onValueChange={(v) => v && setCategory(v)}
          className="flex-wrap"
        >
          <ToggleGroupItem value="all">All</ToggleGroupItem>
          {categories.map((c) => (
            <ToggleGroupItem key={c} value={c}>
              {c}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>

      {catalog === null ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" /> Loading…
        </p>
      ) : shown.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">No apps match.</p>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {shown.map((t) => (
            <li key={t.id}>
              <Button
                variant="outline"
                onClick={() => setPicked(t.id)}
                className="group h-full w-full flex-col items-stretch gap-3 bg-card p-4 text-left font-normal whitespace-normal hover:border-primary/60 hover:bg-card dark:bg-card dark:hover:bg-card"
              >
                <span className="flex items-center gap-3">
                  <AppIcon id={t.id} />
                  <span className="min-w-0">
                    <span className="block truncate font-medium">{t.name}</span>
                    <span className="block text-xs text-muted-foreground">{t.category}</span>
                  </span>
                </span>
                <span className="flex-1 text-[13px] leading-snug text-muted-foreground">{t.tagline}</span>
                <span className="flex items-center justify-between gap-2">
                  {t.wantsDomain ? (
                    <Badge variant="outline" className="text-[10px] text-muted-foreground">
                      <Globe /> Best with a domain
                    </Badge>
                  ) : (
                    <span />
                  )}
                  <span className="flex items-center gap-1 text-xs font-medium text-primary">
                    Install <ArrowRight className="size-3.5 transition-transform group-hover:translate-x-0.5" />
                  </span>
                </span>
              </Button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
