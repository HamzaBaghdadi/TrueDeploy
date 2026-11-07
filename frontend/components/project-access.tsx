"use client";

import { Construction, Loader2, LockKeyhole, Save } from "lucide-react";
import { useState } from "react";
import { api, type ProjectDetail } from "@/lib/api";
import { Notice } from "@/components/notice";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

/** Password protection and maintenance mode, applied by the HTTPS proxy. */
export function ProjectAccess({ project, onSaved }: { project: ProjectDetail; onSaved: () => void }) {
  const wasProtected = !!project.authUser;
  const [protect, setProtect] = useState(wasProtected);
  const [user, setUser] = useState(project.authUser || "team");
  const [password, setPassword] = useState("");
  const [maintenance, setMaintenance] = useState(!!project.maintenance);
  const [message, setMessage] = useState(project.maintenanceMessage ?? "");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const hasDomain = (project.domains?.length ?? 0) > 0;

  const dirty =
    protect !== wasProtected ||
    (protect && (user !== project.authUser || password !== "")) ||
    maintenance !== !!project.maintenance ||
    message !== (project.maintenanceMessage ?? "");

  async function save() {
    setBusy(true);
    setMsg(null);
    try {
      setMsg({ ok: true, text: await api.setAccess({ projectId: project.id, protect, user, password, maintenance, message }) });
      setPassword("");
      onSaved();
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card size="sm" className="mt-4 gap-0 py-0">
      <CardHeader className="border-b py-2.5">
        <CardTitle className="flex items-center gap-2">
          <LockKeyhole className="size-4 text-muted-foreground" />
          Access
        </CardTitle>
        {dirty && (
          <CardAction className="self-center">
            <Button size="xs" onClick={() => void save()} disabled={busy || (protect && !wasProtected && password.length < 8)}>
              {busy ? <Loader2 className="animate-spin" /> : <Save />}
              {busy ? "Applying…" : "Save and apply"}
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="space-y-4 py-3">
        {!hasDomain && <p className="text-[12px] text-muted-foreground">These apply to the project&apos;s domains: add a domain for them to take effect.</p>}

        <div className="space-y-3">
          <div className="flex items-start justify-between gap-4">
            <Label htmlFor="protect" className="flex-col items-start gap-0.5 font-normal">
              <span className="text-sm font-medium">Require a password</span>
              <span className="text-[12px] text-muted-foreground">
                Visitors (and search engines) must log in first. Good for previews, staging and admin tools. Monitoring still counts the
                site as up.
              </span>
            </Label>
            <Switch id="protect" checked={protect} onCheckedChange={setProtect} />
          </div>
          {protect && (
            <div className="grid max-w-md grid-cols-2 gap-3">
              <div className="space-y-1">
                <Label htmlFor="access-user" className="text-xs text-muted-foreground">
                  Username
                </Label>
                <Input id="access-user" value={user} onChange={(e) => setUser(e.target.value)} autoComplete="off" spellCheck={false} />
              </div>
              <div className="space-y-1">
                <Label htmlFor="access-pass" className="text-xs text-muted-foreground">
                  Password
                </Label>
                <Input
                  id="access-pass"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder={wasProtected ? "unchanged" : "at least 8 characters"}
                  autoComplete="new-password"
                />
              </div>
            </div>
          )}
        </div>

        <div className="space-y-3 border-t border-border pt-4">
          <div className="flex items-start justify-between gap-4">
            <Label htmlFor="maintenance" className="flex-col items-start gap-0.5 font-normal">
              <span className="flex items-center gap-1.5 text-sm font-medium">
                <Construction className="size-3.5 text-warning" /> Maintenance mode
              </span>
              <span className="text-[12px] text-muted-foreground">
                Visitors see a &quot;Back soon&quot; page (status 503, so search engines keep your pages). The app keeps running behind it;
                no &quot;site down&quot; alerts meanwhile.
              </span>
            </Label>
            <Switch id="maintenance" checked={maintenance} onCheckedChange={setMaintenance} />
          </div>
          {maintenance && (
            <Textarea
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              placeholder="We're making some improvements and will be back shortly."
              className="h-20 text-sm"
              maxLength={500}
            />
          )}
        </div>
        {msg && <Notice tone={msg.ok ? "success" : "error"}>{msg.text}</Notice>}
      </CardContent>
    </Card>
  );
}
