import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

const engineColor: Record<string, string> = {
  postgres: "border-sky-400/25 bg-sky-400/10 text-sky-300",
  mysql: "border-orange-400/25 bg-orange-400/10 text-orange-300",
  redis: "border-red-400/25 bg-red-400/10 text-red-300",
  mongo: "border-emerald-400/25 bg-emerald-400/10 text-emerald-300",
};

export function EngineBadge({ engine, name }: { engine: string; name: string }) {
  return (
    <Badge variant="outline" className={cn("font-medium", engineColor[engine])}>
      {name}
    </Badge>
  );
}
