"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useRef } from "react";

/**
 * Calls onLink with the query parameters whenever the URL has some (e.g.
 * /projects/?id=p1 from the command palette), then strips them from the URL.
 * Works for client-side navigation within the same page too.
 */
export function DeepLink({ onLink }: { onLink: (params: URLSearchParams) => void }) {
  return (
    <Suspense fallback={null}>
      <DeepLinkInner onLink={onLink} />
    </Suspense>
  );
}

function DeepLinkInner({ onLink }: { onLink: (params: URLSearchParams) => void }) {
  const params = useSearchParams();
  const router = useRouter();
  const path = usePathname();
  const cb = useRef(onLink);
  useEffect(() => {
    cb.current = onLink;
  });

  const query = params.toString();
  useEffect(() => {
    if (!query) return;
    cb.current(new URLSearchParams(query));
    router.replace(path, { scroll: false });
  }, [query, path, router]);
  return null;
}
