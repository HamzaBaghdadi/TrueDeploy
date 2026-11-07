import {
  Activity,
  ChartColumn,
  Container,
  DatabaseZap,
  GitBranch,
  Ghost,
  HardDrive,
  KeyRound,
  LayoutGrid,
  Newspaper,
  Workflow,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";

const icons: Record<string, LucideIcon> = {
  "uptime-kuma": Activity,
  n8n: Workflow,
  wordpress: Newspaper,
  ghost: Ghost,
  gitea: GitBranch,
  vaultwarden: KeyRound,
  metabase: ChartColumn,
  portainer: Container,
  adminer: DatabaseZap,
  seaweedfs: HardDrive,
};

/** The tile shown for a one-click app (by catalog ID). */
export function AppIcon({ id, className }: { id: string; className?: string }) {
  const Icon = icons[id] ?? LayoutGrid;
  return (
    <span
      className={cn(
        "flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary ring-1 ring-primary/20",
        className,
      )}
    >
      <Icon className="size-5" />
    </span>
  );
}
