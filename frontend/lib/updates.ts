"use client";

import { useEffect, useSyncExternalStore } from "react";
import { api, onUpdateStatus, type UpdateStatus } from "@/lib/api";

// One shared copy of the update status for the sidebar and the About card,
// kept current by the backend's update:status event.
let state: UpdateStatus | null = null;
const listeners = new Set<() => void>();
let started = false;

function set(s: UpdateStatus) {
  state = s;
  listeners.forEach((l) => l());
}

function start() {
  if (started) return;
  started = true;
  onUpdateStatus(set);
  api.updateStatus().then(set, () => {});
}

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => listeners.delete(l);
};

export function useUpdateStatus(): UpdateStatus | null {
  useEffect(start, []);
  return useSyncExternalStore(
    subscribe,
    () => state,
    () => null,
  );
}

/** Opens the installer download if the release has one, else the release page. */
export function downloadUpdate(s: UpdateStatus | null) {
  const url = s?.latest?.installer || s?.latest?.pageUrl;
  if (url) void api.openURL(url);
}

export type UpdateAction = { label: string; busy: boolean; disabled: boolean; run: () => void; note?: string };

/** What the update button says and does, for every stage of an update. */
export function updateAction(s: UpdateStatus | null, onError?: (msg: string) => void): UpdateAction | null {
  if (!s?.available || !s.latest) return null;
  const v = s.latest.version;
  const install = () => void api.installUpdate().catch((e: Error) => onError?.(e.message));
  if (!s.canInstall) {
    return { label: `Download v${v}`, busy: false, disabled: false, run: () => downloadUpdate(s), note: s.installNote };
  }
  switch (s.phase) {
    case "downloading":
      return { label: `Downloading v${v}… ${Math.round((s.progress ?? 0) * 100)}%`, busy: true, disabled: true, run: () => {} };
    case "ready":
      return { label: "Restart to update", busy: false, disabled: false, run: install, note: `v${v} is downloaded and verified. It also installs when you quit.` };
    case "installing":
      return { label: "Restarting…", busy: true, disabled: true, run: () => {} };
  }
  return { label: `Update to v${v}`, busy: false, disabled: false, run: install };
}
