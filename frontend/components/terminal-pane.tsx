"use client";

import { useEffect, useRef } from "react";
import type { Terminal as XTerm } from "@xterm/xterm";
import { api, onTerminalEvents } from "@/lib/api";

function b64ToBytes(b64: string) {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

/**
 * One xterm.js terminal bound to an SSH shell on a server. It stays mounted
 * while hidden (inactive tab) so the session and scrollback survive.
 */
export function TerminalPane({
  serverId,
  container,
  active,
  connectKey,
  onState,
}: {
  serverId: string;
  /** Opens a shell inside this container instead of on the server. */
  container?: string;
  active: boolean;
  /** Changing it reconnects (new shell in the same pane). */
  connectKey: number;
  onState: (s: "connecting" | "open" | "closed", error?: string) => void;
}) {
  const hostRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<XTerm | null>(null);
  const fitRef = useRef<(() => void) | null>(null);
  const onStateRef = useRef(onState);
  useEffect(() => {
    onStateRef.current = onState;
  });

  useEffect(() => {
    let disposed = false;
    let sessionId: string | null = null;
    // Output that arrives before Open() returns our session ID.
    const early: { sessionId: string; data: string }[] = [];
    let cleanup = () => {};

    (async () => {
      const [{ Terminal }, { FitAddon }, { WebLinksAddon }] = await Promise.all([
        import("@xterm/xterm"),
        import("@xterm/addon-fit"),
        import("@xterm/addon-web-links"),
      ]);
      if (disposed || !hostRef.current) return;

      const css = getComputedStyle(document.documentElement);
      const font = getComputedStyle(document.body).getPropertyValue("--font-geist-mono").trim();
      const term = new Terminal({
        cursorBlink: true,
        fontSize: 13,
        lineHeight: 1.2,
        fontFamily: `${font || "monospace"}, Consolas, monospace`,
        scrollback: 10000,
        allowProposedApi: true,
        theme: {
          background: css.getPropertyValue("--terminal").trim() || "#060a13",
          foreground: css.getPropertyValue("--foreground").trim() || "#e6e9ee",
          cursor: css.getPropertyValue("--brand-cyan").trim() || "#22c3f0",
          selectionBackground: "#1f6ef259",
        },
      });
      const fit = new FitAddon();
      term.loadAddon(fit);
      term.loadAddon(new WebLinksAddon((_e, uri) => void api.openURL(uri)));
      term.open(hostRef.current);
      termRef.current = term;
      const doFit = () => {
        try {
          fit.fit();
        } catch {
          /* hidden: no size yet */
        }
      };
      fitRef.current = doFit;
      doFit();

      // Keystrokes are queued so fast typing/pastes reach the server in order.
      let pending = "";
      let sending = false;
      const flush = async () => {
        sending = true;
        while (pending && sessionId) {
          const chunk = pending;
          pending = "";
          await api.termWrite(sessionId, chunk).catch(() => {});
        }
        sending = false;
      };
      // Windows-terminal style clipboard: Ctrl+C copies a selection (otherwise
      // it's sent as an interrupt), Ctrl+V / Ctrl+Shift+V paste.
      term.attachCustomKeyEventHandler((e) => {
        if (e.type !== "keydown" || !e.ctrlKey) return true;
        const k = e.key.toLowerCase();
        if (k === "c" && (e.shiftKey || term.hasSelection())) {
          void navigator.clipboard.writeText(term.getSelection());
          term.clearSelection();
          return false;
        }
        if (k === "v") {
          e.preventDefault();
          void navigator.clipboard.readText().then((t) => t && term.paste(t));
          return false;
        }
        return true;
      });

      const input = term.onData((d) => {
        pending += d;
        if (!sending && sessionId) void flush();
      });

      let lastSize = "";
      const sendSize = () => {
        const size = `${term.cols}x${term.rows}`;
        if (sessionId && size !== lastSize) {
          lastSize = size;
          void api.termResize(sessionId, term.cols, term.rows).catch(() => {});
        }
      };
      const ro = new ResizeObserver(() => {
        doFit();
        sendSize();
      });
      ro.observe(hostRef.current);

      const off = onTerminalEvents({
        data: (e) => {
          if (sessionId === null) early.push(e);
          else if (e.sessionId === sessionId) term.write(b64ToBytes(e.data));
        },
        exit: (e) => {
          if (e.sessionId !== sessionId) return;
          term.write(`\r\n\x1b[90m[Connection closed${e.error ? `: ${e.error}` : ""}]\x1b[0m\r\n`);
          sessionId = null;
          onStateRef.current("closed", e.error);
        },
      });

      cleanup = () => {
        off();
        ro.disconnect();
        input.dispose();
        if (sessionId) void api.termClose(sessionId);
        term.dispose();
        termRef.current = null;
      };

      onStateRef.current("connecting");
      term.write("\x1b[90mConnecting…\x1b[0m\r\n");
      try {
        const id = container
          ? await api.termOpenContainer(serverId, container, term.cols, term.rows)
          : await api.termOpen(serverId, term.cols, term.rows);
        if (disposed) {
          void api.termClose(id);
          return;
        }
        sessionId = id;
        lastSize = `${term.cols}x${term.rows}`;
        for (const e of early.splice(0)) if (e.sessionId === id) term.write(b64ToBytes(e.data));
        if (pending) void flush();
        onStateRef.current("open");
        term.focus();
      } catch (err) {
        term.write(`\x1b[31m${(err as Error).message}\x1b[0m\r\n`);
        onStateRef.current("closed", (err as Error).message);
      }
    })();

    return () => {
      disposed = true;
      cleanup();
    };
  }, [serverId, container, connectKey]);

  // Refit and focus when this tab becomes visible.
  useEffect(() => {
    if (!active) return;
    const t = setTimeout(() => {
      fitRef.current?.();
      termRef.current?.focus();
    }, 0);
    return () => clearTimeout(t);
  }, [active]);

  return <div ref={hostRef} className={`h-full w-full p-2 ${active ? "" : "hidden"}`} />;
}
