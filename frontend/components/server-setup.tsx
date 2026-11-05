"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { api, onSetupEvents, type Server, type SetupOptions, type SetupPlan, type StepView } from "@/lib/api";
import { useConfirm } from "@/components/confirm-dialog";
import { LogConsole } from "@/components/log-console";

type StepStatus = { status: "running" | "done" | "failed" | "cancelled"; error?: string; durationMs?: number };
type LogLine = { stepId: string; stream: string; line: string };
type RunState = "idle" | "starting" | "running" | "done" | "failed" | "cancelled";
type Mode = "login" | "create";

const MAX_LOG_LINES = 5000;
const input =
  "w-full rounded-md border border-border bg-background px-3 py-2 text-sm outline-none placeholder:text-muted/60 focus:border-accent disabled:opacity-50";

export function ServerSetup({ server, onBack }: { server: Server; onBack: () => void }) {
  const confirm = useConfirm();
  // Empty mode lets the backend pick the best fit on the first scan.
  const [mode, setMode] = useState<Mode | "">("");
  const [deployUser, setDeployUser] = useState("deploy");
  const [sudoPassword, setSudoPassword] = useState("");
  const [showSudoField, setShowSudoField] = useState(false);
  const [plan, setPlan] = useState<SetupPlan | null>(null);
  const [scanning, setScanning] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const [run, setRun] = useState<RunState>("idle");
  const [runError, setRunError] = useState<string | null>(null);
  const [statuses, setStatuses] = useState<Record<string, StepStatus>>({});
  const [logs, setLogs] = useState<LogLine[]>([]);
  const runId = useRef<string | null>(null);
  const awaitingRunId = useRef(false);

  // Options actually used for scans/runs (the plan echoes the effective ones).
  const optsRef = useRef<SetupOptions>({ mode: "", deployUser: "deploy" });

  const scan = useCallback(
    (o: SetupOptions) => {
      optsRef.current = o;
      return api
        .setupPlan(server.id, o)
        .then(
          (p) => {
            setPlan(p);
            setMode(p.mode as Mode);
            setDeployUser(p.deployUser);
            optsRef.current = { ...o, mode: p.mode, deployUser: p.deployUser };
            setError(null);
            setSelected(
              new Set((p.steps ?? []).filter((s) => s.recommended && !s.done && !s.unavailable).map((s) => s.id)),
            );
          },
          (e: Error) => setError(e.message),
        )
        .finally(() => setScanning(false));
    },
    [server.id],
  );

  const rescan = (patch: Partial<SetupOptions> = {}) => {
    setScanning(true);
    setError(null);
    void scan({
      mode,
      deployUser: deployUser.trim() || "deploy",
      sudoPassword: sudoPassword || undefined,
      ...patch,
    });
  };

  useEffect(() => {
    void scan(optsRef.current);
  }, [scan]);

  useEffect(() => {
    // Events can arrive before Run() resolves with the run ID, so the first
    // event of a just-started run claims it.
    const mine = (id: string) => {
      if (!runId.current && awaitingRunId.current) {
        runId.current = id;
        awaitingRunId.current = false;
      }
      return runId.current === id;
    };
    return onSetupEvents({
      log: (e) => {
        if (!mine(e.runId)) return;
        setLogs((l) => {
          const next = [...l, { stepId: e.stepId, stream: e.stream, line: e.line }];
          return next.length > MAX_LOG_LINES ? next.slice(-MAX_LOG_LINES) : next;
        });
      },
      step: (e) => {
        if (!mine(e.runId)) return;
        setRun("running");
        setStatuses((s) => ({
          ...s,
          [e.stepId]: { status: e.status as StepStatus["status"], error: e.error, durationMs: e.durationMs },
        }));
      },
      finished: (e) => {
        if (!mine(e.runId)) return;
        setRun(e.status as RunState);
        setRunError(e.error ?? null);
        runId.current = null;
        setScanning(true);
        void scan(optsRef.current);
      },
    });
  }, [scan]);

  const steps = plan?.steps ?? [];
  const facts = plan?.facts;
  const access = (facts?.access ?? "") as string;
  const chosen = steps.filter((s) => selected.has(s.id));
  const busy = run === "starting" || run === "running";
  const locked = busy || scanning;

  async function start() {
    const needsRoot = chosen.some((s) => s.runAs === "root");
    const ok = await confirm({
      title: `Run ${chosen.length} step${chosen.length === 1 ? "" : "s"} on ${server.name}?`,
      message: needsRoot
        ? `Server steps run as root${access === "root" ? "" : " through sudo"}; account steps run as ${facts?.loginUser}. Each command is shown under “View commands”.`
        : `Everything runs as ${facts?.loginUser}, no admin rights needed.`,
      items: chosen.map((s) => s.title),
      confirmLabel: "Run setup",
    });
    if (!ok) return;

    setRunError(null);
    setStatuses({});
    setLogs([]);
    setRun("starting");
    runId.current = null;
    awaitingRunId.current = true;
    try {
      const id = await api.runSetup(
        server.id,
        chosen.map((s) => s.id),
        optsRef.current,
      );
      if (!runId.current) runId.current = id;
      awaitingRunId.current = false;
    } catch (e) {
      awaitingRunId.current = false;
      setRun("failed");
      setRunError((e as Error).message);
    }
  }

  const toggle = (set: Set<string>, id: string) => {
    const next = new Set(set);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  };

  const serverGroup = { id: "server", title: "Server", hint: "System-wide changes. Need root or sudo." };
  const accountGroup = {
    id: "account",
    title: plan ? `${plan.target}'s account` : "Account",
    hint: "Run as the deploy account. No admin rights needed.",
  };
  // Without admin rights, lead with what the user can actually run.
  const groups = facts?.adminReady ? [serverGroup, accountGroup] : [accountGroup, serverGroup];

  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      <button onClick={onBack} disabled={busy} className="mb-4 text-sm text-muted hover:text-foreground disabled:opacity-40">
        ← Servers
      </button>
      <header className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Set up {server.name}</h1>
          <p className="selectable mt-1 font-mono text-xs text-muted">
            {server.user}@{server.host}
            {plan && ` · ${plan.os}`}
          </p>
        </div>
        <button
          onClick={() => rescan()}
          disabled={locked}
          className="rounded-md border border-border px-3 py-1.5 text-sm hover:border-accent disabled:opacity-50"
        >
          {scanning ? "Scanning…" : "Rescan"}
        </button>
      </header>

      {error && <p className="selectable mb-4 rounded-md bg-danger/10 p-3 text-sm text-danger">{error}</p>}

      {!plan && scanning && (
        <p className="text-sm text-muted">Connecting to {server.host} and checking what this account can do…</p>
      )}

      {plan && facts && (
        <>
          {/* What this account can do */}
          <section className="mb-4 rounded-lg border border-border bg-surface">
            <div className="grid divide-y divide-border sm:grid-cols-3 sm:divide-x sm:divide-y-0">
              <Fact label="Logged in as" value={facts.loginUser} />
              <Fact
                label="Admin rights"
                value={
                  access === "root"
                    ? "root"
                    : access === "sudo"
                      ? "sudo"
                      : access === "sudo-password"
                        ? facts.adminReady
                          ? "sudo (password)"
                          : "sudo (needs password)"
                        : "none"
                }
                tone={facts.adminReady ? "ok" : access === "none" ? "muted" : "warn"}
              />
              <Fact
                label="Docker"
                value={
                  facts.dockerAccess
                    ? "installed · ready"
                    : facts.rootlessDocker
                      ? "rootless · ready"
                      : facts.dockerInstalled
                        ? "installed · no access"
                        : "not installed"
                }
                tone={facts.dockerAccess || facts.rootlessDocker ? "ok" : "warn"}
              />
            </div>

            {(access === "none" || plan.needsSudoPassword || plan.sudoError || showSudoField) && (
              <div className="border-t border-border p-4 text-sm">
                {access === "none" && !showSudoField && (
                  <p className="text-muted">
                    <span className="text-foreground">{facts.loginUser}</span> has no admin rights, so server-wide steps
                    are disabled. TrueDeploy can still prepare this account for deployments.{" "}
                    <button className="text-accent" onClick={() => setShowSudoField(true)}>
                      This account has sudo?
                    </button>
                  </p>
                )}
                {(plan.needsSudoPassword || showSudoField || plan.sudoError) && (
                  <div className="flex flex-wrap items-end gap-3">
                    <div className="min-w-60 flex-1">
                      <label className="mb-1.5 block text-xs font-medium text-muted">Sudo password for {facts.loginUser}</label>
                      <input
                        className={input}
                        type="password"
                        value={sudoPassword}
                        onChange={(e) => setSudoPassword(e.target.value)}
                        onKeyDown={(e) => e.key === "Enter" && sudoPassword && rescan()}
                        disabled={locked}
                        autoFocus={showSudoField}
                      />
                    </div>
                    <button
                      onClick={() => rescan()}
                      disabled={locked || !sudoPassword}
                      className="rounded-md bg-surface-2 px-4 py-2 text-sm hover:bg-border disabled:opacity-50"
                    >
                      Unlock
                    </button>
                  </div>
                )}
                {plan.sudoError && <p className="mt-2 text-danger">{plan.sudoError}</p>}
              </div>
            )}
          </section>

          {/* Which account deployments run under */}
          <section className="mb-6">
            <h2 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted">Deploy as</h2>
            <div className="grid gap-3 sm:grid-cols-2">
              <ModeCard
                active={mode === "login"}
                disabled={locked}
                title={`My account (${facts.loginUser})`}
                body="Apps and containers run under the account you log in with. Works without admin rights."
                onSelect={() => mode !== "login" && rescan({ mode: "login" })}
              />
              <ModeCard
                active={mode === "create"}
                disabled={locked || !facts.adminReady}
                title="Dedicated user"
                body={
                  facts.adminReady
                    ? "A separate account just for deployments, kept apart from your login."
                    : "Needs admin rights to create the account."
                }
                onSelect={() => mode !== "create" && rescan({ mode: "create" })}
              >
                {mode === "create" && (
                  <input
                    className={`${input} mt-3 font-mono`}
                    value={deployUser}
                    spellCheck={false}
                    disabled={locked}
                    onClick={(e) => e.stopPropagation()}
                    onChange={(e) => setDeployUser(e.target.value)}
                    onBlur={() => deployUser.trim() !== plan.deployUser && rescan()}
                    onKeyDown={(e) => e.key === "Enter" && rescan()}
                  />
                )}
              </ModeCard>
            </div>
          </section>

          {groups.map((g) => {
            const list = steps.filter((s) => s.group === g.id);
            if (list.length === 0) return null;
            // A reason shared by most blocked steps is shown once for the section.
            const counts = new Map<string, number>();
            list.forEach((s) => s.unavailable && !s.done && counts.set(s.unavailable, (counts.get(s.unavailable) ?? 0) + 1));
            const [shared] = [...counts.entries()].sort((a, b) => b[1] - a[1]).find(([, n]) => n > 1) ?? [];
            return (
              <section key={g.id} className="mb-4">
                <div className="mb-2 flex items-baseline justify-between">
                  <h2 className="text-xs font-medium uppercase tracking-wide text-muted">{g.title}</h2>
                  <span className="text-xs text-muted">{g.hint}</span>
                </div>
                {shared && (
                  <p className="mb-2 rounded-md border border-warning/20 bg-warning/5 px-3 py-2 text-[13px] text-warning">
                    {shared}
                  </p>
                )}
                <ul
                  className={`divide-y divide-border overflow-hidden rounded-lg border border-border bg-surface ${
                    scanning ? "opacity-60" : ""
                  }`}
                >
                  {list.map((s) => (
                    <StepRow
                      key={s.id}
                      step={s}
                      checked={selected.has(s.id)}
                      status={statuses[s.id]}
                      running={busy}
                      expanded={expanded.has(s.id)}
                      sharedReason={shared}
                      disabled={locked || !!s.unavailable}
                      onToggle={() => setSelected((x) => toggle(x, s.id))}
                      onExpand={() => setExpanded((x) => toggle(x, s.id))}
                    />
                  ))}
                </ul>
              </section>
            );
          })}

          <div className="mt-4 flex items-center gap-3">
            {busy ? (
              <button
                onClick={() => runId.current && api.cancelSetup(runId.current)}
                className="rounded-md border border-danger/60 px-4 py-2 text-sm text-danger hover:bg-danger/10"
              >
                Cancel
              </button>
            ) : (
              <button
                onClick={start}
                disabled={chosen.length === 0 || scanning}
                className="rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-ink disabled:opacity-40"
              >
                Run {chosen.length} step{chosen.length === 1 ? "" : "s"}
              </button>
            )}
            <RunSummary run={run} error={runError} />
          </div>
        </>
      )}

      {(logs.length > 0 || busy) && (
        <div className="mt-6">
          <LogConsole
            running={busy}
            entries={logs.map((l) => ({
              line: l.line,
              stream: l.stream,
              section: steps.find((s) => s.id === l.stepId)?.title ?? l.stepId,
            }))}
          />
        </div>
      )}
    </div>
  );
}

