"use client";

import { CircleCheck, TriangleAlert } from "lucide-react";
import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";

export type ConfirmOptions = {
  title: string;
  message?: ReactNode;
  /** Optional list under the message, e.g. the steps about to run. */
  items?: string[];
  confirmLabel?: string;
  cancelLabel?: string;
  tone?: "default" | "danger";
};

type Pending = ConfirmOptions & { resolve: (ok: boolean) => void };

const ConfirmContext = createContext<((o: ConfirmOptions) => Promise<boolean>) | null>(null);

/** Returns confirm(options) → Promise<boolean>, shown as a shadcn AlertDialog. */
export function useConfirm() {
  const ctx = useContext(ConfirmContext);
  if (!ctx) throw new Error("useConfirm must be used inside <ConfirmProvider>");
  return ctx;
}

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<Pending | null>(null);
  const [open, setOpen] = useState(false);

  const confirm = useCallback(
    (o: ConfirmOptions) =>
      new Promise<boolean>((resolve) => {
        setPending({ ...o, resolve });
        setOpen(true);
      }),
    [],
  );

  // Resolves once, whichever way the dialog closes (button, Esc, overlay).
  const settle = (ok: boolean) => {
    pending?.resolve(ok);
    setPending((p) => (p ? { ...p, resolve: () => {} } : p));
    setOpen(false);
  };

  const danger = pending?.tone === "danger";
  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      <AlertDialog open={open} onOpenChange={(o) => !o && settle(false)}>
        {pending && (
          <AlertDialogContent className="sm:max-w-md">
            <AlertDialogHeader>
              <AlertDialogMedia className={danger ? "bg-destructive/15 text-destructive" : "bg-primary/15 text-primary"}>
                {danger ? <TriangleAlert /> : <CircleCheck />}
              </AlertDialogMedia>
              <AlertDialogTitle>{pending.title}</AlertDialogTitle>
              {pending.message && <AlertDialogDescription>{pending.message}</AlertDialogDescription>}
            </AlertDialogHeader>
            {pending.items && pending.items.length > 0 && (
              <ul className="max-h-56 space-y-1 overflow-auto rounded-lg border border-border bg-background p-3 text-[13px]">
                {pending.items.map((it) => (
                  <li key={it} className="flex gap-2">
                    <CircleCheck className={`mt-0.5 size-3.5 shrink-0 ${danger ? "text-destructive" : "text-primary"}`} />
                    <span>{it}</span>
                  </li>
                ))}
              </ul>
            )}
            <AlertDialogFooter>
              {/* Destructive dialogs focus Cancel so a stray Enter can't delete anything. */}
              <AlertDialogCancel autoFocus={danger} onClick={() => settle(false)}>
                {pending.cancelLabel ?? "Cancel"}
              </AlertDialogCancel>
              <AlertDialogAction
                autoFocus={!danger}
                variant={danger ? "destructive" : "default"}
                onClick={() => settle(true)}
              >
                {pending.confirmLabel ?? "Confirm"}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        )}
      </AlertDialog>
    </ConfirmContext.Provider>
  );
}
