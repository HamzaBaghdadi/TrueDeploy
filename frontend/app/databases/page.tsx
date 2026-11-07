"use client";

import { ArrowRight, Database, Loader2, Plus, Server as ServerIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { api, type DatabaseView, type Engine, type Server } from "@/lib/api";
import { BackButton } from "@/components/back-button";
import { DatabaseDetail } from "@/components/database-detail";
import { DeepLink } from "@/components/deep-link";
import { EngineBadge } from "@/components/engine-badge";
import { Notice } from "@/components/notice";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { timeAgo } from "@/lib/format";

type View = { kind: "list" } | { kind: "new" } | { kind: "detail"; id: string };

export default function DatabasesPage() {
  const [list, setList] = useState<DatabaseView[] | null>(null);
  const [servers, setServers] = useState<Server[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [view, setView] = useState<View>({ kind: "list" });

  const load = useCallback(
    () =>
      Promise.all([api.listDatabases(), api.listServers()]).then(
        ([d, s]) => {
          setList(d);
          setServers(s);
          setError(null);
        },
        (e: Error) => {
          setError(e.message);
          setList([]);
        },
      ),
    [],
  );
  useEffect(() => {
    void load();
  }, [load]);

  const toList = () => {
    setView({ kind: "list" });
    void load();
  };

  // /databases/?id=<database> opens it; /databases/?new=1 opens the form.
  const link = (
    <DeepLink
      onLink={(p) => {
        const id = p.get("id");
        if (id) setView({ kind: "detail", id });
        else if (p.has("new")) setView({ kind: "new" });
      }}
    />
  );

  if (view.kind === "detail") {
    const db = list?.find((d) => d.id === view.id);
    if (db)
      return (
        <>
          {link}
          <DatabaseDetail key={db.id} db={db} onBack={toList} onDeleted={toList} />
        </>
      );
  }

  return (
    <div className="mx-auto max-w-5xl px-8 py-8">
      {link}
      {view.kind === "new" && <BackButton label="Databases" onClick={toList} />}
      <header className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">{view.kind === "new" ? "New database" : "Databases"}</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Managed database containers on your servers, private to the apps you link them to.
          </p>
        </div>
        {view.kind === "list" && (
          <Button onClick={() => setView({ kind: "new" })}>
            <Plus /> New database
          </Button>
        )}
      </header>

      {error && (
        <Notice tone="error" className="mb-4">
          {error}
        </Notice>
      )}

      {view.kind === "new" ? (
        <NewDatabase servers={servers} onCancel={toList} onCreated={(id) => void load().then(() => setView({ kind: "detail", id }))} />
      ) : list === null ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" /> Loading…
        </p>
      ) : list.length === 0 ? (
        <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border p-12 text-center">
          <Database className="size-6 text-muted-foreground opacity-60" />
          <p className="text-sm text-muted-foreground">No databases yet.</p>
          <Button variant="link" size="sm" onClick={() => setView({ kind: "new" })}>
            Create your first database <ArrowRight />
          </Button>
        </div>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2">
          {list.map((d) => (
            <li key={d.id}>
              <Card
                role="button"
                tabIndex={0}
                onClick={() => setView({ kind: "detail", id: d.id })}
                onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && setView({ kind: "detail", id: d.id })}
                className="cursor-pointer transition-colors hover:ring-primary/50 focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
              >
                <CardContent>
                  <div className="flex items-center justify-between gap-2">
                    <span className="flex min-w-0 items-center gap-2 font-medium">
                      <Database className="size-4 shrink-0 text-muted-foreground" />
                      <span className="truncate">{d.name}</span>
                    </span>
                    <EngineBadge engine={d.engine} name={`${d.engineName} ${d.version}`} />
                  </div>
                  <div className="mt-1 font-mono text-xs text-muted-foreground">
                    {d.host}:{d.port}
                  </div>
                  <div className="mt-3 flex items-center justify-between gap-2 text-xs text-muted-foreground">
                    <span className="flex min-w-0 items-center gap-1.5">
                      <ServerIcon className="size-3.5 shrink-0" />
                      <span className="truncate">
                        {d.serverName} · {d.linkedTo?.length ? `used by ${d.linkedTo.join(", ")}` : "not linked"}
                      </span>
                    </span>
                    <span className="shrink-0">{timeAgo(d.createdAt)}</span>
                  </div>
                </CardContent>
              </Card>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function NewDatabase({ servers, onCancel, onCreated }: { servers: Server[]; onCancel: () => void; onCreated: (id: string) => void }) {
  const [engines, setEngines] = useState<Engine[]>([]);
  const [engine, setEngine] = useState("postgres");
  const [version, setVersion] = useState("");
  const [name, setName] = useState("");
  const [serverId, setServerId] = useState(servers[0]?.id ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.dbEngines().then(setEngines, () => {});
  }, []);
  const current = engines.find((e) => e.id === engine);
  const chosenVersion = version && current?.versions?.includes(version) ? version : (current?.versions?.[0] ?? "");

  async function create(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const d = await api.createDatabase({ serverId, name: name || `${engine}-db`, engine, version: chosenVersion });
      onCreated(d.id);
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  }

  if (servers.length === 0) {
    return (
      <p className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">Add a server first.</p>
    );
  }

  return (
    <form onSubmit={create} className="space-y-6">
      <RadioGroup
        value={engine}
        onValueChange={(v) => {
          setEngine(v);
          setVersion("");
        }}
        className="grid gap-3 sm:grid-cols-4"
      >
        {engines.map((e) => (
          <Label
            key={e.id}
            htmlFor={`engine-${e.id}`}
            className="flex cursor-pointer flex-col items-start gap-2 rounded-lg border border-border bg-card p-4 font-normal transition-colors hover:border-primary/60 has-data-[state=checked]:border-primary has-data-[state=checked]:bg-primary/5"
          >
            <span className="flex w-full items-center justify-between">
              <EngineBadge engine={e.id} name={e.name} />
              <RadioGroupItem value={e.id} id={`engine-${e.id}`} />
            </span>
            <span className="text-xs text-muted-foreground">
              {e.id === "redis" ? "Cache & queues" : e.id === "mongo" ? "Document store" : "Relational"} · port {e.port}
            </span>
          </Label>
        ))}
      </RadioGroup>

      <Card>
        <CardContent className="grid gap-4 sm:grid-cols-3">
          <div className="space-y-1.5">
            <Label htmlFor="db-name">Name</Label>
            <Input id="db-name" value={name} onChange={(e) => setName(e.target.value)} placeholder={`${engine}-db`} autoFocus />
          </div>
          <div className="space-y-1.5">
            <Label>Version</Label>
            <Select value={chosenVersion} onValueChange={setVersion}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Version" />
              </SelectTrigger>
              <SelectContent>
                {current?.versions?.map((v, i) => (
                  <SelectItem key={v} value={v}>
                    {v}
                    {i === 0 ? " (latest)" : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label>Server</Label>
            <Select value={serverId} onValueChange={setServerId}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Server" />
              </SelectTrigger>
              <SelectContent>
                {servers.map((s) => (
                  <SelectItem key={s.id} value={s.id}>
                    {s.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <p className="text-[12px] text-muted-foreground sm:col-span-3">
            A strong password is generated and stored encrypted on this computer. The database is not exposed to the internet: only apps
            you link to it (on the same server) can reach it. Data lives in a Docker volume that survives restarts and redeploys.
          </p>
        </CardContent>
      </Card>

      {error && <Notice tone="error">{error}</Notice>}
      <div className="flex items-center gap-2">
        <Button type="submit" disabled={busy}>
          {busy ? <Loader2 className="animate-spin" /> : <Plus />}
          {busy ? "Creating… (pulling the image can take a minute)" : "Create database"}
        </Button>
        <Button type="button" variant="ghost" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
      </div>
    </form>
  );
}
