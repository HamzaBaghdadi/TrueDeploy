"use client";

import { useEffect, useSyncExternalStore } from "react";
import { api, onAlertsChanged, type MonitorStatus } from "@/lib/api";

// One shared copy of the monitor status for the sidebar badge and the Alerts
// page, kept current by the backend's alerts:changed event.
let state: MonitorStatus | null = null;
const listeners = new Set<() => void>();
let started = false;

function set(s: MonitorStatus) {
  state = s;
  listeners.forEach((l) => l());
}

function start() {
  if (started) return;
  started = true;
  onAlertsChanged(set);
  api.monitorStatus().then(set, () => {});
}

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => listeners.delete(l);
};

export function useMonitorStatus(): MonitorStatus | null {
  useEffect(start, []);
  return useSyncExternalStore(
    subscribe,
    () => state,
    () => null,
  );
}

export function setMonitorStatus(s: MonitorStatus) {
  set(s);
}
