"use client";

import { CircleArrowUp, CircleCheck, ExternalLink, FolderOpen, Info, Loader2, RefreshCw } from "lucide-react";
import Image from "next/image";
import { useEffect, useState } from "react";
import { api, type AppInfo } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Kbd } from "@/components/ui/kbd";
import { timeAgo } from "@/lib/format";
import { updateAction, useUpdateStatus } from "@/lib/updates";

export function AboutCard() {
  const [info, setInfo] = useState<AppInfo | null>(null);
  useEffect(() => {
    api.appInfo().then(setInfo, () => {});
  }, []);

  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Info className="size-4 text-muted-foreground" />
          About
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-center gap-4">
          <Image src="/logo.png" alt="" width={56} height={56} className="size-14" />
          <div>
            <div className="text-base font-semibold">
              True<span className="text-brand-gradient">Deploy</span>
            </div>
            <div className="text-sm text-muted-foreground">
              Version <span className="font-mono text-foreground">{info?.version ?? "…"}</span>
              {info && <span className="font-mono"> · {info.platform}</span>}
            </div>
          </div>
        </div>
        <Updates />
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-background/40 px-3 py-2.5">
          <div className="min-w-0">
            <div className="text-xs text-muted-foreground">Your data on this computer</div>
            <div className="selectable truncate font-mono text-xs">{info?.dataDir ?? "…"}</div>
            <div className="mt-0.5 text-[12px] text-muted-foreground">
              Passwords, keys and tokens in it are encrypted; the key lives in your OS keychain.
            </div>
          </div>
          <Button variant="outline" size="sm" onClick={() => void api.openDataFolder()}>
            <FolderOpen />
            Open folder
          </Button>
        </div>
        <p className="text-[13px] text-muted-foreground">
          Tip: press <Kbd>Ctrl</Kbd> <Kbd>K</Kbd> anywhere to jump to a server, project or action.
        </p>
      </CardContent>
    </Card>
  );
}

function Updates() {
  const s = useUpdateStatus();
  const checking = !!s?.checking;
  const checked = s?.checkedAt && !s.checkedAt.startsWith("0001");
  const latest = s?.latest;
  const [installError, setInstallError] = useState<string | null>(null);
  const upd = updateAction(s, setInstallError);

  return (
    <div
      className={
        s?.available
          ? "rounded-lg border border-primary/40 bg-primary/5 px-3 py-2.5"
          : "rounded-lg border border-border bg-background/40 px-3 py-2.5"
      }
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0 text-sm">
          {s?.available && latest ? (
            <span className="flex items-center gap-2 font-medium text-primary">
              <CircleArrowUp className="size-4" /> Version {latest.version} is available
            </span>
          ) : checked && !s?.error ? (
            <span className="flex items-center gap-2">
              <CircleCheck className="size-4 text-success" /> You&apos;re up to date
            </span>
          ) : (
            <span className="text-muted-foreground">Updates</span>
          )}
          <div className="text-[12px] text-muted-foreground">
            {checking
              ? "Checking GitHub…"
              : s?.error
                ? `Couldn't check: ${s.error}`
                : checked
                  ? `Checked ${timeAgo(s!.checkedAt!)}`
                  : "Not checked yet this session."}
          </div>
          {upd?.note && <div className="text-[12px] text-muted-foreground">{upd.note}</div>}
          {installError && <div className="text-[12px] text-destructive">{installError}</div>}
        </div>
        <div className="flex gap-2">
          {s?.available && latest?.pageUrl && (
            <Button variant="ghost" size="sm" onClick={() => void api.openURL(latest.pageUrl)}>
              What&apos;s new <ExternalLink />
            </Button>
          )}
          {upd ? (
            <Button size="sm" onClick={upd.run} disabled={upd.disabled}>
              {upd.busy ? <Loader2 className="animate-spin" /> : <CircleArrowUp />} {upd.label}
            </Button>
          ) : (
            <Button variant="outline" size="sm" onClick={() => void api.checkForUpdate()} disabled={checking}>
              {checking ? <Loader2 className="animate-spin" /> : <RefreshCw />}
              {checking ? "Checking…" : "Check now"}
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
