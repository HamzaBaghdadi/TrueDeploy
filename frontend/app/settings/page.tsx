"use client";

import { MonitoringSettings } from "@/components/monitoring-settings";
import { AlertChannels } from "@/components/alert-channels";
import { DataBackupCard } from "@/components/data-backup-card";
import { GitProvidersCard } from "@/components/git-providers-card";
import { OffsiteStorageCard } from "@/components/offsite-storage-card";
import { AboutCard } from "@/components/about-card";

export default function SettingsPage() {
  return (
    <div className="mx-auto max-w-3xl px-8 py-8">
      <header className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight">Settings</h1>
        <p className="mt-1 text-sm text-muted-foreground">Connections and app preferences.</p>
      </header>
      <GitProvidersCard />
      <MonitoringSettings />
      <AlertChannels />
      <OffsiteStorageCard />
      <DataBackupCard />
      <AboutCard />
    </div>
  );
}
