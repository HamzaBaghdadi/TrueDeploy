"use client";

import { usePathname } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, type Server } from "@/lib/api";
import { TerminalPane } from "@/components/terminal-pane";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Plus, RotateCw, Server as ServerIcon, SquareTerminal, X } from "lucide-react";

type Tab = {
  key: number;
  serverId: string;
  container?: string; // a shell inside this container
  title: string;
  state: "connecting" | "open" | "closed";
  connectKey: number;
};

/**
 * Lives in the root layout so terminals keep running while you use other
 * pages; it is only visible on /terminal.
 */
export function TerminalHost() {
  const path = usePathname();
  const visible = path.startsWith("/terminal");
  const [tabs, setTabs] = useState<Tab[]>([]);
  const [activeKey, setActiveKey] = useState<number | null>(null);
  const [servers, setServers] = useState<Server[]>([]);
  const nextKey = useRef(1);

  const open = useCallback((s: Server, container?: string) => {
    const key = nextKey.current++;
    const base = container ? `${container} (${s.name})` : s.name;
    const same = (t: Tab) => t.serverId === s.id && t.container === container;
    setTabs((ts) => {
      const n = ts.filter(same).length;
      return [...ts, { key, serverId: s.id, container, title: n ? `${base} (${n + 1})` : base, state: "connecting", connectKey: 0 }];
    });
    setActiveKey(key);
  }, []);

  // Load servers when shown; /terminal/?server=<id> opens a tab for it.
  useEffect(() => {
    if (!visible) return;
    let cancelled = false;
    api.listServers().then((list) => {
      if (cancelled) return;
      setServers(list);
      const params = new URLSearchParams(window.location.search);
      const id = params.get("server");
      if (id) {
        window.history.replaceState(null, "", "/terminal/");
        const s = list.find((x) => x.id === id);
        if (s) open(s, params.get("container") ?? undefined);
      }
    }, () => {});
    return () => {
      cancelled = true;
    };
  }, [visible, path, open]);

  function close(key: number) {
    setTabs((ts) => {
      const rest = ts.filter((t) => t.key !== key);
      if (activeKey === key) setActiveKey(rest.at(-1)?.key ?? null);
      return rest;
    });
  }

  const setState = (key: number, state: Tab["state"]) =>
    setTabs((ts) => ts.map((t) => (t.key === key && t.state !== state ? { ...t, state } : t)));

  return (
    <div className={visible ? "absolute inset-0 flex flex-col bg-terminal" : "hidden"}>
      <div className="flex shrink-0 items-center gap-1 border-b border-border bg-surface px-2 pt-2">
        {tabs.map((t) => (
          <div
            key={t.key}
            className={`group flex items-center gap-2 rounded-t-md border border-b-0 px-3 py-1.5 text-sm ${
              t.key === activeKey ? "border-border bg-terminal text-foreground" : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            <Button variant="ghost" size="xs" onClick={() => setActiveKey(t.key)} className="gap-2 px-1 hover:bg-transparent">
              <span
                className={`size-1.5 rounded-full ${
                  t.state === "open" ? "bg-success" : t.state === "connecting" ? "bg-warning animate-pulse" : "bg-muted-foreground/50"
                }`}
              />
              {t.title}
            </Button>
            <Button
              variant="ghost"
              size="icon-xs"
              onClick={() => close(t.key)}
              className="text-muted-foreground opacity-60 hover:text-destructive hover:opacity-100"
              aria-label="Close tab"
            >
              <X />
            </Button>
          </div>
        ))}
        <DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon-sm" className="mb-1 text-muted-foreground" aria-label="New terminal">
                  <Plus />
                </Button>
              </DropdownMenuTrigger>
            </TooltipTrigger>
            <TooltipContent>New terminal</TooltipContent>
          </Tooltip>
          <DropdownMenuContent align="start" className="min-w-64">
            <DropdownMenuLabel>Open a shell on</DropdownMenuLabel>
            {servers.length === 0 && <DropdownMenuItem disabled>No servers yet</DropdownMenuItem>}
            {servers.map((s) => (
              <DropdownMenuItem key={s.id} onSelect={() => open(s)}>
                <ServerIcon />
                <span>{s.name}</span>
                <span className="ml-auto font-mono text-xs text-muted-foreground">
                  {s.user}@{s.host}
                </span>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        {tabs.find((t) => t.key === activeKey)?.state === "closed" && (
          <Button
            size="xs"
            className="mb-1 ml-auto"
            onClick={() =>
              setTabs((ts) => ts.map((t) => (t.key === activeKey ? { ...t, connectKey: t.connectKey + 1, state: "connecting" } : t)))
            }
          >
            <RotateCw />
            Reconnect
          </Button>
        )}
      </div>

      <div className="relative min-h-0 flex-1">
        {tabs.map((t) => (
          <div key={t.key} className={t.key === activeKey ? "absolute inset-0" : "hidden"}>
            <TerminalPane
              serverId={t.serverId}
              container={t.container}
              active={visible && t.key === activeKey}
              connectKey={t.connectKey}
              onState={(s) => setState(t.key, s)}
            />
          </div>
        ))}
        {tabs.length === 0 && (
          <div className="grid h-full place-items-center">
            <div className="text-center">
              <SquareTerminal className="mx-auto mb-3 size-10 text-muted-foreground/60" />
              <p className="mb-4 text-sm text-muted-foreground">Open a shell on one of your servers.</p>
              <div className="flex flex-wrap justify-center gap-2">
                {servers.map((s) => (
                  <Button key={s.id} variant="outline" onClick={() => open(s)}>
                    <ServerIcon />
                    {s.name}
                  </Button>
                ))}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
