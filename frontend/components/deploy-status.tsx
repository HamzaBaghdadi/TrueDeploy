import { Ban, CircleCheck, CircleDot, CircleX, Loader2, type LucideIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

const config: Record<string, { label: string; className: string; icon: LucideIcon }> = {
  success: { label: "Live", className: "border-success/25 bg-success/10 text-success", icon: CircleCheck },
  running: { label: "Deploying", className: "border-info/25 bg-info/10 text-info", icon: Loader2 },
  failed: { label: "Failed", className: "border-destructive/25 bg-destructive/10 text-destructive", icon: CircleX },
  cancelled: { label: "Cancelled", className: "border-border bg-muted text-muted-foreground", icon: Ban },
};

/** `live` marks the deploy that is currently running on the server; other
 * successful deploys are shown as "Deployed". */
export function DeployStatus({ status, live = true }: { status: string; live?: boolean }) {
  const c = config[status] ?? config.cancelled;
  const deployed = status === "success" && !live;
  const Icon = deployed ? CircleDot : c.icon;
  return (
    <Badge variant="outline" className={cn("gap-1 font-medium", deployed ? "border-border bg-muted text-muted-foreground" : c.className)}>
      <Icon className={cn("size-3", status === "running" && "animate-spin")} />
      {deployed ? "Deployed" : (config[status]?.label ?? status)}
    </Badge>
  );
}
