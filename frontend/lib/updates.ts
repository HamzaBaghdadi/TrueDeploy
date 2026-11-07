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
