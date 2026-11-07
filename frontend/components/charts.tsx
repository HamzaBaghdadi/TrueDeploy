"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";

/** Width of an element, kept current as it resizes. */
function useWidth<T extends HTMLElement>() {
  const ref = useRef<T | null>(null);
  const [width, setWidth] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setWidth(Math.floor(e.contentRect.width)));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, width] as const;
}

export type Point = { t: number; v: number | null }; // unix seconds; null = no reading (unreachable)

const timeFmt = (t: number, long: boolean) =>
  new Date(t * 1000).toLocaleString(undefined, long ? { weekday: "short", hour: "2-digit", minute: "2-digit" } : { hour: "2-digit", minute: "2-digit" });

/**
 * One series over time: a 2px line over a faint area, a recessive % or value
 * axis, gaps where there was no reading, and a crosshair tooltip on hover.
 */
export function TimeSeriesChart({
  points,
  max,
  format,
  height = 96,
  long = false,
  label,
}: {
  points: Point[];
  max?: number; // fixed top of the axis (e.g. 100 for percentages)
  format: (v: number) => string;
  height?: number;
  long?: boolean; // the window spans days: show weekdays in times
  label: string; // for screen readers and the tooltip
}) {
  const [ref, width] = useWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const padB = 16;
  const padT = 6;
  const plotH = height - padB - padT;

  const values = points.map((p) => p.v).filter((v): v is number => v !== null);
  const top = max ?? niceMax(Math.max(1, ...values));
  // Room for the widest axis label (about 6px per character at 10px).
  const padL = Math.max(28, Math.max(...[0, 0.5, 1].map((f) => format(top * f).length)) * 6 + 10);
  const plotW = Math.max(0, width - padL);
  const t0 = points[0]?.t ?? 0;
  const t1 = points[points.length - 1]?.t ?? 1;
  const x = (t: number) => padL + (t1 === t0 ? plotW : ((t - t0) / (t1 - t0)) * plotW);
  const y = (v: number) => padT + plotH - (Math.min(v, top) / top) * plotH;
  // Over about a day, plain times repeat: add the weekday.
  const showDay = long || t1 - t0 > 20 * 3600;

  // Split at gaps so missing readings break the line instead of bridging it.
  const runs: Point[][] = [];
  let cur: Point[] = [];
  for (const p of points) {
    if (p.v === null) {
      if (cur.length) runs.push(cur);
      cur = [];
    } else cur.push(p);
  }
  if (cur.length) runs.push(cur);
  const line = (run: Point[]) => run.map((p, i) => `${i ? "L" : "M"}${x(p.t).toFixed(1)},${y(p.v!).toFixed(1)}`).join("");
  const area = (run: Point[]) =>
    run.length > 1 ? `${line(run)}L${x(run[run.length - 1].t).toFixed(1)},${padT + plotH}L${x(run[0].t).toFixed(1)},${padT + plotH}Z` : "";

  const onMove = useCallback(
    (e: React.PointerEvent<SVGRectElement>) => {
      if (!points.length || plotW <= 0) return;
      const box = (e.currentTarget as SVGRectElement).getBoundingClientRect();
      const t = t0 + ((e.clientX - box.left) / box.width) * (t1 - t0);
      let best = 0;
      for (let i = 1; i < points.length; i++) if (Math.abs(points[i].t - t) < Math.abs(points[best].t - t)) best = i;
      setHover(best);
    },
    [points, plotW, t0, t1],
  );

  const h = hover !== null ? points[hover] : null;
  const gaps = points.filter((p) => p.v === null);

  return (
    <div ref={ref} className="relative w-full" style={{ height }}>
      {width > 0 && points.length > 0 && (
        <svg width={width} height={height} role="img" aria-label={`${label} over time`} className="block">
          {[0, 0.5, 1].map((f) => (
            <g key={f}>
              <line x1={padL} x2={width} y1={padT + plotH - f * plotH} y2={padT + plotH - f * plotH} className="stroke-border" strokeWidth={1} strokeDasharray={f === 0 ? undefined : "2 4"} />
              <text x={padL - 6} y={padT + plotH - f * plotH} dy="0.32em" textAnchor="end" className="fill-muted-foreground text-[10px]">
                {format(top * f)}
              </text>
            </g>
          ))}
          {runs.map((r, i) => (
            <g key={i}>
              <path d={area(r)} className="fill-primary/10" />
              <path d={line(r)} className="fill-none stroke-primary" strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
              {r.length === 1 && <circle cx={x(r[0].t)} cy={y(r[0].v!)} r={2.5} className="fill-primary" />}
            </g>
          ))}
          {/* Unreachable checks: marked on the baseline (with a text legend under the chart). */}
          {gaps.map((p) => (
            <rect key={p.t} x={x(p.t) - 1.5} y={padT + plotH - 3} width={3} height={6} rx={1} className="fill-destructive" />
          ))}
          <text x={padL} y={height - 3} className="fill-muted-foreground text-[10px]">
            {timeFmt(t0, showDay)}
          </text>
          <text x={width - 1} y={height - 3} textAnchor="end" className="fill-muted-foreground text-[10px]">
            {timeFmt(t1, showDay)}
          </text>
          {h && (
            <g pointerEvents="none">
              <line x1={x(h.t)} x2={x(h.t)} y1={padT} y2={padT + plotH} className="stroke-muted-foreground/60" strokeWidth={1} />
              {h.v !== null && <circle cx={x(h.t)} cy={y(h.v)} r={4} className="fill-primary stroke-card" strokeWidth={2} />}
            </g>
          )}
          <rect
            x={padL}
            y={0}
            width={plotW}
            height={plotH}
            fill="transparent"
            onPointerMove={onMove}
            onPointerLeave={() => setHover(null)}
          />
        </svg>
      )}
      {h && width > 0 && (
        <div
          className="pointer-events-none absolute top-0 z-10 rounded-md border border-border bg-popover px-2 py-1 text-[11px] whitespace-nowrap shadow-md"
          style={{ left: Math.min(Math.max(x(h.t) - 60, 0), width - 130) }}
        >
          <div className="text-muted-foreground">{timeFmt(h.t, true)}</div>
          <div className="font-medium">
            {label}: {h.v === null ? "unreachable" : format(h.v)}
          </div>
        </div>
      )}
    </div>
  );
}

