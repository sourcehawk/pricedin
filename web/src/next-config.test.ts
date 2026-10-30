// @vitest-environment node
import { describe, expect, it } from "vitest";
import nextConfig from "../next.config";

// server/internal/httpapi/httpapi.go: loadTimeout = 2 * time.Minute, and the
// load handler extends its write deadline by 10 seconds more.
const serverLoadWindowMs = 130_000;

describe("next.config", () => {
  it("lets a proxied company load run as long as the API allows it", () => {
    expect(nextConfig.experimental?.proxyTimeout).toBeGreaterThan(serverLoadWindowMs);
  });
});
