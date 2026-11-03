"use client";

import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";

export type ConfirmOptions = {
  title: string;
  message?: ReactNode;
  /** Optional bullet list under the message, e.g. the steps about to run. */
  items?: string[];
  confirmLabel?: string;
  cancelLabel?: string;
  tone?: "default" | "danger";
};

type Pending = ConfirmOptions & { resolve: (ok: boolean) => void };

const ConfirmContext = createContext<((o: ConfirmOptions) => Promise<boolean>) | null>(null);

/** Returns confirm(options) → Promise<boolean>, rendered as an in-app dialog. */
export function useConfirm() {
  const ctx = useContext(ConfirmContext);
  if (!ctx) throw new Error("useConfirm must be used inside <ConfirmProvider>");
  return ctx;
}

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<Pending | null>(null);

  const confirm = useCallback(
    (o: ConfirmOptions) =>
      new Promise<boolean>((resolve) => {
        setPending({ ...o, resolve });
      }),
    [],
  );

  const close = (ok: boolean) => {
    pending?.resolve(ok);
    setPending(null);
  };

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {pending && <Dialog {...pending} onClose={close} />}
    </ConfirmContext.Provider>
  );
}

function Dialog({
  title,
  message,
  items,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  tone = "default",
  onClose,
}: ConfirmOptions & { onClose: (ok: boolean) => void }) {
  const cancelRef = useRef<HTMLButtonElement>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);
  const danger = tone === "danger";
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  });

  useEffect(() => {
    // Destructive actions focus Cancel, so a stray Enter doesn't delete anything.
    (danger ? cancelRef : confirmRef).current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onCloseRef.current(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [danger]);

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-black/60 p-6 backdrop-blur-[2px] animate-[fade-in_120ms_ease-out]"
      onMouseDown={(e) => e.target === e.currentTarget && onClose(false)}
    >
      <div
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        className="w-full max-w-md overflow-hidden rounded-xl border border-border bg-surface shadow-2xl shadow-black/50 animate-[pop-in_160ms_cubic-bezier(0.2,0.9,0.3,1.2)]"
      >
        <div className="flex gap-4 p-5">
          <div
            className={`grid size-10 shrink-0 place-items-center rounded-full ${
              danger ? "bg-danger/15 text-danger" : "bg-accent/15 text-accent"
            }`}
          >
            {danger ? <WarnIcon /> : <QuestionIcon />}
          </div>
          <div className="min-w-0 flex-1 pt-0.5">
            <h2 id="confirm-title" className="text-[15px] font-semibold">
              {title}
            </h2>
            {message && <div className="mt-1.5 text-sm leading-relaxed text-muted">{message}</div>}
            {items && items.length > 0 && (
              <ul className="mt-3 max-h-56 space-y-1 overflow-auto rounded-md border border-border bg-background p-3 text-[13px]">
                {items.map((it) => (
                  <li key={it} className="flex gap-2">
                    <span className={danger ? "text-danger" : "text-accent"}>•</span>
                    <span>{it}</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
        <div className="flex justify-end gap-2 border-t border-border bg-background/40 px-5 py-3">
          <button
            ref={cancelRef}
            onClick={() => onClose(false)}
            className="rounded-md border border-border px-4 py-2 text-sm hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent"
          >
            {cancelLabel}
          </button>
          <button
            ref={confirmRef}
            onClick={() => onClose(true)}
            className={`rounded-md px-4 py-2 text-sm font-medium focus-visible:outline-2 focus-visible:outline-offset-2 ${
              danger
                ? "bg-danger text-white hover:bg-danger/90 focus-visible:outline-danger"
                : "bg-accent text-accent-ink hover:bg-accent/90 focus-visible:outline-accent"
            }`}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

function WarnIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" fill="none" stroke="currentColor" strokeWidth={2}>
      <path d="M12 9v4m0 4h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function QuestionIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" fill="none" stroke="currentColor" strokeWidth={2}>
      <path d="M5 12l5 5L20 7" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
