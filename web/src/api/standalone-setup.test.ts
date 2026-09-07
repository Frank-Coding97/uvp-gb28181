import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  BOOTSTRAP_TOKEN_HEADER,
  BCRYPT_MAX_PASSWORD_BYTES,
  STANDALONE_SETUP_ADMIN_PATH,
  STANDALONE_SETUP_STATUS_PATH,
  createStandaloneAdmin,
  isBcryptPasswordLengthValid,
  loadStandaloneSetupStatus,
  readBootstrapTokenOnce,
  requestStandaloneSetupStatus,
  resetStandaloneSetupStateForTests,
  standaloneSetupNavigation,
  type Fetcher
} from "./standalone-setup";

function response(status: number, body: unknown): Response {
  return {
    status,
    ok: status >= 200 && status < 300,
    json: vi.fn().mockResolvedValue(body)
  } as unknown as Response;
}

describe("standalone setup API contract", () => {
  beforeEach(() => {
    resetStandaloneSetupStateForTests();
    window.history.replaceState({}, "", "/#/standalone-setup");
  });

  it("requests the status without query credentials and validates the standalone response", async () => {
    const fetcher = vi.fn<Fetcher>().mockResolvedValue(response(200, { phase: "pending_admin", standalone: true }));

    await expect(requestStandaloneSetupStatus(fetcher)).resolves.toEqual({
      kind: "standalone",
      status: { phase: "pending_admin", standalone: true }
    });
    expect(fetcher).toHaveBeenCalledWith(STANDALONE_SETUP_STATUS_PATH, {
      method: "GET",
      cache: "no-store",
      headers: { Accept: "application/json" }
    });
  });

  it("keeps legacy installations unchanged on a status 404 and does not cache a network failure", async () => {
    const legacyFetcher = vi.fn<Fetcher>().mockResolvedValue(response(404, { error: "not found" }));
    await expect(requestStandaloneSetupStatus(legacyFetcher)).resolves.toEqual({ kind: "legacy" });

    const fetcher = vi
      .fn<Fetcher>()
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce(response(200, { phase: "complete", standalone: true }));
    await expect(loadStandaloneSetupStatus(fetcher)).resolves.toEqual({ kind: "unavailable" });
    await expect(loadStandaloneSetupStatus(fetcher)).resolves.toEqual({
      kind: "standalone",
      status: { phase: "complete", standalone: true }
    });
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("caches one successful status probe for the router and maps the setup phases", async () => {
    const fetcher = vi.fn<Fetcher>().mockResolvedValue(response(200, { phase: "pending_admin", standalone: true }));
    await loadStandaloneSetupStatus(fetcher);
    await loadStandaloneSetupStatus(fetcher);
    expect(fetcher).toHaveBeenCalledTimes(1);

    const query = { bootstrap_token: "one-time-token", target: "home" };
    expect(
      standaloneSetupNavigation("/home", query, {
        kind: "standalone",
        status: { phase: "pending_admin", standalone: true }
      })
    ).toEqual({ path: "/standalone-setup", query });
    expect(
      standaloneSetupNavigation("/standalone-setup", query, {
        kind: "standalone",
        status: { phase: "pending_admin", standalone: true }
      })
    ).toBeNull();
    expect(
      standaloneSetupNavigation(
        "/standalone-setup",
        {},
        {
          kind: "standalone",
          status: { phase: "pending_sip", standalone: true }
        }
      )
    ).toEqual({ path: "/login" });
    expect(standaloneSetupNavigation("/home", {}, { kind: "legacy" })).toBeNull();
  });

  it("reads the launcher token once, removes it from the hash, and never persists it", () => {
    window.history.replaceState({}, "", "/#/standalone-setup?bootstrap_token=secret%2Bvalue&target=home");

    expect(readBootstrapTokenOnce()).toBe("secret+value");
    expect(window.location.hash).toBe("#/standalone-setup?target=home");
    expect(window.location.href).not.toContain("bootstrap_token");
    expect(readBootstrapTokenOnce({ bootstrap_token: "different" })).toBe("secret+value");

    const source = readFileSync(resolve(process.cwd(), "src/api/standalone-setup.ts"), "utf8");
    expect(source).not.toMatch(/localStorage|sessionStorage|console\./);
  });

  it("sends the admin payload and one-time token in the locked header", async () => {
    const fetcher = vi.fn<Fetcher>().mockResolvedValue(response(201, { phase: "pending_sip" }));

    await expect(createStandaloneAdmin({ username: "admin", password: "password" }, "secret-token", fetcher)).resolves.toEqual({
      outcome: "created",
      phase: "pending_sip"
    });
    const [path, init] = fetcher.mock.calls[0];
    expect(path).toBe(STANDALONE_SETUP_ADMIN_PATH);
    expect(init?.cache).toBe("no-store");
    expect(init?.method).toBe("POST");
    expect(init?.headers).toEqual({
      Accept: "application/json",
      "Content-Type": "application/json",
      [BOOTSTRAP_TOKEN_HEADER]: "secret-token"
    });
    expect(init?.body).toBe(JSON.stringify({ username: "admin", password: "password" }));
    expect(String(path)).not.toContain("secret-token");
  });

  it("keeps admin failure outcomes generic and handles a repeat as an existing login", async () => {
    for (const status of [401, 403] as const) {
      const fetcher = vi.fn<Fetcher>().mockResolvedValue(response(status, { detail: "credential detail" }));
      await expect(createStandaloneAdmin({ username: "admin", password: "password" }, "secret-token", fetcher)).resolves.toEqual({
        outcome: "unauthorized",
        status
      });
    }

    const repeatFetcher = vi.fn<Fetcher>().mockResolvedValue(response(409, { detail: "already initialized" }));
    await expect(
      createStandaloneAdmin({ username: "admin", password: "password" }, "secret-token", repeatFetcher)
    ).resolves.toEqual({
      outcome: "conflict",
      status: 409
    });
  });

  it("validates bcrypt's UTF-8 byte ceiling before submission", () => {
    expect(BCRYPT_MAX_PASSWORD_BYTES).toBe(72);
    expect(isBcryptPasswordLengthValid("a".repeat(72))).toBe(true);
    expect(isBcryptPasswordLengthValid("a".repeat(73))).toBe(false);
    expect(isBcryptPasswordLengthValid("中".repeat(24))).toBe(true);
    expect(isBcryptPasswordLengthValid("中".repeat(25))).toBe(false);
  });
});
