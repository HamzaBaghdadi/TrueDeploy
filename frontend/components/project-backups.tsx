"use client";

import { api, type ProjectDetail } from "@/lib/api";
import { BackupsPanel } from "@/components/backups-panel";

/** Backups of a project's Docker volumes (uploads, app data, embedded databases). */
export function ProjectBackups({ project }: { project: ProjectDetail }) {
  return (
    <BackupsPanel
      className="mt-4"
      source={{
        name: project.name,
        description: (
          <>
            Every Docker volume of the project in one archive, saved on the server in{" "}
            <span className="font-mono">~/truedeploy/backups/projects/{project.slug}</span>.
          </>
        ),
        replaces: "All of the project's volumes are",
        backupNowNote:
          "The project's containers stop for the few seconds the copy takes, so data that changes together is saved consistently. They start again right after.",
        load: async () => {
          const r = await api.projectBackups(project.id);
          return { backups: r.backups ?? [], schedule: r.schedule };
        },
        backupNow: () => api.backupProjectNow(project.id),
        setSchedule: (s) => api.setProjectBackupSchedule(project.id, s),
        restore: (n) => api.restoreProjectBackup(project.id, n),
        remove: (n) => api.deleteProjectBackup(project.id, n),
        download: (n) => api.downloadProjectBackup(project.id, n),
        offsite: { kind: "project", id: project.id },
      }}
    />
  );
}
