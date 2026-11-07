// Go's zero time.Time serialises as year 1; treat it as "not set".
export function isSet(t?: string) {
  return !!t && !t.startsWith("0001-");
}

export function timeAgo(t?: string) {
  if (!isSet(t)) return "";
  const s = Math.max(0, (Date.now() - new Date(t!).getTime()) / 1000);
  if (s < 45) return "just now";
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)} h ago`;
  if (s < 86400 * 7) return `${Math.round(s / 86400)} d ago`;
  return new Date(t!).toLocaleDateString();
}

export function duration(from?: string, to?: string) {
  if (!isSet(from) || !isSet(to)) return "";
  const s = Math.round((new Date(to!).getTime() - new Date(from!).getTime()) / 1000);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

export function bytes(n: number) {
  if (!n || n < 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n >= 100 || i === 0 ? Math.round(n) : n.toFixed(1)} ${units[i]}`;
}

export function uptime(sec: number) {
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  return d > 0 ? `${d}d ${h}h` : h > 0 ? `${h}h ${m}m` : `${m}m`;
}
