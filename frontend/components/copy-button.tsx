"use client";

import { Check, Copy } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

/** Copies `text`; shows a check for a moment. Icon-only unless `label` is set. */
export function CopyButton({ text, label, size = "icon-xs" }: { text: string; label?: string; size?: "icon-xs" | "icon-sm" | "xs" | "sm" }) {
  const [done, setDone] = useState(false);
  const copy = () =>
    void navigator.clipboard.writeText(text).then(() => {
      setDone(true);
      setTimeout(() => setDone(false), 1500);
    });

  if (label) {
    return (
      <Button variant="ghost" size={size === "icon-xs" ? "xs" : size} onClick={copy} className="text-primary">
        {done ? <Check /> : <Copy />}
        {done ? "Copied" : label}
      </Button>
    );
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size={size} onClick={copy} aria-label="Copy" className="text-muted-foreground hover:text-foreground">
          {done ? <Check className="text-success" /> : <Copy />}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{done ? "Copied" : "Copy"}</TooltipContent>
    </Tooltip>
  );
}
