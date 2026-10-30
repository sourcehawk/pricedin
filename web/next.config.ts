import path from "node:path";
import type { NextConfig } from "next";

// The workspace root holds the lockfile and the hoisted node_modules that
// the standalone output must trace.
const workspaceRoot = path.join(import.meta.dirname, "..");

const nextConfig: NextConfig = {
  output: "standalone",
  outputFileTracingRoot: workspaceRoot,
  turbopack: { root: workspaceRoot },
  poweredByHeader: false,
  reactStrictMode: true,
  experimental: {
    // proxy.ts forwards /api/* through Next's proxy, which ends a request
    // after 30 s by default. A company load may take 2 minutes on the API,
    // with 10 s more to write its answer (server/internal/httpapi).
    proxyTimeout: 150_000,
  },
};

export default nextConfig;
