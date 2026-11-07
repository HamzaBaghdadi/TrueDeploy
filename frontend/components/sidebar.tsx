"use client";

import {
  Bell,
  Braces,
  CircleArrowUp,
  Database,
  FolderGit2,
  House,
  LayoutGrid,
  Rocket,
  Search,
  Server,
  Settings,
  SquareTerminal,
  type LucideIcon,
} from "lucide-react";
import Image from "next/image";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";
import { useMonitorStatus } from "@/lib/alerts";
import { downloadUpdate, useUpdateStatus } from "@/lib/updates";
import { openCommandPalette } from "@/components/command-palette";
import { Kbd } from "@/components/ui/kbd";
import { Button } from "@/components/ui/button";

const nav: { label: string; href: string; icon: LucideIcon }[] = [
  { label: "Overview", href: "/overview/", icon: House },
  { label: "Servers", href: "/", icon: Server },
  { label: "Projects", href: "/projects/", icon: FolderGit2 },
  { label: "Apps", href: "/apps/", icon: LayoutGrid },
  { label: "Databases", href: "/databases/", icon: Database },
  { label: "Variables", href: "/variables/", icon: Braces },
  { label: "Deployments", href: "/deployments/", icon: Rocket },
  { label: "Terminal", href: "/terminal/", icon: SquareTerminal },
  { label: "Alerts", href: "/alerts/", icon: Bell },
  { label: "Settings", href: "/settings/", icon: Settings },
];

export function Sidebar() {
  const path = usePathname();
  const monitor = useMonitorStatus();
  const update = useUpdateStatus();
  const [version, setVersion] = useState("");
  useEffect(() => {
    api.appInfo().then((i) => setVersion(i.version), () => {});
  }, []);
  const alertCount = monitor?.alerts?.length ?? 0;
  const critical = monitor?.alerts?.some((a) => a.severity === "critical");
  const isActive = (href: string) => (href === "/" ? path === "/" : path.startsWith(href.replace(/\/$/, "")));

  return (
    <aside className="flex w-56 shrink-0 flex-col border-r border-sidebar-border bg-sidebar">
      <div className="flex items-center gap-2.5 px-4 py-4">
        <Image src="/logo.png" alt="" width={36} height={36} priority className="size-9 drop-shadow-[0_2px_8px_rgba(17,137,235,0.35)]" />
        <span className="text-[15px] font-semibold tracking-tight">
          True<span className="text-brand-gradient">Deploy</span>
        </span>
      </div>
      <div className="px-3 pb-3">
        <Button
          variant="outline"
          onClick={openCommandPalette}
          className="w-full justify-start gap-2 font-normal text-muted-foreground hover:text-foreground"
        >
          <Search />
          <span className="flex-1 text-left">Search…</span>
          <Kbd>Ctrl K</Kbd>
        </Button>
      </div>
      <nav className="flex flex-col gap-0.5 px-3">
        {nav.map(({ label, href, icon: Icon }) => (
          <Link
            key={label}
            href={href}
            className={cn(
              "relative flex items-center gap-2.5 rounded-md px-3 py-2 text-sm transition-colors",
              isActive(href)
                ? "bg-sidebar-accent text-sidebar-accent-foreground"
                : "text-muted-foreground hover:bg-sidebar-accent/50 hover:text-foreground",
            )}
          >
            {isActive(href) && <span className="bg-brand-gradient absolute top-1.5 bottom-1.5 left-0 w-0.5 rounded-full" />}
            <Icon className={cn("size-4", isActive(href) && "text-brand-cyan")} />
            {label}
            {href === "/alerts/" && alertCount > 0 && (
              <span
                className={cn(
                  "ml-auto min-w-5 rounded-full px-1.5 text-center text-[11px] leading-5 font-semibold",
                  critical ? "bg-destructive text-white" : "bg-warning text-black",
                )}
              >
                {alertCount}
              </span>
            )}
          </Link>
        ))}
      </nav>
      <div className="mt-auto space-y-2 px-3 py-4">
        {update?.available && update.latest && (
          <Button size="sm" className="w-full justify-start" onClick={() => downloadUpdate(update)}>
            <CircleArrowUp />
            Update to v{update.latest.version}
          </Button>
        )}
        <div className="px-2 font-mono text-[11px] text-muted-foreground">{version && `v${version}`}</div>
      </div>
    </aside>
  );
}
