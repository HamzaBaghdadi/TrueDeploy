"use client";

import { ArrowDown, Download, Loader2, Pause, Play, RotateCw, ScrollText, Search, Square, X } from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api, onLogsEvents } from "@/lib/api";
import { Notice } from "@/components/notice";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";

const RANGES: { value: string; label: string; tail: number }[] = [
  { value: "lines", label: "Last 200 lines", tail: 200 },
  { value: "15m", label: "Last 15 minutes", tail: 5000 },
  { value: "1h", label: "Last hour", tail: 5000 },
  { value: "6h", label: "Last 6 hours", tail: 5000 },
  { value: "24h", label: "Last 24 hours", tail: 5000 },
  { value: "168h", label: "Last 7 days", tail: 5000 },
];
const MAX_LINES = 5000;
const ERROR_RE = /\b(error|err|exception|fatal|panic|critical|traceback|unhandled|failed)\b|\b(HTTP\/\d(\.\d)?"?\s+|status[=: ]+)5\d\d\b/i;

/** Live container logs with time ranges, search, an errors filter, pause and download. */
export function ProjectLogs({ projectId }: { projectId: string }) {
  const [range, setRange] = useState("lines");
  const [lines, setLines] = useState<string[]>([]);
  const [streamId, setStreamId] = useState<string | null>(null);
  const streamRef = useRef<string | null>(null);
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [errorsOnly, setErrorsOnly] = useState(false);
  const [paused, setPaused] = useState<string[] | null>(null); // frozen copy while paused
  const [downloading, setDownloading] = useState(false);
  const [saved, setSaved] = useState<string | null>(null);

  useEffect(() => {
    const off = onLogsEvents({
      line: (e) => {
        if (e.streamId === streamRef.current) setLines((l) => (l.length >= MAX_LINES ? [...l.slice(-MAX_LINES + 1), e.line] : [...l, e.line]));
      },
      end: (e) => {
        if (e.streamId !== streamRef.current) return;
        streamRef.current = null;
        setStreamId(null);
        if (e.error) setError(e.error);
      },
    });
    return () => {
      off();
      // Leaving the page stops the stream on the server too.
      if (streamRef.current) void api.stopLogs(streamRef.current);
    };
  }, []);

  async function start(r = range) {
    if (streamRef.current) void api.stopLogs(streamRef.current);
    streamRef.current = null;
    setStarting(true);
    setError(null);
    setLines([]);
    setPaused(null);
    try {
      const sel = RANGES.find((x) => x.value === r)!;
      const id = await api.streamLogs(projectId, sel.tail, r === "lines" ? "" : r);
      streamRef.current = id;
      setStreamId(id);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setStarting(false);
    }
  }

  function stop() {
    if (streamRef.current) void api.stopLogs(streamRef.current);
  }

  async function download() {
    setDownloading(true);
    setSaved(null);
    setError(null);
    try {
      const path = await api.downloadLogs(projectId, range === "lines" ? "24h" : range);
      if (path) setSaved(path);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setDownloading(false);
    }
  }

  const live = streamId !== null;
  const shown = paused ?? lines;
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return shown.filter((l) => (!errorsOnly || ERROR_RE.test(l)) && (!q || l.toLowerCase().includes(q)));
  }, [shown, query, errorsOnly]);
  const newWhilePaused = paused ? lines.length - paused.length : 0;
  const started = live || lines.length > 0;

  return (
    <Card size="sm" className="mt-4 gap-0 py-0">
      <CardHeader className="border-b py-2.5">
        <CardTitle className="flex items-center gap-2">
          <ScrollText className="size-4 text-muted-foreground" />
          Logs
        </CardTitle>
        <CardAction className="flex items-center gap-1 self-center">
          <Button variant="ghost" size="xs" onClick={() => void download()} disabled={downloading} className="text-muted-foreground">
            {downloading ? <Loader2 className="animate-spin" /> : <Download />}
            {downloading ? "Saving…" : "Download"}
          </Button>
          {live ? (
            <Button variant="ghost" size="xs" onClick={stop} className="text-destructive">
              <Square /> Stop
            </Button>
          ) : (
            <Button variant="ghost" size="xs" onClick={() => void start()} disabled={starting} className="text-primary">
              {starting ? <Loader2 className="animate-spin" /> : started ? <RotateCw /> : <Play />}
              {starting ? "Connecting…" : started ? "Restart" : "Show live logs"}
            </Button>
          )}
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-3 py-3">
        <div className="flex flex-wrap items-center gap-2">
          <Select
            value={range}
            onValueChange={(v) => {
              setRange(v);
              if (started) void start(v);
            }}
          >
            <SelectTrigger size="sm" className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {RANGES.map((r) => (
                <SelectItem key={r.value} value={r.value}>
                  {r.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <div className="relative min-w-48 flex-1">
            <Search className="absolute top-2 left-2.5 size-3.5 text-muted-foreground" />
            <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search…" className="h-8 pl-8 font-mono text-xs" spellCheck={false} />
            {query && (
              <Button variant="ghost" size="icon-xs" aria-label="Clear search" className="absolute top-1 right-1 text-muted-foreground" onClick={() => setQuery("")}>
                <X />
              </Button>
            )}
          </div>
          <Label className="gap-2 text-xs font-normal">
            <Switch checked={errorsOnly} onCheckedChange={setErrorsOnly} size="sm" /> Errors only
          </Label>
          {live && (
            <Button variant="outline" size="xs" onClick={() => setPaused(paused ? null : [...lines])}>
              {paused ? <Play /> : <Pause />}
              {paused ? "Resume" : "Pause"}
            </Button>
          )}
        </div>

        {error && <Notice tone="error">{error}</Notice>}
        {saved && <Notice tone="success">Saved to {saved}</Notice>}

        {started ? (
          <LogView lines={filtered} total={shown.length} query={query.trim()} live={live && !paused} paused={!!paused} newCount={newWhilePaused} onResume={() => setPaused(null)} />
        ) : (
          <p className="text-[13px] text-muted-foreground">
            Pick a time range and click Show live logs: earlier lines first, then new ones as they happen. Download saves up to 50,000 lines
            {range === "lines" ? " from the last 24 hours" : " from the chosen range"} to a file.
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function LogView({
  lines,
  total,
  query,
  live,
  paused,
  newCount,
  onResume,
}: {
  lines: string[];
  total: number;
  query: string;
  live: boolean;
  paused: boolean;
  newCount: number;
  onResume: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  const [atBottom, setAtBottom] = useState(true);

  // Follow new lines unless the user scrolled up to read.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (stick.current) el.scrollTop = el.scrollHeight;
    // Filtering can shrink the list until nothing scrolls; then we're at the bottom.
    const bottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    stick.current = bottom;
    setAtBottom(bottom);
  }, [lines]);

  const lower = query.toLowerCase();
  const mark = (l: string) => {
    if (!query) return l;
    const parts: React.ReactNode[] = [];
    let from = 0;
    const ll = l.toLowerCase();
    for (let i = ll.indexOf(lower); i >= 0; i = ll.indexOf(lower, from)) {
      parts.push(l.slice(from, i), <mark key={i} className="rounded-[2px] bg-warning/35 text-foreground">{l.slice(i, i + query.length)}</mark>);
      from = i + query.length;
    }
    parts.push(l.slice(from));
    return parts;
  };

  return (
    <div className="relative overflow-hidden rounded-lg border border-border">
      <div className="flex items-center justify-between border-b border-border bg-surface px-3 py-1.5 text-[11px] text-muted-foreground">
        <span>
          {lines.length === total ? `${total} lines` : `${lines.length} of ${total} lines`}
          {paused && newCount > 0 && ` · ${newCount} new while paused`}
        </span>
        <span className="flex items-center gap-1.5">
          {live && <span className="size-1.5 animate-pulse rounded-full bg-primary" />}
          {live ? "Following" : paused ? "Paused" : "Stopped"}
        </span>
      </div>
      <div
        ref={ref}
        onScroll={(e) => {
          const el = e.currentTarget;
          const bottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
          stick.current = bottom;
          setAtBottom(bottom);
        }}
        className="selectable h-96 overflow-auto bg-terminal px-3 py-2 font-mono text-[11.5px] leading-relaxed"
      >
        {lines.length === 0 ? (
          <div className="py-6 text-center text-muted-foreground">{total === 0 ? "Waiting for log lines…" : "No lines match."}</div>
        ) : (
          lines.map((l, i) => (
            <div key={i} className={cn("break-all whitespace-pre-wrap", ERROR_RE.test(l) && "text-destructive")}>
              {mark(l)}
            </div>
          ))
        )}
      </div>
      {(!atBottom || (paused && newCount > 0)) && lines.length > 0 && (
        <Button
          size="xs"
          className="absolute right-3 bottom-3 shadow-md"
          onClick={() => {
            if (paused) onResume();
            stick.current = true;
            setAtBottom(true);
            const el = ref.current;
            if (el) el.scrollTop = el.scrollHeight;
          }}
        >
          <ArrowDown /> Newest
        </Button>
      )}
    </div>
  );
}
