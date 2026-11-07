"use client";

import { api, type DatabaseView } from "@/lib/api";
import { BackupsPanel } from "@/components/backups-panel";

/** Backups for one database: compressed dumps in ~/truedeploy/backups. */
export function DatabaseBackups({ db }: { db: DatabaseView }) {
  return (
    <BackupsPanel
      source={{
        name: db.name,
        description: "Compressed dumps saved on the server in ~/truedeploy/backups.",
        replaces: "The current data is",
        load: async () => {
          const [backups, schedule] = await Promise.all([api.dbBackups(db.id), api.dbBackupSchedule(db.id)]);
          return { backups, schedule };
        },
        backupNow: () => api.dbBackupNow(db.id),
        setSchedule: (s) => api.dbSetBackupSchedule(db.id, s),
        restore: (n) => api.dbRestore(db.id, n),
        remove: (n) => api.dbDeleteBackup(db.id, n),
        download: (n) => api.dbDownloadBackup(db.id, n),
        offsite: { kind: "database", id: db.id },
      }}
    />
  );
}
