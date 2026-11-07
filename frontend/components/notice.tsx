import { CircleAlert, CircleCheck, Info, TriangleAlert } from "lucide-react";
import { cn } from "@/lib/utils";

type Tone = "success" | "error" | "warning" | "info";

const styles: Record<Tone, string> = {
  success: "border-success/25 bg-success/10 text-success",
  error: "border-destructive/25 bg-destructive/10 text-destructive",
  warning: "border-warning/25 bg-warning/5 text-warning",
  info: "border-border bg-muted text-foreground",
};
const icons = { success: CircleCheck, error: CircleAlert, warning: TriangleAlert, info: Info };

/** Inline message (result, error, warning) with an icon. Text is selectable. */
export function Notice({
  tone,
  children,
  className,
  action,
}: {
  tone: Tone;
  children: React.ReactNode;
  className?: string;
  action?: React.ReactNode;
}) {
  const Icon = icons[tone];
  return (
    <div role={tone === "error" ? "alert" : "status"} className={cn("flex items-start gap-2.5 rounded-lg border px-3 py-2.5 text-[13px]", styles[tone], className)}>
      <Icon className="mt-px size-4 shrink-0" />
      <div className="selectable min-w-0 flex-1 leading-relaxed">{children}</div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  );
}
