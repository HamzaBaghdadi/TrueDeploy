import type { NextConfig } from "next";

// Wails serves the UI from embedded static files, so Next.js must build
// as a static export: no SSR, API routes or server actions. Go is the backend.
const nextConfig: NextConfig = {
  output: "export", // writes the static site to frontend/out, embedded by main.go
  trailingSlash: true,
  images: { unoptimized: true },
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
};

export default nextConfig;