function Fact({ label, value, tone }: { label: string; value: string; tone?: "ok" | "warn" | "muted" }) {
  const dot = tone === "ok" ? "bg-accent" : tone === "warn" ? "bg-warning" : tone === "muted" ? "bg-muted/50" : "";
  return (
    <div className="px-4 py-3">
      <div className="text-xs text-muted">{label}</div>
      <div className="mt-1 flex items-center gap-2 font-mono text-sm">
        {dot && <span className={`size-1.5 rounded-full ${dot}`} />}
        <span className="selectable truncate">{value}</span>
      </div>
    </div>
  );
}

function ModeCard({
  active,
  disabled,
  title,
  body,
  onSelect,
  children,
}: {
  active: boolean;
  disabled: boolean;
  title: string;
  body: string;
  onSelect: () => void;
  children?: React.ReactNode;
}) {
  return (
    <div
      role="radio"
      aria-checked={active}
      aria-disabled={disabled}
      tabIndex={disabled ? -1 : 0}
      onClick={() => !disabled && onSelect()}
      onKeyDown={(e) => !disabled && (e.key === "Enter" || e.key === " ") && onSelect()}
      className={`rounded-lg border p-4 transition-colors ${
        active ? "border-accent bg-accent/5" : "border-border bg-surface"
      } ${disabled && !active ? "cursor-not-allowed opacity-50" : "cursor-pointer hover:border-accent/60"}`}
    >
      <div className="flex items-center gap-2.5">
        <span
          className={`grid size-4 place-items-center rounded-full border ${active ? "border-accent" : "border-muted/60"}`}
        >
          {active && <span className="size-2 rounded-full bg-accent" />}
        </span>
        <span className="text-sm font-medium">{title}</span>
      </div>
      <p className="mt-1.5 pl-6.5 text-[13px] text-muted">{body}</p>
      {children && <div className="pl-6.5">{children}</div>}
    </div>
  );
}

