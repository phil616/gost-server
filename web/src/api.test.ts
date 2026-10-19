import { afterEach, describe, expect, it, vi } from "vitest";
import { api, login, mutation } from "./api";
afterEach(() => vi.unstubAllGlobals());
describe("authenticated API client", () => {
  it("propagates CSRF and uses a unique idempotency key for each mutation", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ username: "admin", csrf: "challenge-token" }),
      });
    vi.stubGlobal("fetch", fetcher);
    await login("admin", "not-stored");
    await mutation("/clients/device/tunnels", { protocol: "tcp" });
    await mutation("/clients/device/tunnels", { protocol: "tcp" });
    const first = fetcher.mock.calls[1][1];
    const second = fetcher.mock.calls[2][1];
    expect(first.headers["X-CSRF-Token"]).toBe("challenge-token");
    expect(first.credentials).toBe("same-origin");
    expect(first.headers["Idempotency-Key"]).not.toBe(
      second.headers["Idempotency-Key"],
    );
  });
  it("redirects an expired session without suppressing the server error", async () => {
    const assign = vi.fn();
    vi.stubGlobal("window", { location: { assign } });
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue({
          ok: false,
          status: 401,
          json: async () => ({ error: "session expired" }),
        }),
    );
    await expect(api("/clients")).rejects.toThrow("session expired");
    expect(assign).toHaveBeenCalledWith("/login");
  });
});
