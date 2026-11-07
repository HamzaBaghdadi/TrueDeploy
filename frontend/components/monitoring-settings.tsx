"use client";

import { Activity, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { api, type AppSettings } from "@/lib/api";
import { Notice } from "@/components/notice";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";

const intervals = [1, 5, 10, 15, 30, 60];

/** Background monitoring, notifications and tray preferences. Saves on change. */
export function MonitoringSettings() {
  const [s, setS] = useState<AppSettings | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.getSettings().then(setS, (e: Error) => setError(e.message));
  }, []);

  async function save(next: AppSettings) {
    setS(next);
    setSaving(true);
    setError(null);
    try {
      setS(await api.saveSettings(next));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Activity className="size-4 text-muted-foreground" />
          Monitoring & notifications
          {saving && <Loader2 className="size-3.5 animate-spin text-muted-foreground" />}
        </CardTitle>
        <CardDescription>
          Background checks for unreachable servers, full disks, apps and websites that are down, expiring HTTPS certificates
          and failed auto-deploys.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {!s ? (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> Loading…
          </p>
        ) : (
          <div className="space-y-4">
            <Row
              id="monitor"
              title="Watch servers and projects"
              body="Runs while TrueDeploy is open or in the tray. It only reads; nothing is changed on your servers."
              checked={s.monitorEnabled}
              onChange={(v) => void save({ ...s, monitorEnabled: v })}
            />
            {s.monitorEnabled && (
              <div className="flex items-center justify-between gap-4 pl-0.5">
                <Label className="text-sm text-muted-foreground">Check every</Label>
                <Select value={String(s.intervalMinutes)} onValueChange={(v) => void save({ ...s, intervalMinutes: Number(v) })}>
                  <SelectTrigger className="w-40">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {intervals.map((m) => (
                      <SelectItem key={m} value={String(m)}>
                        {m === 1 ? "1 minute" : m === 60 ? "1 hour" : `${m} minutes`}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
            <Separator />
            <Row
              id="notify"
              title="Desktop notifications"
              body="A Windows notification when a problem starts and when it's resolved (once per problem, not every check)."
              checked={s.notify}
              onChange={(v) => void save({ ...s, notify: v })}
            />
            <Separator />
            <Row
              id="tray"
              title="Keep running in the system tray"
              body="Closing the window hides TrueDeploy to the tray so monitoring continues. Quit from the tray icon's menu."
              checked={s.closeToTray}
              onChange={(v) => void save({ ...s, closeToTray: v })}
            />
            <Separator />
            <Row
              id="updates"
              title="Check for updates"
              body="Once a day, asks GitHub whether a newer TrueDeploy is out. Nothing is downloaded until you choose to."
              checked={!s.skipUpdateCheck}
              onChange={(v) => void save({ ...s, skipUpdateCheck: !v })}
            />
          </div>
        )}
        {error && (
          <Notice tone="error" className="mt-3">
            {error}
          </Notice>
        )}
      </CardContent>
    </Card>
  );
}

function Row({
  id,
  title,
  body,
  checked,
  onChange,
}: {
  id: string;
  title: string;
  body: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-6">
      <Label htmlFor={id} className="flex-col items-start gap-0.5 font-normal">
        <span className="text-sm font-medium">{title}</span>
        <span className="text-[13px] text-muted-foreground">{body}</span>
      </Label>
      <Switch id={id} checked={checked} onCheckedChange={onChange} />
    </div>
  );
}
