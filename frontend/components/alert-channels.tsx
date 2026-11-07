"use client";

import {
  CircleAlert,
  Hash,
  Loader2,
  MessageSquare,
  Pencil,
  Plus,
  Save,
  Send,
  SendHorizontal,
  Trash2,
  Webhook,
  type LucideIcon,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { api, type ChannelInput, type ChannelView } from "@/lib/api";
import { useConfirm } from "@/components/confirm-dialog";
import { Notice } from "@/components/notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { isSet, timeAgo } from "@/lib/format";

type Kind = "discord" | "slack" | "telegram" | "webhook";

const KINDS: { id: Kind; label: string; icon: LucideIcon }[] = [
  { id: "discord", label: "Discord", icon: MessageSquare },
  { id: "slack", label: "Slack", icon: Hash },
  { id: "telegram", label: "Telegram", icon: Send },
  { id: "webhook", label: "Webhook", icon: Webhook },
];

const kindOf = (k: string) => KINDS.find((x) => x.id === k) ?? KINDS[3];

/** Discord, Slack, Telegram and webhook destinations for alerts. */
export function AlertChannels() {
  const confirm = useConfirm();
  const [list, setList] = useState<ChannelView[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState<ChannelView | "new" | null>(null);

  const load = useCallback(
    () =>
      api.listChannels().then(
        (l) => {
          setList(l);
          setError(null);
        },
        (e: Error) => setError(e.message),
      ),
    [],
  );
  useEffect(() => {
    void load();
  }, [load]);

  async function toggle(c: ChannelView, enabled: boolean) {
    setList((l) => l?.map((x) => (x.id === c.id ? { ...x, enabled } : x)) ?? l);
    try {
      // Empty URL/token keeps the saved secret.
      await api.saveChannel({ ...inputOf(c), enabled });
    } catch (e) {
      setError((e as Error).message);
    }
    void load();
  }

  async function remove(c: ChannelView) {
    const ok = await confirm({
      title: `Remove ${c.name}?`,
      message: "Alerts stop going there. The webhook or bot itself isn't touched.",
      confirmLabel: "Remove channel",
      tone: "danger",
    });
    if (!ok) return;
    await api.deleteChannel(c.id);
    void load();
  }

  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <SendHorizontal className="size-4 text-muted-foreground" />
          Alert channels
        </CardTitle>
        <CardDescription>Get alerts on your phone or in your team&apos;s chat, not only on this computer.</CardDescription>
        <CardAction>
          <Button variant="outline" size="sm" onClick={() => setEditing("new")}>
            <Plus /> Add channel
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {list === null ? (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> Loading…
          </p>
        ) : list.length === 0 ? (
          <div className="flex flex-wrap items-center gap-2 rounded-lg border border-dashed border-border px-4 py-5 text-[13px] text-muted-foreground">
            {KINDS.map(({ id, icon: Icon }) => (
              <Icon key={id} className="size-4" />
            ))}
            <span className="ml-1">No channels yet. Add Discord, Slack, Telegram or any webhook.</span>
          </div>
        ) : (
          <ul className="divide-y divide-border rounded-lg border border-border">
            {list.map((c) => {
              const { icon: Icon, label } = kindOf(c.kind);
              return (
                <li key={c.id} className="flex items-center gap-3 px-3 py-2.5">
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                    <Icon className="size-4" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2 text-sm font-medium">
                      {c.name}
                      {c.criticalOnly && (
                        <Badge variant="secondary" className="text-[10px]">
                          Critical only
                        </Badge>
                      )}
                      {c.deploys && (
                        <Badge variant="secondary" className="text-[10px]">
                          Deploys
                        </Badge>
                      )}
                    </div>
                    <div className="truncate text-xs text-muted-foreground">
                      {label} · <span className="font-mono">{c.target}</span>
                      {c.lastError ? null : isSet(c.lastSent) ? (
                        <> · last sent {timeAgo(c.lastSent)}</>
                      ) : null}
                    </div>
                    {c.lastError && (
                      <div className="mt-0.5 flex items-start gap-1 text-xs text-destructive">
                        <CircleAlert className="mt-0.5 size-3 shrink-0" />
                        <span className="selectable">{c.lastError}</span>
                      </div>
                    )}
                  </div>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span>
                        <Switch checked={c.enabled} onCheckedChange={(v) => void toggle(c, v)} aria-label={`Send alerts to ${c.name}`} />
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>{c.enabled ? "On" : "Off"}</TooltipContent>
                  </Tooltip>
                  <Button variant="ghost" size="icon-sm" aria-label={`Edit ${c.name}`} onClick={() => setEditing(c)}>
                    <Pencil />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Remove ${c.name}`}
                    className="text-muted-foreground hover:text-destructive"
                    onClick={() => void remove(c)}
                  >
                    <Trash2 />
                  </Button>
                </li>
              );
            })}
          </ul>
        )}
        {error && (
          <Notice tone="error" className="mt-3">
            {error}
          </Notice>
        )}
      </CardContent>
      <ChannelDialog
        key={editing === null ? "closed" : editing === "new" ? "new" : editing.id}
        channel={editing}
        onClose={() => setEditing(null)}
        onSaved={() => {
          setEditing(null);
          void load();
        }}
      />
    </Card>
  );
}

function inputOf(c: ChannelView): ChannelInput {
  return {
    id: c.id,
    kind: c.kind,
    name: c.name,
    enabled: c.enabled,
    criticalOnly: c.criticalOnly,
    recoveries: c.recoveries,
    deploys: !!c.deploys,
    url: "",
    token: "",
    chatId: c.chatId ?? "",
  };
}

const HELP: Record<Kind, { steps: string[]; url?: string; urlLabel?: string; placeholder?: string }> = {
  discord: {
    steps: ["In Discord: Server Settings → Integrations → Webhooks → New Webhook.", "Pick the channel, then Copy Webhook URL and paste it here."],
    placeholder: "https://discord.com/api/webhooks/…",
  },
  slack: {
    steps: ["Create a Slack app, turn on Incoming Webhooks and add one to a channel.", "Copy the webhook URL and paste it here."],
    url: "https://api.slack.com/apps?new_app=1",
    urlLabel: "Create a Slack app",
    placeholder: "https://hooks.slack.com/services/…",
  },
  telegram: {
    steps: [
      "Message @BotFather, send /newbot and copy the token it gives you.",
      "Send your new bot any message (or add it to a group), then paste your chat ID. @userinfobot tells you yours.",
    ],
    url: "https://t.me/BotFather",
    urlLabel: "Open @BotFather",
  },
  webhook: {
    steps: [
      'Receives a JSON POST: { "event": "alert.opened" | "alert.resolved", "text", "alert" }.',
      "Works with ntfy, n8n, Zapier, Home Assistant and your own endpoints.",
    ],
    placeholder: "https://ntfy.sh/my-servers",
  },
};

function ChannelDialog({
  channel,
  onClose,
  onSaved,
}: {
  channel: ChannelView | "new" | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const existing = channel && channel !== "new" ? channel : null;
  const [kind, setKind] = useState<Kind>((existing?.kind as Kind) ?? "discord");
  const [name, setName] = useState(existing?.name ?? "");
  const [url, setUrl] = useState("");
  const [token, setToken] = useState("");
  const [chatId, setChatId] = useState(existing?.chatId ?? "");
  const [criticalOnly, setCriticalOnly] = useState(existing?.criticalOnly ?? false);
  const [recoveries, setRecoveries] = useState(existing?.recoveries ?? true);
  const [deploys, setDeploys] = useState(existing?.deploys ?? false);
  const [busy, setBusy] = useState<"" | "test" | "save">("");
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);

  // A saved secret is kept unless replaced (only when the kind is unchanged).
  const keepsSecret = !!existing && existing.kind === kind;
  const input: ChannelInput = {
    id: existing?.id ?? "",
    kind,
    name: name.trim() || kindOf(kind).label,
    enabled: existing?.enabled ?? true,
    criticalOnly,
    recoveries,
    deploys,
    url,
    token,
    chatId,
  };
  const help = HELP[kind];

  async function run(what: "test" | "save") {
    setBusy(what);
    setMsg(null);
    try {
      if (what === "test") setMsg({ ok: true, text: await api.testChannel(input) });
      else {
        await api.saveChannel(input);
        onSaved();
      }
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    } finally {
      setBusy("");
    }
  }

  return (
    <Dialog open={channel !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{existing ? `Edit ${existing.name}` : "Add alert channel"}</DialogTitle>
          <DialogDescription>Alerts and recoveries are posted here as soon as a check finds them.</DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            void run("save");
          }}
        >
          <ToggleGroup
            type="single"
            variant="outline"
            value={kind}
            onValueChange={(v) => {
              if (!v) return;
              setKind(v as Kind);
              setMsg(null);
            }}
            className="w-full"
          >
            {KINDS.map(({ id, label, icon: Icon }) => (
              <ToggleGroupItem
                key={id}
                value={id}
                className="flex-1 data-[state=on]:border-primary data-[state=on]:bg-primary/10 data-[state=on]:text-primary"
              >
                <Icon /> {label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>

          <ol className="space-y-1 rounded-lg bg-muted px-3 py-2.5 text-[12px] text-muted-foreground">
            {help.steps.map((s, i) => (
              <li key={s}>
                {i + 1}. {s}
              </li>
            ))}
            {help.url && (
              <li>
                <Button type="button" variant="link" size="xs" className="h-auto px-0" onClick={() => api.openURL(help.url!)}>
                  {help.urlLabel} →
                </Button>
              </li>
            )}
          </ol>

          <div className="space-y-1.5">
            <Label htmlFor="ch-name">Name</Label>
            <Input id="ch-name" value={name} onChange={(e) => setName(e.target.value)} placeholder={`e.g. ${kindOf(kind).label} #ops`} />
          </div>

          {kind === "telegram" ? (
            <div className="grid gap-3 sm:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
              <div className="space-y-1.5">
                <Label htmlFor="ch-token">Bot token</Label>
                <Input
                  id="ch-token"
                  type="password"
                  className="font-mono"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  placeholder={keepsSecret ? "Saved (leave empty to keep)" : "123456789:AA…"}
                  autoComplete="off"
                  spellCheck={false}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="ch-chat">Chat ID</Label>
                <Input
                  id="ch-chat"
                  className="font-mono"
                  value={chatId}
                  onChange={(e) => setChatId(e.target.value)}
                  placeholder="123456789"
                  spellCheck={false}
                />
              </div>
            </div>
          ) : (
            <div className="space-y-1.5">
              <Label htmlFor="ch-url">Webhook URL</Label>
              <Input
                id="ch-url"
                type="password"
                className="font-mono"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder={keepsSecret ? `Saved: ${existing?.target} (leave empty to keep)` : help.placeholder}
                autoComplete="off"
                spellCheck={false}
              />
              <p className="text-[11px] text-muted-foreground">Anyone with this URL can post to your channel; it&apos;s stored encrypted.</p>
            </div>
          )}

          <div className="space-y-2.5">
            <Label className="gap-2.5 font-normal">
              <Checkbox checked={recoveries} onCheckedChange={(v) => setRecoveries(v === true)} />
              Also send recoveries (&quot;Resolved: …&quot;)
            </Label>
            <Label className="gap-2.5 font-normal">
              <Checkbox checked={deploys} onCheckedChange={(v) => setDeploys(v === true)} />
              Deploys from TrueDeploy (succeeded, failed, rolled back)
            </Label>
            <Label className="gap-2.5 font-normal">
              <Checkbox checked={criticalOnly} onCheckedChange={(v) => setCriticalOnly(v === true)} />
              Critical alerts only (skip warnings like a disk at 90%)
            </Label>
          </div>

          {msg && <Notice tone={msg.ok ? "success" : "error"}>{msg.text}</Notice>}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => void run("test")} disabled={!!busy}>
              {busy === "test" ? <Loader2 className="animate-spin" /> : <Send />}
              {busy === "test" ? "Sending…" : "Send test"}
            </Button>
            <Button type="submit" disabled={!!busy}>
              {busy === "save" ? <Loader2 className="animate-spin" /> : <Save />}
              {busy === "save" ? "Saving…" : existing ? "Save changes" : "Add channel"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
