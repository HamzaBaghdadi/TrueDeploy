"use client";

import { ChevronRight, LayoutDashboard, Loader2, Plus, RefreshCw, Settings2, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { api, type Server, type ServerInfo } from "@/lib/api";
import { AddServerForm } from "@/components/add-server-form";
import { ServerInfoPanel } from "@/components/server-info";
import { ServerSetup } from "@/components/server-setup";
import { ServerDashboard } from "@/components/server-dashboard";
import { DeepLink } from "@/components/deep-link";
import { GettingStarted } from "@/components/getting-started";
import { useConfirm } from "@/components/confirm-dialog";
import { Notice } from "@/components/notice";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

type Check = { busy: boolean; info?: ServerInfo; error?: string };

export default function ServersPage() {
  const [servers, setServers] = useState<Server[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [settingUp, setSettingUp] = useState<Server | null>(null);
  const [viewing, setViewing] = useState<Server | null>(null);
  const [checks, setChecks] = useState<Record<string, Check>>({});
  const confirm = useConfirm();

  const load = useCallback(async () => {
    try {
      setServers(await api.listServers());
      setLoadError(null);
    } catch (e) {
      setLoadError((e as Error).message);
      setServers([]);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function check(id: string) {
    setChecks((c) => ({ ...c, [id]: { busy: true } }));
    try {
      const info = await api.testSavedServer(id);
      setChecks((c) => ({ ...c, [id]: { busy: false, info } }));
    } catch (e) {
      setChecks((c) => ({ ...c, [id]: { busy: false, error: (e as Error).message } }));
    }
  }

  async function remove(s: Server) {
    const ok = await confirm({
      title: `Remove ${s.name}?`,
      message: "The server and its saved credentials will be deleted from this computer. Nothing on the server changes.",
      confirmLabel: "Remove server",
      tone: "danger",
    });
    if (!ok) return;
    await api.deleteServer(s.id);
    void load();
  }

  // /?id=<server>&view=dashboard|setup opens a server; /?add=1 opens the form.
  // A link can arrive before the list has loaded, so it waits in `pending`.
  const [pending, setPending] = useState<{ id: string; view: string } | null>(null);
  useEffect(() => {
    if (!pending || !servers) return;
    const s = servers.find((x) => x.id === pending.id);
    const t = setTimeout(() => {
      setPending(null);
      if (!s) return;
      setAdding(false);
      if (pending.view === "setup") {
        setViewing(null);
        setSettingUp(s);
      } else {
        setSettingUp(null);
        setViewing(s);
      }
    }, 0);
    return () => clearTimeout(t);
  }, [pending, servers]);

  const link = (
    <DeepLink
      onLink={(p) => {
        const id = p.get("id");
        if (id) setPending({ id, view: p.get("view") ?? "dashboard" });
        else if (p.has("add")) {
          setViewing(null);
          setSettingUp(null);
          setAdding(true);
        }
      }}
    />
  );

  if (settingUp)
    return (
      <>
        {link}
        <ServerSetup key={settingUp.id} server={settingUp} onBack={() => setSettingUp(null)} />
      </>
    );
  if (viewing)
    return (
      <>
        {link}
        <ServerDashboard
          key={viewing.id}
          server={viewing}
          onBack={() => setViewing(null)}
          onSetup={() => {
            setSettingUp(viewing);
            setViewing(null);
          }}
        />
      </>
    );

  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      {link}
      <header className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">{adding ? "Add server" : "Servers"}</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {adding
              ? "Connect over SSH. Credentials are encrypted and stored in your OS keychain."
              : "Machines TrueDeploy can set up and deploy to."}
          </p>
        </div>
        {!adding && (
          <Button onClick={() => setAdding(true)}>
            <Plus />
            Add server
          </Button>
        )}
      </header>

      {loadError && !adding && (
        <Notice tone="error" className="mb-4">
          {loadError}
        </Notice>
      )}

      {!adding && servers !== null && <GettingStarted servers={servers} />}

      {adding ? (
        <AddServerForm
          onCancel={() => setAdding(false)}
          onSaved={() => {
            setAdding(false);
            void load();
          }}
        />
      ) : servers === null ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" />
          Loading…
        </p>
      ) : servers.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-12 text-center">
          <p className="text-sm text-muted-foreground">No servers yet.</p>
          <Button variant="link" onClick={() => setAdding(true)} className="mt-2">
            Add your first server
            <ChevronRight />
          </Button>
        </div>
      ) : (
        <ul className="space-y-3">
          {servers.map((s) => {
            const c = checks[s.id];
            return (
              <li key={s.id}>
                <Card className="gap-0 px-4 py-3">
                  <div className="flex items-center gap-4">
                    <span
                      className={`size-2 shrink-0 rounded-full ${
                        c?.info ? "bg-success" : c?.error ? "bg-destructive" : "bg-muted-foreground/40"
                      }`}
                    />
                    <Button
                      variant="ghost"
                      onClick={() => setViewing(s)}
                      className="group -ml-2 h-auto min-w-0 flex-1 flex-col items-start justify-start gap-0 px-2 py-1 text-left whitespace-normal hover:bg-transparent"
                    >
                      <span className="flex items-center gap-1 font-medium group-hover:text-primary">
                        {s.name}
                        <ChevronRight className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-primary" />
                      </span>
                      <span className="font-mono text-xs font-normal text-muted-foreground">
                        {s.user}@{s.host}:{s.port} · {s.auth === "key" ? "SSH key" : "password"}
                      </span>
                    </Button>
                    <Button size="sm" onClick={() => setViewing(s)}>
                      <LayoutDashboard />
                      Dashboard
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => setSettingUp(s)}>
                      <Settings2 />
                      Set up
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => check(s.id)} disabled={c?.busy}>
                      <RefreshCw className={c?.busy ? "animate-spin" : ""} />
                      {c?.busy ? "Checking…" : "Check"}
                    </Button>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button
                          size="icon-sm"
                          variant="ghost"
                          onClick={() => remove(s)}
                          aria-label={`Remove ${s.name}`}
                          className="text-muted-foreground hover:text-destructive"
                        >
                          <Trash2 />
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>Remove server</TooltipContent>
                    </Tooltip>
                  </div>
                  {c?.error && (
                    <Notice tone="error" className="mt-3">
                      {c.error}
                    </Notice>
                  )}
                  {c?.info && (
                    <div className="mt-3 border-t border-border pt-2">
                      <ServerInfoPanel info={c.info} />
                    </div>
                  )}
                </Card>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
