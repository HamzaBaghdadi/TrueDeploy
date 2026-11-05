"use client";

import { useLayoutEffect, useRef } from "react";

export type LogEntry = {
  line: string;
  stream?: string;
  /** A heading is printed whenever the section changes (e.g. a setup step). */
  section?: string;
};

export function LogConsole({
  entries,
  running,
  title = "Output",
  height = "h-80",
}: {
  entries: LogEntry[];
  running?: boolean;
  title?: string;
  height?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const stick = useRef(true);

  // Follow new output unless the user scrolled up to read.
  useLayoutEffect(() => {
    const el = ref.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [entries]);

  return (
    <div className="overflow-hidden rounded-lg border border-border">
      <div className="flex items-center justify-between border-b border-border bg-surface px-4 py-2">
        <span className="text-xs font-medium text-muted">{title}</span>
        {running && <span className="size-2 animate-pulse rounded-full bg-accent" />}
      </div>
      <div
        ref={ref}
        onScroll={(e) => {
          const el = e.currentTarget;
          stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
        }}
        className={`selectable ${height} overflow-auto bg-[#07090b] p-3 font-mono text-[12px] leading-relaxed`}
      >
        {entries.length === 0 && <div className="text-muted">{running ? "Waiting for output…" : "No output."}</div>}
        {entries.map((l, i) => (
          <div key={i}>
            {l.section && (i === 0 || entries[i - 1].section !== l.section) && (
              <div className="mt-2 mb-1 font-semibold text-accent first:mt-0">▶ {l.section}</div>
            )}
            <div
              className={`whitespace-pre-wrap break-all ${
                l.line.startsWith("==>")
                  ? "font-semibold text-foreground"
                  : l.stream === "stderr"
                    ? "text-foreground/60"
                    : "text-foreground/90"
              }`}
            >
              {l.line || " "}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
