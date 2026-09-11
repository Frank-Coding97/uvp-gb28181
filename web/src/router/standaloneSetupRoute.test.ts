import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import {
  STANDALONE_SETUP_PATH,
  STANDALONE_SIP_SETUP_PATH,
  standaloneSetupNavigation
} from "@/api/standalone-setup";
import { staticRoutes } from "./route";

describe("standalone setup route", () => {
  it("is a hidden anonymous static route beside login", () => {
    const route = staticRoutes.find(item => item.path === STANDALONE_SETUP_PATH);
    expect(route).toMatchObject({
      path: "/standalone-setup",
      name: "standalone-setup",
      meta: { hide: true }
    });
    expect(staticRoutes.some(item => item.path === "/login")).toBe(true);
  });

  it("keeps the pending SIP route full-screen and outside the layout", () => {
    const route = staticRoutes.find(item => item.path === STANDALONE_SIP_SETUP_PATH);
    expect(route).toMatchObject({
      path: "/standalone-sip-setup",
      name: "standalone-sip-setup",
      meta: { hide: true, standaloneSetup: true }
    });
    const routeRouter = createRouter({ history: createMemoryHistory(), routes: staticRoutes as never });
    expect(routeRouter.resolve(STANDALONE_SIP_SETUP_PATH).matched.map((record: { name?: unknown }) => record.name)).toEqual(["standalone-sip-setup"]);
  });

  it("probes setup status before applying the legacy refresh-token guard", () => {
    const source = readFileSync(resolve(process.cwd(), "src/router/index.ts"), "utf8");
    expect(source).toContain("readBootstrapTokenOnce(to.query);");
    expect(source.indexOf("readBootstrapTokenOnce(to.query);")).toBeLessThan(
      source.indexOf("const standaloneProbe = await loadStandaloneSetupStatus();")
    );
    expect(source).toContain("const standaloneProbe = await loadStandaloneSetupStatus();");
    expect(source.indexOf("standaloneProbe")).toBeLessThan(source.indexOf("const tokenExist = hasRefreshToken();"));
    expect(source).toContain("if (standaloneNavigation) return next(standaloneNavigation);");
  });

  it("scrubs the token before an unresolved status probe in the real router flow", async () => {
    vi.resetModules();
    const actualApi = await vi.importActual<typeof import("@/api/standalone-setup")>("@/api/standalone-setup");
    actualApi.resetStandaloneSetupStateForTests();

    type StandaloneSetupProbe = import("@/api/standalone-setup").StandaloneSetupProbe;
    let resolveStatus!: (probe: StandaloneSetupProbe) => void;
    const statusProbe = new Promise<StandaloneSetupProbe>(resolve => {
      resolveStatus = resolve;
    });
    vi.doMock("@/api/standalone-setup", () => ({
      ...actualApi,
      loadStandaloneSetupStatus: vi.fn(() => statusProbe)
    }));
    vi.doMock("vue-router", async () => {
      const actualRouter = await vi.importActual<typeof import("vue-router")>("vue-router");
      return { ...actualRouter, createWebHashHistory: actualRouter.createMemoryHistory };
    });

    window.history.replaceState({}, "", "/#/home?bootstrap_token=secret-token");
    const { default: router } = await import("./index");
    const navigation = router.push({ path: "/home", query: { bootstrap_token: "secret-token" } });
    let navigationSettled = false;
    void navigation.then(() => {
      navigationSettled = true;
    });
    await vi.waitFor(() => {
      expect(window.location.href).not.toContain("bootstrap_token");
    });
    expect(navigationSettled).toBe(false);

    resolveStatus({ kind: "standalone", status: { phase: "pending_admin", standalone: true } });
    await navigation;
    expect(router.currentRoute.value.path).toBe(STANDALONE_SETUP_PATH);
    expect(router.currentRoute.value.fullPath).not.toContain("bootstrap_token");
  });

  it("redirects authenticated pending SIP navigation to the full-screen setup route", () => {
    expect(standaloneSetupNavigation("/home", {}, { kind: "standalone", status: { phase: "pending_sip", standalone: true } })).toEqual({
      path: "/standalone-sip-setup",
      query: {}
    });
    expect(
      standaloneSetupNavigation("/home", {}, { kind: "standalone", status: { phase: "pending_sip", standalone: true } }, false)
    ).toBeNull();
  });

  it("keeps the legacy setup route redirect for a completed installation", () => {
    const complete = { kind: "standalone" as const, status: { phase: "complete" as const, standalone: true as const } };

    expect(standaloneSetupNavigation(STANDALONE_SETUP_PATH, {}, complete)).toEqual({ path: "/login" });
    expect(standaloneSetupNavigation(STANDALONE_SIP_SETUP_PATH, {}, complete)).toEqual({ path: "/home" });
  });
});
