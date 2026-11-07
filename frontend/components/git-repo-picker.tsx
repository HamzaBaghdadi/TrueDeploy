"use client";

import { ArrowRight, Loader2, Lock, Plus, Search, X } from "lucide-react";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { api, type GitAccountView, type GitRepo } from "@/lib/api";
import { GitProviderIcon } from "@/components/git-provider-icon";
import { Notice } from "@/components/notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { timeAgo } from "@/lib/format";

/** Lets the user pick a repository on any connected Git host, or create one. */
export function GitRepoPicker({
  suggestedName,
  onPicked,
  onClose,
}: {
  suggestedName: string;
  onPicked: (repo: GitRepo) => void;
  onClose: () => void;
}) {
  const [accounts, setAccounts] = useState<GitAccountView[] | null>(null);
  const [host, setHost] = useState("");

  useEffect(() => {
    api.gitAccounts().then(
      (a) => {
        setAccounts(a);
        if (a.length) setHost(a[0].host);
      },
      () => setAccounts([]),
    );
  }, []);

  const account = accounts?.find((a) => a.host === host);
  const close = (
    <Button type="button" variant="ghost" size="icon-xs" onClick={onClose} aria-label="Close" className="ml-auto text-muted-foreground">
      <X />
    </Button>
  );

  if (!accounts || !account) {
    return (
      <div className="mt-3 rounded-lg border border-border bg-background">
        <div className="flex items-center gap-2 border-b border-border px-3 py-2">
          <span className="text-sm text-muted-foreground">Repositories</span>
          {close}
        </div>
        <div className="p-3">
          {accounts === null ? (
            <p className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" /> Loading…
            </p>
          ) : (
            <p className="flex flex-wrap items-center gap-1 text-sm text-muted-foreground">
              Connect GitHub, GitLab, Bitbucket or Gitea to pick or create repositories here.
              <Button asChild variant="link" size="xs">
                <Link href="/settings/">
                  Open Settings <ArrowRight />
                </Link>
              </Button>
            </p>
          )}
        </div>
      </div>
    );
  }

  return (
    <Tabs defaultValue="choose" className="mt-3 gap-0 rounded-lg border border-border bg-background">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
        {accounts.length > 1 ? (
          <Select value={host} onValueChange={setHost}>
            <SelectTrigger size="sm" className="w-auto min-w-40" aria-label="Git host">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {accounts.map((a) => (
                <SelectItem key={a.host} value={a.host}>
                  <GitProviderIcon kind={a.kind} className="size-4" /> {a.host}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <GitProviderIcon kind={account.kind} className="size-4" />
        )}
        <TabsList>
          <TabsTrigger value="choose">
            <Search /> Choose a repository
          </TabsTrigger>
          <TabsTrigger value="create">
            <Plus /> Create a new one
          </TabsTrigger>
        </TabsList>
        {close}
      </div>
      {/* Keyed by host so switching accounts reloads the lists. */}
      <TabsContent value="choose" className="p-3">
        <Choose key={host} host={host} onPicked={onPicked} />
      </TabsContent>
      <TabsContent value="create" className="p-3">
        <Create key={host} account={account} suggestedName={suggestedName} onCreated={onPicked} />
      </TabsContent>
    </Tabs>
  );
}

function Choose({ host, onPicked }: { host: string; onPicked: (r: GitRepo) => void }) {
  const [repos, setRepos] = useState<GitRepo[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");

  useEffect(() => {
    api.gitRepos(host).then(setRepos, (e: Error) => setError(e.message));
  }, [host]);

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (repos ?? []).filter((r) => !q || r.fullName.toLowerCase().includes(q)).slice(0, 100);
  }, [repos, query]);

  if (error) return <Notice tone="error">{error}</Notice>;
  if (!repos)
    return (
      <p className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" /> Loading your repositories…
      </p>
    );

  return (
    <>
      <div className="relative">
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input className="pl-8" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search repositories…" autoFocus />
      </div>
      <ul className="mt-2 max-h-72 divide-y divide-border overflow-auto rounded-md border border-border">
        {shown.length === 0 && <li className="p-3 text-sm text-muted-foreground">No matching repositories.</li>}
        {shown.map((r) => (
          <li key={r.fullName}>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onPicked(r)}
              className="h-auto w-full justify-start gap-3 rounded-none px-3 py-2 text-left font-normal whitespace-normal"
            >
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm">{r.fullName}</span>
                {r.description && <span className="block truncate text-xs text-muted-foreground">{r.description}</span>}
              </span>
              {r.private && (
                <Badge variant="secondary">
                  <Lock /> private
                </Badge>
              )}
              <span className="shrink-0 text-xs text-muted-foreground">{timeAgo(r.pushedAt)}</span>
            </Button>
          </li>
        ))}
      </ul>
    </>
  );
}

function Create({ account, suggestedName, onCreated }: { account: GitAccountView; suggestedName: string; onCreated: (r: GitRepo) => void }) {
  const [owners, setOwners] = useState<string[]>([account.login]);
  const [owner, setOwner] = useState(account.login);
  const [name, setName] = useState(suggestedName);
  const [description, setDescription] = useState("");
  const [isPrivate, setPrivate] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.gitOwners(account.host).then((o) => {
      if (!o.length) return;
      setOwners(o);
      setOwner((cur) => (o.includes(cur) ? cur : o[0]));
    }, () => {});
  }, [account.host]);

  async function create() {
    setBusy(true);
    setError(null);
    try {
      onCreated(await api.gitCreateRepo(account.host, { owner, name, description, private: isPrivate }));
    } catch (e) {
      setError((e as Error).message);
      setBusy(false);
    }
  }

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,2fr)] items-center gap-2">
        <Select value={owner} onValueChange={setOwner}>
          <SelectTrigger className="w-full" aria-label={account.kind === "bitbucket" ? "Workspace" : "Owner"}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {owners.map((o) => (
              <SelectItem key={o} value={o}>
                {o}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <span className="text-muted-foreground">/</span>
        <Input className="font-mono" value={name} onChange={(e) => setName(e.target.value)} placeholder="repository-name" spellCheck={false} />
      </div>
      <Input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Description (optional)" />
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Checkbox id="repo-private" checked={isPrivate} onCheckedChange={(v) => setPrivate(v === true)} />
          <Label htmlFor="repo-private">Private repository</Label>
        </div>
        <Button type="button" onClick={create} disabled={busy || !name.trim()}>
          {busy ? <Loader2 className="animate-spin" /> : <Plus />}
          {busy ? "Creating…" : "Create repository"}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        {account.kind === "bitbucket"
          ? "Bitbucket creates it empty: push a first commit before the first deploy."
          : "Created with a README, so the default branch exists and can be cloned right away."}
      </p>
      {error && <Notice tone="error">{error}</Notice>}
    </div>
  );
}
