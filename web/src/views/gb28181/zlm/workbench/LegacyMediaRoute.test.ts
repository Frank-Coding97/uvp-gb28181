import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const routing = vi.hoisted(() => ({
  current: { path: "/gb28181/zlm/overview", query: {} as Record<string, unknown> },
  replace: vi.fn()
}));

vi.mock("vue-router", async importOriginal => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRoute: () => routing.current,
  useRouter: () => ({ replace: routing.replace })
}));

import { LEGACY_MEDIA_ROUTE_PATHS } from "./mediaRoutes";
import LegacyMediaRoute from "./LegacyMediaRoute.vue";

describe("LegacyMediaRoute", () => {
  beforeEach(() => {
    routing.current.path = "/gb28181/zlm/overview";
    routing.current.query = {};
    routing.replace.mockReset();
    sessionStorage.clear();
  });

  it("replaces every legacy route exactly once without starting business requests", async () => {
    sessionStorage.setItem("uvp:zlm:selected-node", "8");
    for (const legacyPath of LEGACY_MEDIA_ROUTE_PATHS) {
      routing.current.path = legacyPath.replace(":id", "17");
      routing.current.query = legacyPath.endsWith("/runtime") ? { nodeId: "2" } : {};
      const wrapper = mount(LegacyMediaRoute);
      await flushPromises();

      expect(routing.replace).toHaveBeenCalledOnce();
      expect(routing.replace.mock.calls[0]?.[0]).toMatchObject({ path: expect.stringMatching(/^\/(media|gb28181)\//) });
      wrapper.unmount();
      routing.replace.mockReset();
    }

    const source = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/zlm/workbench/LegacyMediaRoute.vue"),
      "utf8"
    );
    expect(source).not.toMatch(/listZLM|getZLM|createZLM|updateZLM|deleteZLM/);
  });

  it("uses the safe recent node for config and drops sensitive query values", async () => {
    sessionStorage.setItem("uvp:zlm:selected-node", "8");
    routing.current.path = "/gb28181/zlm/config";
    routing.current.query = {
      token: "discard",
      apiSecret: "discard",
      sourceUrl: "rtsp://user:password@example.test/live"
    };
    const wrapper = mount(LegacyMediaRoute);
    await flushPromises();

    expect(routing.replace).toHaveBeenCalledWith({
      path: "/media/nodes/8",
      query: { view: "config" }
    });
    expect(JSON.stringify(routing.replace.mock.calls)).not.toMatch(/secret|password|sourceUrl|token/i);
    wrapper.unmount();
  });

  it("renders a recoverable state for an unsupported path", async () => {
    routing.current.path = "/gb28181/zlm/not-supported";
    const wrapper = mount(LegacyMediaRoute);
    await flushPromises();

    expect(routing.replace).not.toHaveBeenCalled();
    expect(wrapper.get("[role='alert']").text()).toContain("旧地址无法识别");
    wrapper.unmount();
  });
});
