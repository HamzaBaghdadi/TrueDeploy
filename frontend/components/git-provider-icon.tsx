import { GitHubMark } from "@/components/github-mark";
import { cn } from "@/lib/utils";

const BADGES: Record<string, { letter: string; className: string }> = {
  gitlab: { letter: "GL", className: "bg-[#fc6d26]/15 text-[#fc6d26]" },
  bitbucket: { letter: "BB", className: "bg-[#2684ff]/15 text-[#4c9aff]" },
  gitea: { letter: "Gt", className: "bg-[#609926]/15 text-[#7cbf3a]" },
};

/** A small mark for a Git provider: GitHub's logo, a lettered badge for the others. */
export function GitProviderIcon({ kind, className = "size-5" }: { kind?: string; className?: string }) {
  if (!kind || kind === "github") return <GitHubMark className={className} />;
  const b = BADGES[kind] ?? { letter: "G", className: "bg-muted text-muted-foreground" };
  return (
    <span
      aria-hidden
      className={cn("inline-grid shrink-0 place-items-center rounded-[5px] font-mono text-[9px] leading-none font-bold tracking-tight", b.className, className)}
    >
      {b.letter}
    </span>
  );
}

export const GIT_LABELS: Record<string, string> = { github: "GitHub", gitlab: "GitLab", bitbucket: "Bitbucket", gitea: "Gitea / Forgejo" };