function StepRow({
  step,
  checked,
  status,
  running,
  expanded,
  sharedReason,
  disabled,
  onToggle,
  onExpand,
}: {
  step: StepView;
  checked: boolean;
  status?: StepStatus;
  running: boolean;
  expanded: boolean;
  sharedReason?: string;
  disabled: boolean;
  onToggle: () => void;
  onExpand: () => void;
}) {
  const blocked = !!step.unavailable && !step.done;
  return (
    <li className={`p-4 ${blocked ? "opacity-70" : ""}`}>
      <div className="flex items-start gap-3">
        <div className="mt-0.5 grid size-5 shrink-0 place-items-center">
          {running && status ? (
            <StatusIcon status={status.status} />
          ) : (
            <input
              type="checkbox"
              checked={checked}
              disabled={disabled}
              onChange={onToggle}
              className="size-4 accent-[var(--accent)] disabled:opacity-40"
            />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-medium">{step.title}</span>
            {step.done ? (
              <Badge tone="ok">Done</Badge>
            ) : step.unavailable ? (
              <Badge tone="muted">Unavailable</Badge>
            ) : (
              <Badge tone="warn">Not set up</Badge>
            )}
            {step.runAs === "root" && <Badge tone="muted">admin</Badge>}
            {status?.durationMs ? (
              <span className="font-mono text-[11px] text-muted">{(status.durationMs / 1000).toFixed(1)}s</span>
            ) : null}
          </div>
          <p className="mt-1 text-[13px] text-muted">{step.description}</p>
          {blocked && (step.unavailable !== sharedReason || step.adminCommands) && (
            <Unavailable
              reason={step.unavailable === sharedReason ? "Your admin can enable this with:" : step.unavailable!}
              commands={step.adminCommands}
            />
          )}
          {status?.error && <p className="selectable mt-1 text-[13px] text-danger">{status.error}</p>}
          <button onClick={onExpand} className="mt-2 text-xs text-accent">
            {expanded ? "Hide commands" : "View commands"}
          </button>
          {expanded && (
            <pre className="selectable mt-2 max-h-72 overflow-auto rounded-md border border-border bg-background p-3 font-mono text-[12px] leading-relaxed text-foreground/90">
              {step.script}
            </pre>
          )}
        </div>
      </div>
    </li>
  );
}

// Why a step can't run, plus copyable commands for an admin when there are any.
function Unavailable({ reason, commands: cmd }: { reason: string; commands?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="mt-2 rounded-md border border-warning/20 bg-warning/5 p-2.5 text-[13px] text-warning">
      {reason}
      {cmd && (
        <div className="mt-2 flex items-start gap-2">
          <code className="selectable flex-1 rounded bg-background px-2 py-1.5 font-mono text-[12px] text-foreground/90">
            {cmd}
          </code>
          <button
            onClick={() => {
              void navigator.clipboard.writeText(cmd).then(() => {
                setCopied(true);
                setTimeout(() => setCopied(false), 1500);
              });
            }}
            className="shrink-0 rounded border border-border px-2 py-1 text-xs text-foreground hover:bg-surface-2"
          >
            {copied ? "Copied" : "Copy"}
          </button>
        </div>
      )}
    </div>
  );
}

function Badge({ tone, children }: { tone: "ok" | "warn" | "muted"; children: React.ReactNode }) {
  const cls =
    tone === "ok" ? "bg-accent/10 text-accent" : tone === "warn" ? "bg-warning/10 text-warning" : "bg-surface-2 text-muted";
  return <span className={`rounded px-1.5 py-0.5 text-[11px] font-medium ${cls}`}>{children}</span>;
}

function StatusIcon({ status }: { status: StepStatus["status"] }) {
  if (status === "running")
    return <span className="size-3.5 animate-spin rounded-full border-2 border-accent border-t-transparent" />;
  if (status === "done") return <span className="text-sm text-accent">✓</span>;
  if (status === "failed") return <span className="text-sm text-danger">✕</span>;
  return <span className="text-sm text-muted">–</span>;
}

function RunSummary({ run, error }: { run: RunState; error: string | null }) {
  if (run === "starting") return <span className="text-sm text-muted">Connecting…</span>;
  if (run === "running") return <span className="text-sm text-muted">Running…</span>;
  if (run === "done") return <span className="text-sm text-accent">All steps finished.</span>;
  if (run === "cancelled") return <span className="text-sm text-warning">Cancelled.</span>;
  if (run === "failed") return <span className="selectable text-sm text-danger">{error ?? "Setup failed."}</span>;
  return null;
}
