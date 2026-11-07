"use client";

import { ExternalLink, Loader2, Plug, Plus, Unplug } from "lucide-react";
import { useEffect, useState } from "react";
import { api, type GitAccountView } from "@/lib/api";
import { useConfirm } from "@/components/confirm-dialog";
import { GIT_LABELS, GitProviderIcon } from "@/components/git-provider-icon";
import { Notice } from "@/components/notice";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

type Kind = "github" | "gitlab" | "bitbucket" | "gitea";

/** Where to create a token, and what it needs, per provider. */
function tokenHelp(kind: Kind, base: string): { url: string; link: string; needs: React.ReactNode; placeholder: string } {
  const web = (base.trim() ? (base.includes("://") ? base.trim() : "https://" + base.trim()) : "").replace(/\/+$/, "");
  switch (kind) {
    case "gitlab":
      return {
        url: `${web || "https://gitlab.com"}/-/user_settings/personal_access_tokens?name=TrueDeploy&scopes=api`,
        link: "Create a GitLab access token",
        needs: (
          <>
            the <Mono>api</Mono> scope is pre-selected. It covers repositories, deploy keys and CI/CD variables.
          </>
        ),
        placeholder: "glpat-…",
      };
    case "bitbucket":
      return {
        url: "https://id.atlassian.com/manage-profile/security/api-tokens",
        link: "Create an Atlassian API token for Bitbucket",
        needs: (
          <>
            give it read and write access to repositories, read and write to pipelines, and read to your account and workspaces. Sign in
            with your Atlassian email. An app password works too, with your Bitbucket username.
          </>
        ),
        placeholder: "ATATT… or app password",
      };
    case "gitea":
      return {
        url: `${web || "https://codeberg.org"}/user/settings/applications`,
        link: "Create an access token",
        needs: (
          <>
            under Generate new token, choose <Mono>repository</Mono>: read and write, <Mono>organization</Mono>: read (write to create
            repos in organizations) and <Mono>user</Mono>: read. Auto-deploy needs Actions turned on for the repository.
          </>
        ),
        placeholder: "Access token",
      };
  }
  return {
    url: "https://github.com/settings/tokens/new?scopes=repo,read:org,workflow&description=TrueDeploy",
    link: "Create a GitHub token",
    needs: (
      <>
        the <Mono>repo</Mono>, <Mono>read:org</Mono> and <Mono>workflow</Mono> scopes are pre-selected.
      </>
    ),
    placeholder: "ghp_…",
  };
}

function Mono({ children }: { children: React.ReactNode }) {
  return <span className="font-mono text-foreground">{children}</span>;
}

/** Settings card: connected Git hosts and a form to connect another. */
export function GitProvidersCard() {
  const confirm = useConfirm();
  const [accounts, setAccounts] = useState<GitAccountView[] | null>(null);
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = () => api.gitAccounts().then(setAccounts, (e: Error) => setError(e.message));
  useEffect(() => {
    void load();
  }, []);

  async function disconnect(a: GitAccountView) {
    const ok = await confirm({
      title: `Disconnect ${a.label} (${a.host})?`,
      message:
        "TrueDeploy forgets the token on this computer. Projects keep deploying (the server has its own deploy key); you just can't pick repos, add deploy keys or change auto-deploy for this host until you reconnect. " +
        `The token stays valid on ${a.label} until you revoke it there.`,
      confirmLabel: "Disconnect",
      tone: "danger",
    });
    if (!ok) return;
    try {
      await api.gitDisconnect(a.host);
      await load();
    } catch (e) {
      setError((e as Error).message);
    }
  }

  const empty = accounts !== null && accounts.length === 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Git providers</CardTitle>
        <CardDescription>
          Pick or create repositories, add deploy keys and turn on auto-deploy. GitHub, GitLab (cloud or self-hosted), Bitbucket, and
          Gitea or Forgejo (Codeberg or self-hosted).
        </CardDescription>
        {!empty && !adding && accounts !== null && (
          <CardAction>
            <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
              <Plus /> Connect another
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        {accounts === null ? (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> Loading…
          </p>
        ) : (
          accounts.length > 0 && (
            <ul className="divide-y divide-border rounded-lg border border-border">
              {accounts.map((a) => (
                <li key={a.host} className="flex items-center gap-3 px-3 py-2.5">
                  {a.avatarUrl ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img src={a.avatarUrl} alt="" className="size-8 rounded-full border border-border" />
                  ) : (
                    <div className="grid size-8 place-items-center rounded-full bg-muted text-sm">{a.login?.[0]?.toUpperCase()}</div>
                  )}
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5 text-sm font-medium">
                      <GitProviderIcon kind={a.kind} className="size-4" />
                      {a.label}
                      <span className="truncate font-mono text-xs font-normal text-muted-foreground">{a.host}</span>
                    </div>
                    <div className="truncate text-xs text-muted-foreground">
                      {a.name ? `${a.name} · ` : ""}@{a.login}
                    </div>
                  </div>
                  <Button variant="ghost" size="sm" onClick={() => void disconnect(a)} className="text-muted-foreground hover:text-destructive">
                    <Unplug /> Disconnect
                  </Button>
                </li>
              ))}
            </ul>
          )
        )}
        {(empty || adding) && (
          <ConnectForm
            taken={accounts?.map((a) => a.host) ?? []}
            onCancel={empty ? undefined : () => setAdding(false)}
            onConnected={async () => {
              setAdding(false);
              await load();
            }}
          />
        )}
        {error && <Notice tone="error">{error}</Notice>}
      </CardContent>
    </Card>
  );
}

