"use client";

import { Boxes, Check, FileCode2, Folder, Info, Layers } from "lucide-react";
import type { DetectResult, Preset, Suggestion } from "@/lib/api";
import { Notice } from "@/components/notice";
import { Button } from "@/components/ui/button";

/** What "Detect framework" found, with a button to apply each suggestion. */
export function DetectPanel({
  result,
  presets,
  applied,
  onUse,
}: {
  result: DetectResult;
  presets: Preset[];
  applied: Suggestion | null;
  onUse: (s: Suggestion) => void;
}) {
  const list = result.suggestions ?? [];
  if (list.length === 0) {
    return (
      <Notice tone="info">
        Nothing recognizable found on this branch (read through {result.source}). Pick the runtime and framework yourself below.
      </Notice>
    );
  }
  const name = (s: Suggestion) =>
    s.kind === "compose" ? "Docker Compose" : s.kind === "dockerfile" ? "Its own Dockerfile" : (presets.find((p) => p.id === s.preset)?.name ?? s.preset);

  return (
    <div className="rounded-lg border border-border bg-background/40">
      <div className="border-b border-border px-3 py-2 text-xs text-muted-foreground">
        Found {list.length === 1 ? "one way" : `${list.length} ways`} to deploy this repository (read through {result.source})
        {list.some((s) => s.folder) && ". Each folder becomes its own project: create one, then another for the next folder."}
        {result.partial && " The repository is very large, so some folders may have been missed."}
      </div>
      <ul className="divide-y divide-border">
        {list.map((s, i) => {
          const isApplied = applied === s;
          const Icon = s.kind === "compose" ? Layers : s.kind === "dockerfile" ? FileCode2 : Boxes;
          return (
            <li key={i} className="flex items-start gap-3 px-3 py-2.5">
              <Icon className="mt-0.5 size-4 shrink-0 text-primary" />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2 text-sm font-medium">
                  {name(s)}
                  <span className="flex items-center gap-1 font-mono text-xs font-normal text-muted-foreground">
                    <Folder className="size-3" />
                    {s.folder || "(repo root)"}
                  </span>
                </div>
                <div className="text-[12px] text-muted-foreground">{s.reason}</div>
                {(s.notes ?? []).map((n) => (
                  <div key={n} className="mt-0.5 flex gap-1.5 text-[12px] text-warning">
                    <Info className="mt-0.5 size-3 shrink-0" />
                    {n}
                  </div>
                ))}
              </div>
              <Button type="button" size="sm" variant={isApplied ? "secondary" : i === 0 ? "default" : "outline"} onClick={() => onUse(s)}>
                {isApplied && <Check />}
                {isApplied ? "Applied" : "Use this"}
              </Button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
