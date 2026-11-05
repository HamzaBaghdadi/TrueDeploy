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
