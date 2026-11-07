"use client";

import {
  ChevronDown,
  ChevronRight,
  CircleCheck,
  CircleX,
  Code,
  Loader2,
  LockOpen,
  Minus,
  Play,
  RefreshCw,
  ShieldAlert,
  Square,
  TriangleAlert,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, onSetupEvents, type Server, type SetupOptions, type SetupPlan, type StepView } from "@/lib/api";
import { useConfirm } from "@/components/confirm-dialog";
import { markSetupDone } from "@/components/getting-started";
import { LogConsole } from "@/components/log-console";
import { BackButton } from "@/components/back-button";
import { CopyButton } from "@/components/copy-button";
import { Notice } from "@/components/notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { cn } from "@/lib/utils";

type StepStatus = { status: "running" | "done" | "failed" | "cancelled"; error?: string; durationMs?: number };
type LogLine = { stepId: string; stream: string; line: string };
type RunState = "idle" | "starting" | "running" | "done" | "failed" | "cancelled";
type Mode = "login" | "create";

const MAX_LOG_LINES = 5000;

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
        if (e.status === "done") markSetupDone();
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
      <BackButton label="Servers" onClick={onBack} disabled={busy} />
      <header className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Set up {server.name}</h1>
          <p className="selectable mt-1 font-mono text-xs text-muted-foreground">
            {server.user}@{server.host}
            {plan && ` · ${plan.os}`}
          </p>
        </div>
        <Button variant="outline" onClick={() => rescan()} disabled={locked}>
          <RefreshCw className={scanning ? "animate-spin" : ""} />
          {scanning ? "Scanning…" : "Rescan"}
        </Button>
      </header>

      {error && (
        <Notice tone="error" className="mb-4">
          {error}
        </Notice>
      )}

      {!plan && scanning && (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" />
          Connecting to {server.host} and checking what this account can do…
        </p>
      )}

      {plan && facts && (
        <>
          {/* What this account can do */}
          <Card className="mb-4 gap-0 py-0">
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
              <div className="space-y-3 border-t border-border p-4 text-sm">
                {access === "none" && !showSudoField && (
                  <p className="text-muted-foreground">
                    <span className="text-foreground">{facts.loginUser}</span> has no admin rights, so server-wide steps
                    are disabled. TrueDeploy can still prepare this account for deployments.{" "}
                    <Button variant="link" size="xs" className="h-auto px-0" onClick={() => setShowSudoField(true)}>
                      This account has sudo?
                    </Button>
                  </p>
                )}
                {(plan.needsSudoPassword || showSudoField || plan.sudoError) && (
                  <div className="flex flex-wrap items-end gap-3">
                    <div className="min-w-60 flex-1 space-y-1.5">
                      <Label htmlFor="sudo-pw">Sudo password for {facts.loginUser}</Label>
                      <Input
                        id="sudo-pw"
                        type="password"
                        value={sudoPassword}
                        onChange={(e) => setSudoPassword(e.target.value)}
                        onKeyDown={(e) => e.key === "Enter" && sudoPassword && rescan()}
                        disabled={locked}
                        autoFocus={showSudoField}
                      />
                    </div>
                    <Button variant="secondary" onClick={() => rescan()} disabled={locked || !sudoPassword}>
                      <LockOpen />
                      Unlock
                    </Button>
                  </div>
                )}
                {plan.sudoError && <Notice tone="error">{plan.sudoError}</Notice>}
              </div>
            )}
          </Card>

          {/* Which account deployments run under */}
          <section className="mb-6">
            <h2 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">Deploy as</h2>
            <RadioGroup
              value={mode}
              onValueChange={(v) => v !== mode && rescan({ mode: v as Mode })}
              disabled={locked}
              className="grid gap-3 sm:grid-cols-2"
            >
              <ModeOption
                value="login"
                active={mode === "login"}
                disabled={locked}
                title={`My account (${facts.loginUser})`}
                body="Apps and containers run under the account you log in with. Works without admin rights."
              />
              <ModeOption
                value="create"
                active={mode === "create"}
                disabled={locked || !facts.adminReady}
                title="Dedicated user"
                body={
                  facts.adminReady
                    ? "A separate account just for deployments, kept apart from your login."
                    : "Needs admin rights to create the account."
                }
              >
                {mode === "create" && (
                  <Input
                    className="mt-3 font-mono"
                    value={deployUser}
                    spellCheck={false}
                    disabled={locked}
                    aria-label="Deploy username"
                    onChange={(e) => setDeployUser(e.target.value)}
                    onBlur={() => deployUser.trim() !== plan.deployUser && rescan()}
                    onKeyDown={(e) => e.key === "Enter" && rescan()}
                  />
                )}
              </ModeOption>
            </RadioGroup>
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
                  <h2 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{g.title}</h2>
                  <span className="text-xs text-muted-foreground">{g.hint}</span>
                </div>
                {shared && (
                  <Notice tone="warning" className="mb-2">
                    {shared}
                  </Notice>
                )}
                <Card className={cn("gap-0 py-0", scanning && "opacity-60")}>
                  <ul className="divide-y divide-border">
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
                </Card>
              </section>
            );
          })}

          <div className="mt-4 flex items-center gap-3">
            {busy ? (
              <Button variant="destructive" onClick={() => runId.current && api.cancelSetup(runId.current)}>
                <Square />
                Cancel
              </Button>
            ) : (
              <Button onClick={start} disabled={chosen.length === 0 || scanning}>
                <Play />
                Run {chosen.length} step{chosen.length === 1 ? "" : "s"}
              </Button>
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
  const dot = tone === "ok" ? "bg-success" : tone === "warn" ? "bg-warning" : tone === "muted" ? "bg-muted-foreground/50" : "";
  return (
    <div className="px-4 py-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 flex items-center gap-2 font-mono text-sm">
        {dot && <span className={`size-1.5 rounded-full ${dot}`} />}
        <span className="selectable truncate">{value}</span>
      </div>
    </div>
  );
}

/** One option card in the "Deploy as" radio group. */
function ModeOption({
  value,
  active,
  disabled,
  title,
  body,
  children,
}: {
  value: Mode;
  active: boolean;
  disabled: boolean;
  title: string;
  body: string;
  children?: React.ReactNode;
}) {
  const id = `mode-${value}`;
  return (
    <div
      className={cn(
        "rounded-lg border p-4 transition-colors",
        active ? "border-primary bg-primary/5" : "border-border bg-card",
        disabled && !active ? "opacity-50" : "hover:border-primary/60",
      )}
    >
      <Label htmlFor={id} className={cn("flex items-start gap-2.5", disabled ? "cursor-not-allowed" : "cursor-pointer")}>
        <RadioGroupItem id={id} value={value} disabled={disabled} className="mt-0.5" />
        <span>
          <span className="block text-sm font-medium">{title}</span>
          <span className="mt-1.5 block text-[13px] font-normal text-muted-foreground">{body}</span>
        </span>
      </Label>
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
  const id = `step-${step.id}`;
  return (
    <li className={cn("p-4", blocked && "opacity-70")}>
      <div className="flex items-start gap-3">
        <div className="mt-0.5 grid size-5 shrink-0 place-items-center">
          {running && status ? (
            <StatusIcon status={status.status} />
          ) : (
            <Checkbox id={id} checked={checked} disabled={disabled} onCheckedChange={onToggle} />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <Label htmlFor={id} className="text-sm font-medium">
              {step.title}
            </Label>
            {step.done ? (
              <Badge className="bg-success/10 text-success">
                <CircleCheck />
                Done
              </Badge>
            ) : step.unavailable ? (
              <Badge variant="secondary" className="text-muted-foreground">
                Unavailable
              </Badge>
            ) : (
              <Badge className="bg-warning/10 text-warning">Not set up</Badge>
            )}
            {step.runAs === "root" && (
              <Badge variant="outline" className="text-muted-foreground">
                <ShieldAlert />
                admin
              </Badge>
            )}
            {status?.durationMs ? (
              <span className="font-mono text-[11px] text-muted-foreground">{(status.durationMs / 1000).toFixed(1)}s</span>
            ) : null}
          </div>
          <p className="mt-1 text-[13px] text-muted-foreground">{step.description}</p>
          {blocked && (step.unavailable !== sharedReason || step.adminCommands) && (
            <Unavailable
              reason={step.unavailable === sharedReason ? "Your admin can enable this with:" : step.unavailable!}
              commands={step.adminCommands}
            />
          )}
          {status?.error && (
            <Notice tone="error" className="mt-2">
              {status.error}
            </Notice>
          )}
          <Button variant="link" size="xs" onClick={onExpand} className="mt-1 h-auto px-0">
            <Code />
            {expanded ? "Hide commands" : "View commands"}
            {expanded ? <ChevronDown /> : <ChevronRight />}
          </Button>
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
  return (
    <Notice tone="warning" className="mt-2">
      {reason}
      {cmd && (
        <div className="mt-2 flex items-start gap-2">
          <code className="selectable flex-1 rounded bg-background px-2 py-1.5 font-mono text-[12px] text-foreground/90">{cmd}</code>
          <CopyButton text={cmd} />
        </div>
      )}
    </Notice>
  );
}

function StatusIcon({ status }: { status: StepStatus["status"] }) {
  if (status === "running") return <Loader2 className="size-4 animate-spin text-primary" />;
  if (status === "done") return <CircleCheck className="size-4 text-success" />;
  if (status === "failed") return <CircleX className="size-4 text-destructive" />;
  return <Minus className="size-4 text-muted-foreground" />;
}

function RunSummary({ run, error }: { run: RunState; error: string | null }) {
  if (run === "starting")
    return (
      <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" />
        Connecting…
      </span>
    );
  if (run === "running")
    return (
      <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" />
        Running…
      </span>
    );
  if (run === "done")
    return (
      <span className="flex items-center gap-1.5 text-sm text-success">
        <CircleCheck className="size-4" />
        All steps finished.
      </span>
    );
  if (run === "cancelled")
    return (
      <span className="flex items-center gap-1.5 text-sm text-warning">
        <TriangleAlert className="size-4" />
        Cancelled.
      </span>
    );
  if (run === "failed")
    return (
      <span className="selectable flex items-center gap-1.5 text-sm text-destructive">
        <CircleX className="size-4 shrink-0" />
        {error ?? "Setup failed."}
      </span>
    );
  return null;
}
