"use client";

import {
  Bell,
  Braces,
  Database,
  FolderGit2,
  House,
  LayoutGrid,
  LayoutDashboard,
  Plus,
  RefreshCw,
  Rocket,
  Server,
  Settings,
  ScanSearch,
  Settings2,
  SquareTerminal,
  type LucideIcon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { api, type DatabaseView, type ProjectView, type Server as ServerT, type Template } from "@/lib/api";
import { AppIcon } from "@/components/app-icon";
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut,
} from "@/components/ui/command";

const OPEN_EVENT = "td:command-palette";

/** Opens the palette from anywhere (e.g. the sidebar search button). */
export function openCommandPalette() {
  window.dispatchEvent(new Event(OPEN_EVENT));
}

type Data = { servers: ServerT[]; projects: ProjectView[]; databases: DatabaseView[]; apps: Template[] };

export function CommandPalette() {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [data, setData] = useState<Data>({ servers: [], projects: [], databases: [], apps: [] });

  // Ctrl+K / Cmd+K anywhere, plus the sidebar button.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        // The terminal has its own Ctrl+K (clear line); leave it alone there.
        if ((e.target as HTMLElement | null)?.closest?.(".xterm")) return;
        e.preventDefault();
        setOpen((o) => !o);
      }
    };
    const onOpen = () => setOpen(true);
    window.addEventListener("keydown", onKey);
    window.addEventListener(OPEN_EVENT, onOpen);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener(OPEN_EVENT, onOpen);
    };
  }, []);

  // Fresh data every time it opens.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    Promise.all([api.listServers(), api.listProjects(), api.listDatabases(), api.templates()]).then(
      ([servers, projects, databases, apps]) => !cancelled && setData({ servers, projects, databases, apps }),
      () => {},
    );
    return () => {
      cancelled = true;
    };
  }, [open]);

  const go = useCallback(
    (href: string) => {
      setOpen(false);
      router.push(href);
    },
    [router],
  );

  const pages: [string, string, LucideIcon][] = [
    ["Overview", "/overview/", House],
    ["Servers", "/", Server],
    ["Projects", "/projects/", FolderGit2],
    ["Apps", "/apps/", LayoutGrid],
    ["Databases", "/databases/", Database],
    ["Variables", "/variables/", Braces],
    ["Deployments", "/deployments/", Rocket],
    ["Terminal", "/terminal/", SquareTerminal],
    ["Alerts", "/alerts/", Bell],
    ["Settings", "/settings/", Settings],
  ];
  const enc = encodeURIComponent;

  return (
    <CommandDialog open={open} onOpenChange={setOpen} title="Command palette" description="Jump to a server, project or action">
      {/* This shadcn version's CommandDialog doesn't include <Command>; the list needs it. */}
      <Command>
      <CommandInput placeholder="Search servers, projects, databases, actions…" />
      <CommandList className="max-h-[420px]">
        <CommandEmpty>Nothing matches.</CommandEmpty>

        {data.projects.length > 0 && (
          <CommandGroup heading="Projects">
            {data.projects.map((p) => (
              <CommandItem key={"p" + p.id} value={`project ${p.name} ${p.repoUrl}`} onSelect={() => go(`/projects/?id=${enc(p.id)}`)}>
                <FolderGit2 />
                <span>{p.name}</span>
                <span className="truncate text-xs text-muted-foreground">{p.serverName}</span>
              </CommandItem>
            ))}
            {data.projects.map((p) => (
              <CommandItem
                key={"d" + p.id}
                value={`deploy ${p.name}`}
                onSelect={() => {
                  // Start the deploy, then show the project (it follows the running deploy).
                  void api.deploy(p.id).catch(() => {});
                  go(`/projects/?id=${enc(p.id)}`);
                }}
              >
                <Rocket />
                <span>Deploy {p.name}</span>
              </CommandItem>
            ))}
          </CommandGroup>
        )}

        {data.servers.length > 0 && (
          <CommandGroup heading="Servers">
            {data.servers.map((s) => (
              <CommandItem key={"s" + s.id} value={`server ${s.name} ${s.host} dashboard`} onSelect={() => go(`/?id=${enc(s.id)}&view=dashboard`)}>
                <LayoutDashboard />
                <span>{s.name}</span>
                <span className="truncate font-mono text-xs text-muted-foreground">
                  {s.user}@{s.host}
                </span>
              </CommandItem>
            ))}
            {data.servers.map((s) => (
              <CommandItem key={"t" + s.id} value={`terminal shell ssh ${s.name} ${s.host}`} onSelect={() => go(`/terminal/?server=${enc(s.id)}`)}>
                <SquareTerminal />
                <span>Terminal on {s.name}</span>
              </CommandItem>
            ))}
            {data.servers.map((s) => (
              <CommandItem key={"u" + s.id} value={`set up ${s.name} ${s.host} docker firewall`} onSelect={() => go(`/?id=${enc(s.id)}&view=setup`)}>
                <Settings2 />
                <span>Set up {s.name}</span>
              </CommandItem>
            ))}
            {data.servers.map((s) => (
              <CommandItem key={"i" + s.id} value={`import existing apps databases nginx ${s.name} ${s.host}`} onSelect={() => go(`/import/?server=${enc(s.id)}`)}>
                <ScanSearch />
                <span>Import existing setup from {s.name}</span>
              </CommandItem>
            ))}
          </CommandGroup>
        )}

        {data.databases.length > 0 && (
          <CommandGroup heading="Databases">
            {data.databases.map((d) => (
              <CommandItem key={"db" + d.id} value={`database ${d.name} ${d.engineName}`} onSelect={() => go(`/databases/?id=${enc(d.id)}`)}>
                <Database />
                <span>{d.name}</span>
                <span className="text-xs text-muted-foreground">
                  {d.engineName} {d.version}
                </span>
              </CommandItem>
            ))}
          </CommandGroup>
        )}

        {data.apps.length > 0 && (
          <CommandGroup heading="Install an app">
            {data.apps.map((t) => (
              <CommandItem key={"a" + t.id} value={`install app ${t.name} ${t.category}`} onSelect={() => go(`/apps/?id=${enc(t.id)}`)}>
                <AppIcon id={t.id} className="size-5 rounded-sm ring-0 [&_svg]:size-3.5" />
                <span>Install {t.name}</span>
                <span className="truncate text-xs text-muted-foreground">{t.category}</span>
              </CommandItem>
            ))}
          </CommandGroup>
        )}

        <CommandSeparator />
        <CommandGroup heading="Create">
          <CommandItem value="add new server" onSelect={() => go("/?add=1")}>
            <Plus /> Add server
          </CommandItem>
          <CommandItem value="new project create repo" onSelect={() => go("/projects/?new=1")}>
            <Plus /> New project
          </CommandItem>
          <CommandItem value="new database create postgres mysql redis mongo" onSelect={() => go("/databases/?new=1")}>
            <Plus /> New database
          </CommandItem>
        </CommandGroup>

        <CommandGroup heading="Go to">
          {pages.map(([label, href, Icon]) => (
            <CommandItem key={href} value={`go to ${label}`} onSelect={() => go(href)}>
              <Icon /> {label}
            </CommandItem>
          ))}
          <CommandItem
            value="check servers now monitor alerts"
            onSelect={() => {
              void api.checkNow();
              go("/alerts/");
            }}
          >
            <RefreshCw /> Check servers now
            <CommandShortcut>alerts</CommandShortcut>
          </CommandItem>
        </CommandGroup>
      </CommandList>
      </Command>
    </CommandDialog>
  );
}