function ConnectForm({ taken, onCancel, onConnected }: { taken: string[]; onCancel?: () => void; onConnected: () => void }) {
  const [kind, setKind] = useState<Kind>(taken.includes("github.com") ? "gitlab" : "github");
  const [baseUrl, setBaseUrl] = useState("");
  const [username, setUsername] = useState("");
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const help = tokenHelp(kind, baseUrl);
  const needsBase = kind === "gitlab" || kind === "gitea";

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.gitConnect({ kind, baseUrl: needsBase ? baseUrl : "", username: kind === "bitbucket" ? username : "", token });
      setToken("");
      onConnected();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="space-y-4 rounded-lg border border-border bg-background/40 p-4">
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label>Provider</Label>
          <Select value={kind} onValueChange={(v) => setKind(v as Kind)}>
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(Object.keys(GIT_LABELS) as Kind[]).map((k) => (
                <SelectItem key={k} value={k}>
                  <GitProviderIcon kind={k} className="size-4" /> {GIT_LABELS[k]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {needsBase && (
          <div className="space-y-1.5">
            <Label htmlFor="git-base">{kind === "gitlab" ? "Address (self-hosted only)" : "Address"}</Label>
            <Input
              id="git-base"
              className="font-mono"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
              placeholder={kind === "gitlab" ? "gitlab.com" : "https://codeberg.org"}
              spellCheck={false}
            />
          </div>
        )}
        {kind === "bitbucket" && (
          <div className="space-y-1.5">
            <Label htmlFor="git-user">Atlassian email or username</Label>
            <Input id="git-user" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" spellCheck={false} />
          </div>
        )}
      </div>
      <p className="text-[13px] text-muted-foreground">
        <Button type="button" variant="link" size="xs" className="h-auto p-0" onClick={() => api.openURL(help.url)}>
          {help.link} <ExternalLink />
        </Button>
        : {help.needs} It&apos;s stored encrypted on this computer only.
      </p>
      <div className="flex gap-2">
        <Input
          type="password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder={help.placeholder}
          spellCheck={false}
          autoComplete="off"
          aria-label={`${GIT_LABELS[kind]} token`}
          className="flex-1 font-mono"
        />
        {onCancel && (
          <Button type="button" variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
        )}
        <Button type="submit" disabled={busy || !token.trim() || (kind === "gitea" && !baseUrl.trim()) || (kind === "bitbucket" && !username.trim())}>
          {busy ? <Loader2 className="animate-spin" /> : <Plug />}
          {busy ? "Checking…" : "Connect"}
        </Button>
      </div>
      {error && <Notice tone="error">{error}</Notice>}
    </form>
  );
}
