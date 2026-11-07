"use client";

import { Cloud, CloudOff, Loader2, ShieldCheck } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, type OffsiteTarget } from "@/lib/api";
import { useConfirm } from "@/components/confirm-dialog";
import { Notice } from "@/components/notice";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

type Provider = {
  id: string;
  name: string;
  /** What the region field is called and an example, or null when not asked. */
  region: { label: string; example: string } | null;
  endpoint: (region: string, account: string) => string;
  help: string;
};

const PROVIDERS: Provider[] = [
  {
    id: "aws",
    name: "Amazon S3",
    region: { label: "Region", example: "eu-central-1" },
    endpoint: () => "",
    help: "Create a bucket, then an IAM user with access to it (s3:ListBucket, GetObject, PutObject, DeleteObject).",
  },
  {
    id: "r2",
    name: "Cloudflare R2",
    region: null,
    endpoint: (_, account) => (account ? `https://${account}.r2.cloudflarestorage.com` : ""),
    help: "R2 → Create bucket, then Manage R2 API tokens → Object Read & Write for that bucket. The account ID is on the R2 overview page.",
  },
  {
    id: "b2",
    name: "Backblaze B2",
    region: { label: "Region", example: "us-west-004" },
    endpoint: (r) => (r ? `https://s3.${r}.backblazeb2.com` : ""),
    help: "Create a private bucket, then an application key for it. The region is in the bucket's endpoint (s3.<region>.backblazeb2.com).",
  },
  {
    id: "wasabi",
    name: "Wasabi",
    region: { label: "Region", example: "eu-central-1" },
    endpoint: (r) => (r ? `https://s3.${r}.wasabisys.com` : ""),
    help: "Create a bucket, then an access key with access to it.",
  },
  {
    id: "spaces",
    name: "DigitalOcean Spaces",
    region: { label: "Region", example: "fra1" },
    endpoint: (r) => (r ? `https://${r}.digitaloceanspaces.com` : ""),
    help: "Create a Space, then a Spaces access key (API → Spaces Keys).",
  },
  {
    id: "other",
    name: "Other S3-compatible (MinIO, SeaweedFS…)",
    region: { label: "Region", example: "us-east-1" },
    endpoint: () => "",
    help: "Use the storage's S3 URL and an access key that can list, read, write and delete in the bucket.",
  },
];

const r2Account = (endpoint: string) => /^https:\/\/([a-z0-9]+)\.r2\.cloudflarestorage\.com/i.exec(endpoint)?.[1] ?? "";

