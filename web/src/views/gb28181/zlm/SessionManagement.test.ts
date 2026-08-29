import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const sessionMocks = vi.hoisted(() => ({ kick: vi.fn() }));

vi.mock("@/api/gb28181-zlm-runtime", () => ({ kickZLMSession: sessionMocks.kick }));

import {
  buildNetworkSessionQuery,
  buildViewerTarget,
  canKickViewer
} from "./sessionManagementState";
import ZLMSessionKickDialog from "./ZLMSessionKickDialog.vue";

describe("session management state", () => {
  beforeEach(() => sessionMocks.kick.mockReset());

  it("keeps network filters and viewer media identity as separate contracts", () => {
    expect(buildNetworkSessionQuery({ peerIp: " 192.0.2.8 ", localPort: "18080", page: 2, pageSize: 20 })).toEqual({
      peerIp: "192.0.2.8",
      localPort: 18080,
      page: 2,
      pageSize: 20
    });
    expect(buildViewerTarget({ schema: " rtsp ", vhost: " __defaultVhost__ ", app: " live ", stream: " cam-1 " })).toEqual({
      schema: "rtsp",
      vhost: "__defaultVhost__",
      app: "live",
      stream: "cam-1"
    });
    expect(buildViewerTarget({ schema: "rtsp", vhost: "", app: "live", stream: "cam-1" })).toBeNull();
  });

  it("shows kick only when both the backend and RBAC allow it", () => {
    expect(canKickViewer({ kickable: true }, true)).toBe(true);
    expect(canKickViewer({ kickable: false }, true)).toBe(false);
    expect(canKickViewer({ kickable: true }, false)).toBe(false);
  });

  it("uses independent typed server queries and lifecycle-aware polling", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/SessionManagement.vue"), "utf8");
    const kickSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/ZLMSessionKickDialog.vue"), "utf8");
    expect(source).toContain("listZLMNetworkSessions");
    expect(source).toContain("listZLMMediaViewers");
    expect(source).toContain("ZLMSessionKickDialog");
    expect(kickSource).toContain("kickZLMSession");
    expect(source).toContain("useZLMRuntimePolling");
    expect(source).toContain("kickable");
    expect(source).not.toContain("fetch(");
    expect(source).not.toContain("http.request");
  });

  it("submits the exact viewer identity and identifier from the backend row", async () => {
    const viewer = {
      nodeId: 7,
      nodeUuid: "uuid-a",
      media: { schema: "rtsp", vhost: "__defaultVhost__", app: "live", stream: "cam-1" },
      identifier: "opaque-session",
      peerIp: "192.0.2.10",
      peerPort: 40000,
      localIp: "192.0.2.20",
      localPort: 18080,
      typeId: "TcpSession",
      kickable: true
    };
    sessionMocks.kick.mockResolvedValue({ code: 0, data: { kicked: true, alreadyDisconnected: false, uncertain: false } });
    const DangerStub = defineComponent({
      props: ["visible", "nodeId", "targetKey"],
      emits: ["confirm"],
      setup(props, { emit }) {
        return () => props.visible
          ? h("button", { class: "confirm-kick", onClick: () => emit("confirm", { nodeId: props.nodeId, targetKey: props.targetKey }) }, "kick")
          : null;
      }
    });
    const wrapper = mount(ZLMSessionKickDialog, {
      props: { visible: true, nodeName: "zlm-a", viewer },
      global: { stubs: { ZLMDangerActionDialog: DangerStub } }
    });
    await wrapper.get(".confirm-kick").trigger("click");
    await flushPromises();
    expect(sessionMocks.kick).toHaveBeenCalledWith(7, viewer.media, "opaque-session");
    expect(wrapper.emitted("done")?.[0]?.[0]).toMatchObject({ kicked: true, uncertain: false });
  });
});