function niceMax(v: number) {
  const p = Math.pow(10, Math.floor(Math.log10(v)));
  for (const m of [1, 2, 2.5, 5, 10]) if (m * p >= v) return m * p;
  return 10 * p;
}

export type UptimePoint = { t: number; up: boolean; ms?: number; status?: number };

/** A row of bars, one per time slice: up (green) or down (red), with a tooltip. */
export function UptimeStrip({ points }: { points: UptimePoint[] }) {
  const [hover, setHover] = useState<number | null>(null);
  if (points.length === 0) return <p className="text-[12px] text-muted-foreground">No checks yet: they start with the next monitoring round.</p>;
  const h = hover !== null ? points[hover] : null;
  return (
    <div className="relative">
      <div className="flex h-6 items-stretch gap-[2px]" role="img" aria-label="Uptime over time" onPointerLeave={() => setHover(null)}>
        {points.map((p, i) => (
          <div
            key={p.t}
            onPointerEnter={() => setHover(i)}
            className={cn("min-w-[2px] flex-1 rounded-[2px]", p.up ? "bg-success/70 hover:bg-success" : "bg-destructive/80 hover:bg-destructive")}
          />
        ))}
      </div>
      {h && (
        <div
          className="pointer-events-none absolute top-7 z-10 rounded-md border border-border bg-popover px-2 py-1 text-[11px] whitespace-nowrap shadow-md"
          style={{ left: `clamp(0px, calc(${(100 * (hover! + 0.5)) / points.length}% - 60px), calc(100% - 130px))` }}
        >
          <div className="text-muted-foreground">until {timeFmt(h.t, true)}</div>
          <div className="font-medium">{h.up ? `Up${h.ms ? ` · ${h.ms} ms` : ""}` : `Down${h.status ? ` (${h.status})` : ""}`}</div>
        </div>
      )}
    </div>
  );
}

/** A tiny line of recent values (no axes); -1 marks a missing reading. */
export function Sparkline({ values, max = 100, label, className }: { values: number[]; max?: number; label: string; className?: string }) {
  const w = 96;
  const h = 24;
  if (values.length < 2) return <span className={cn("inline-block h-6 w-24", className)} aria-hidden />;
  const x = (i: number) => (i / (values.length - 1)) * (w - 2) + 1;
  const y = (v: number) => h - 2 - (Math.min(v, max) / max) * (h - 4);
  const parts: string[] = [];
  let pen = false;
  values.forEach((v, i) => {
    if (v < 0) {
      pen = false;
      return;
    }
    parts.push(`${pen ? "L" : "M"}${x(i).toFixed(1)},${y(v).toFixed(1)}`);
    pen = true;
  });
  const gaps = values.map((v, i) => (v < 0 ? i : -1)).filter((i) => i >= 0);
  return (
    <svg width={w} height={h} role="img" aria-label={label} className={cn("shrink-0", className)}>
      <path d={parts.join("")} className="fill-none stroke-primary" strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" />
      {gaps.map((i) => (
        <rect key={i} x={x(i) - 1} y={h - 3} width={2} height={3} className="fill-destructive" />
      ))}
    </svg>
  );
}