/** Settings card: the S3-compatible storage backups are copied to. */
export function OffsiteStorageCard() {
  const confirm = useConfirm();
  const ref = useRef<HTMLDivElement>(null);
  const [loaded, setLoaded] = useState(false);
  const [configured, setConfigured] = useState(false);
  const [hasSecret, setHasSecret] = useState(false);
  const [provider, setProvider] = useState("aws");
  const [region, setRegion] = useState("");
  const [account, setAccount] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [bucket, setBucket] = useState("");
  const [prefix, setPrefix] = useState("truedeploy");
  const [accessKey, setAccessKey] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [busy, setBusy] = useState<"save" | "remove" | null>(null);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);

  function fill(t: OffsiteTarget, isSet: boolean, secret: boolean) {
    setConfigured(isSet);
    setHasSecret(secret);
    setProvider(t.provider || "aws");
    setRegion(t.provider === "r2" ? "" : t.region ?? "");
    setAccount(r2Account(t.endpoint ?? ""));
    setEndpoint(t.endpoint ?? "");
    setBucket(t.bucket ?? "");
    setPrefix(t.prefix || "truedeploy");
    setAccessKey(t.accessKey ?? "");
    setSecretKey("");
  }

  useEffect(() => {
    api.offsite().then(
      (v) => {
        if (v) fill(v.target, v.configured, v.hasSecret);
        setLoaded(true);
        // Links from a backups panel land here (#offsite).
        if (window.location.hash === "#offsite") setTimeout(() => ref.current?.scrollIntoView({ block: "start" }), 50);
      },
      () => setLoaded(true),
    );
  }, []);

  const p = PROVIDERS.find((x) => x.id === provider) ?? PROVIDERS[0];
  const computed = provider === "other" ? endpoint : p.endpoint(region.trim(), account.trim());

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy("save");
    setMsg(null);
    try {
      const text = await api.saveOffsite({
        provider,
        endpoint: computed,
        region: provider === "r2" ? "auto" : region,
        bucket,
        prefix,
        accessKey,
        secretKey,
      });
      setConfigured(true);
      setHasSecret(true);
      setSecretKey("");
      setMsg({ ok: true, text });
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message });
    } finally {
      setBusy(null);
    }
  }

  async function remove() {
    const ok = await confirm({
      title: "Remove the offsite storage?",
      message:
        "Backups stop being copied offsite, and the credentials are removed from your servers. Copies already in the bucket are kept; delete them in your storage's console if you want.",
      confirmLabel: "Remove",
      tone: "danger",
    });
    if (!ok) return;
    setBusy("remove");
    setMsg(null);
    try {
      const text = await api.removeOffsite();
      fill({ provider: "aws", endpoint: "", region: "", bucket: "", prefix: "truedeploy", accessKey: "", secretKey: "" }, false, false);
      setMsg({ ok: true, text });
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message });
    } finally {
      setBusy(null);
    }
  }

  return (
    <Card className="mt-4 scroll-mt-4" id="offsite" ref={ref}>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Cloud className="size-4 text-muted-foreground" />
          Offsite backups
        </CardTitle>
        <CardDescription>
          Database and volume backups are saved on the server they come from, so losing the server loses them too. Add S3-compatible
          storage, then turn on “Also copy each backup offsite” in a backup schedule.
        </CardDescription>
        {configured && (
          <CardAction>
            <span className="flex items-center gap-1.5 text-xs text-success">
              <ShieldCheck className="size-3.5" /> Connected
            </span>
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        {!loaded ? (
          <p className="flex items-center gap-2 text-[13px] text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" /> Loading…
          </p>
        ) : (
          <form onSubmit={save} className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label>Storage</Label>
                <Select value={provider} onValueChange={setProvider}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {PROVIDERS.map((x) => (
                      <SelectItem key={x.id} value={x.id}>
                        {x.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {provider === "r2" ? (
                <div className="space-y-1.5">
                  <Label htmlFor="off-account">Account ID</Label>
                  <Input id="off-account" className="font-mono" value={account} onChange={(e) => setAccount(e.target.value)} spellCheck={false} />
                </div>
              ) : (
                p.region && (
                  <div className="space-y-1.5">
                    <Label htmlFor="off-region">{p.region.label}</Label>
                    <Input
                      id="off-region"
                      className="font-mono"
                      value={region}
                      onChange={(e) => setRegion(e.target.value)}
                      placeholder={p.region.example}
                      spellCheck={false}
                    />
                  </div>
                )
              )}
            </div>
            {provider === "other" ? (
              <div className="space-y-1.5">
                <Label htmlFor="off-endpoint">S3 URL</Label>
                <Input
                  id="off-endpoint"
                  className="font-mono"
                  value={endpoint}
                  onChange={(e) => setEndpoint(e.target.value)}
                  placeholder="https://s3.example.com"
                  spellCheck={false}
                />
              </div>
            ) : (
              computed && (
                <p className="text-[12px] text-muted-foreground">
                  Uses <span className="font-mono">{computed}</span>
                </p>
              )
            )}
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="off-bucket">Bucket</Label>
                <Input id="off-bucket" className="font-mono" value={bucket} onChange={(e) => setBucket(e.target.value)} spellCheck={false} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="off-prefix">Folder in the bucket</Label>
                <Input id="off-prefix" className="font-mono" value={prefix} onChange={(e) => setPrefix(e.target.value)} spellCheck={false} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="off-access">Access key ID</Label>
                <Input
                  id="off-access"
                  className="font-mono"
                  value={accessKey}
                  onChange={(e) => setAccessKey(e.target.value)}
                  autoComplete="off"
                  spellCheck={false}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="off-secret">Secret access key</Label>
                <Input
                  id="off-secret"
                  type="password"
                  className="font-mono"
                  value={secretKey}
                  onChange={(e) => setSecretKey(e.target.value)}
                  placeholder={hasSecret ? "Saved (leave empty to keep)" : ""}
                  autoComplete="new-password"
                />
              </div>
            </div>
            <p className="text-[12px] text-muted-foreground">
              {p.help} The key is stored encrypted here, and in a file only your user can read on servers that copy backups offsite.
            </p>
            {msg && <Notice tone={msg.ok ? "success" : "error"}>{msg.text}</Notice>}
            <div className="flex justify-end gap-2">
              {configured && (
                <Button type="button" variant="ghost" className="text-destructive" onClick={() => void remove()} disabled={busy !== null}>
                  {busy === "remove" ? <Loader2 className="animate-spin" /> : <CloudOff />}
                  Remove
                </Button>
              )}
              <Button type="submit" disabled={busy !== null || !bucket.trim() || !accessKey.trim() || (!hasSecret && !secretKey)}>
                {busy === "save" && <Loader2 className="animate-spin" />}
                {busy === "save" ? "Testing…" : "Save and test"}
              </Button>
            </div>
          </form>
        )}
      </CardContent>
    </Card>
  );
}
