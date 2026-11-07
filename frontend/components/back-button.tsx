"use client";

import { ArrowLeft } from "lucide-react";
import { Button } from "@/components/ui/button";

/** "← Projects"-style link back to the parent screen. */
export function BackButton({ label, onClick, disabled }: { label: string; onClick: () => void; disabled?: boolean }) {
  return (
    <Button variant="ghost" size="sm" onClick={onClick} disabled={disabled} className="mb-4 -ml-2 text-muted-foreground">
      <ArrowLeft />
      {label}
    </Button>
  );
}
