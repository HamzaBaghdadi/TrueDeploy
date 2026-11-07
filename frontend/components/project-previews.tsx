"use client";

import { ArrowRight, ExternalLink, GitBranchPlus, Loader2, Save, Trash2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { api, type ProjectDetail, type ProjectView } from "@/lib/api";
import { useConfirm } from "@/components/confirm-dialog";
import { DeployStatus } from "@/components/deploy-status";
import { Notice } from "@/components/notice";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

/** Branch previews: a branch deployed next to the main project on its own address. */
export function ProjectPreviews({ project, onChanged }: { project: ProjectDetail; onChanged: () => void }) {
  const router = useRouter();
  const confirm = useConfirm();
  const [list, setList] = useState<ProjectView[] | null>(null);
  const [domain, setDomain] = useState(project.previewDomain ?? "");
  const [creating, setCreating] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const compose = project.runtime !== "dockerfile";

  const load = useCallback(() => api.previews(project.id).then(setList, () => setList([])), [project.id]);
  useEffect(() => {
    void load();
  }, [load]);

  async function saveDomain() {
    setBusy("domain");
    setMsg(null);
    try {
      await api.setPreviewDomain(project.id, domain);
      setMsg({ ok: true, text: domain ? `New previews get <branch>.${domain.replace(/^https?:\/\//, "")}.` : "Preview domains are off." });
      onChanged();
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    } finally {
      setBusy(null);
    }
  }

  async function remove(p: ProjectView) {
    const ok = await confirm({
      title: `Delete the preview of ${p.branch}?`,
      message: "Its container is stopped and removed and its HTTPS route goes away. The main project and the branch itself aren't touched.",
      confirmLabel: "Delete preview",
      tone: "danger",
    });
    if (!ok) return;
    setBusy("del:" + p.id);
    setMsg(null);
    try {
      setMsg({ ok: true, text: await api.deletePreview(p.id) });
      await load();
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message });
    } finally {
      setBusy(null);
    }
  }

  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <GitBranchPlus className="size-4 text-muted-foreground" />
          Branch previews
        </CardTitle>
        <CardDescription>Deploy a branch next to production on its own address, to test it or share it. Delete it when it&apos;s merged.</CardDescription>
        {!compose && (
          <CardAction>
            <Button size="sm" variant="outline" onClick={() => setCreating(true)}>
              <GitBranchPlus /> New preview
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        {compose ? (
          <p className="text-[13px] text-muted-foreground">
            Previews need the Dockerfile runtime (or a framework preset). With Docker Compose the published ports are written in your
            compose file, so a second copy of the app would collide with this one.
          </p>
        ) : (
          <>
            <div className="space-y-1.5">
              <Label htmlFor="preview-domain" className="text-xs text-muted-foreground">
                Preview domain
              </Label>
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-xs text-muted-foreground">&lt;branch&gt;.</span>
                <Input
                  id="preview-domain"
                  className="w-64 font-mono text-xs"
                  value={domain}
                  onChange={(e) => setDomain(e.target.value)}
                  placeholder="preview.example.com"
                  spellCheck={false}
                />
                <Button size="sm" variant="outline" onClick={() => void saveDomain()} disabled={busy !== null || domain === (project.previewDomain ?? "")}>
                  {busy === "domain" ? <Loader2 className="animate-spin" /> : <Save />} Save
                </Button>
              </div>
              <p className="text-[11px] text-muted-foreground">
                Add one wildcard DNS record, <span className="font-mono">*.{domain || "preview.example.com"}</span> → the server&apos;s IP, and every
                preview gets HTTPS automatically. Without it, previews are reachable on their port.
              </p>
            </div>
            {list === null ? (
              <p className="flex items-center gap-2 text-[13px] text-muted-foreground">
                <Loader2 className="size-3.5 animate-spin" /> Loading…
              </p>
            ) : list.length === 0 ? (
              <p className="text-[13px] text-muted-foreground">No previews.</p>
            ) : (
              <ul className="divide-y divide-border rounded-lg border border-border">
                {list.map((p) => (
                  <li key={p.id} className="flex flex-wrap items-center gap-3 px-3 py-2.5">
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2 text-sm font-medium">
                        <span className="font-mono">{p.branch}</span>
                        {p.last ? <DeployStatus status={p.last.status} /> : <span className="text-[11px] text-muted-foreground">not deployed</span>}
                      </div>
                      <div className="text-xs text-muted-foreground">
                        {p.domains?.length ? (
                          <Button variant="link" size="xs" className="h-auto px-0 font-mono" onClick={() => api.openURL(`https://${p.domains![0]}`)}>
                            {p.domains[0]} <ExternalLink />
                          </Button>
                        ) : (
                          <span className="font-mono">port {p.hostPort}</span>
                        )}
                      </div>
                    </div>
                    <Button size="xs" variant="outline" onClick={() => router.push(`/projects/?id=${encodeURIComponent(p.id)}`)}>
                      Open <ArrowRight />
                    </Button>
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label={`Delete preview of ${p.branch}`}
                      className="text-muted-foreground hover:text-destructive"
                      onClick={() => void remove(p)}
                      disabled={busy !== null}
                    >
                      {busy === "del:" + p.id ? <Loader2 className="animate-spin" /> : <Trash2 />}
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
        {msg && <Notice tone={msg.ok ? "success" : "error"}>{msg.text}</Notice>}
      </CardContent>
      {creating && (
        <NewPreviewDialog
          project={project}
          onClose={() => setCreating(false)}
          onCreated={(p) => {
            setCreating(false);
            router.push(`/projects/?id=${encodeURIComponent(p.id)}`);
          }}
        />
      )}
    </Card>
  );
}

function NewPreviewDialog({ project, onClose, onCreated }: { project: ProjectDetail; onClose: () => void; onCreated: (p: ProjectView) => void }) {
  const [branches, setBranches] = useState<string[] | null>(null);
  const [branch, setBranch] = useState("");
  const [copyDbs, setCopyDbs] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState<string | null>(null);
  const hasLinks = (project.links?.length ?? 0) > 0;

  useEffect(() => {
    api.branches(project.id).then(
      (b) => setBranches(b.filter((x) => x !== project.branch)),
      () => setBranches([]), // host not connected: type it
    );
  }, [project.id, project.branch]);

  async function create(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy("Creating…");
    try {
      const p = await api.createPreview({ projectId: project.id, branch, copyDatabases: copyDbs });
      if (p.domains?.length) {
        setBusy("Setting up HTTPS…");
        await api.applyDomains(p.id).catch(() => {}); // can be retried from the preview's Domains panel
      }
      setBusy("Starting deploy…");
      await api.deploy(p.id).catch(() => {});
      onCreated(p);
    } catch (err) {
      setError((err as Error).message);
      setBusy("");
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New branch preview</DialogTitle>
          <DialogDescription>
            Uses {project.name}&apos;s build settings and environment variables, with its own folder and port on the server.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={create} className="space-y-4">
          <div className="space-y-1.5">
            <Label>Branch</Label>
            {branches === null ? (
              <p className="flex items-center gap-2 text-[13px] text-muted-foreground">
                <Loader2 className="size-3.5 animate-spin" /> Loading branches…
              </p>
            ) : branches.length > 0 ? (
              <Select value={branch} onValueChange={setBranch}>
                <SelectTrigger className="w-full font-mono">
                  <SelectValue placeholder="Choose a branch" />
                </SelectTrigger>
                <SelectContent className="max-h-72">
                  {branches.map((b) => (
                    <SelectItem key={b} value={b} className="font-mono">
                      {b}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : (
              <Input className="font-mono" value={branch} onChange={(e) => setBranch(e.target.value)} placeholder="feature/login" spellCheck={false} autoFocus />
            )}
          </div>
          {hasLinks && (
            <div className="space-y-2">
              <Label className="gap-2.5 font-normal">
                <Checkbox checked={copyDbs} onCheckedChange={(v) => setCopyDbs(v === true)} />
                Use the same databases as {project.name}
              </Label>
              {copyDbs ? (
                <Notice tone="warning">The preview&apos;s migrations and writes will run against your production data.</Notice>
              ) : (
                <p className="text-[12px] text-muted-foreground">
                  Off: the preview gets no database link. Give it its own database (Databases → New) and link it, or it will fail where it
                  needs one.
                </p>
              )}
            </div>
          )}
          {error && <Notice tone="error">{error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose} disabled={!!busy}>
              Cancel
            </Button>
            <Button type="submit" disabled={!branch.trim() || !!busy}>
              {busy ? <Loader2 className="animate-spin" /> : <GitBranchPlus />}
              {busy || "Create and deploy"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
