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
};

export default nextConfig;
