import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ZLMStreamClosePreflight } from "@/api/gb28181-zlm-runtime";

const closeMocks = vi.hoisted(() => ({
  preflightOne: vi.fn(),
  preflightBatch: vi.fn(),
  closeOne: vi.fn(),
  closeBatch: vi.fn(),
  forceClose: vi.fn()
}));

vi.mock("@/api/gb28181-zlm-runtime", () => ({
  preflightCloseZLMStream: closeMocks.preflightOne,
  preflightCloseZLMStreams: closeMocks.preflightBatch,
  closeZLMStream: closeMocks.closeOne,
  closeZLMStreams: closeMocks.closeBatch,
  forceCloseZLMStream: closeMocks.forceClose
}));

import {
  buildStreamQuery,
  streamCloseDecision,
  streamIdentityKey,
  streamRouteFilter
} from "./streamManagementState";
import ZLMStreamCloseDialog from "./ZLMStreamCloseDialog.vue";

const media = { schema: "rtsp", vhost: "__defaultVhost__", app: "live/main", stream: "cam 01" };

function preflight(status: ZLMStreamClosePreflight["snapshot"]["status"]): ZLMStreamClosePreflight {
  return {
    target: { nodeId: 7, media },
    snapshot: { status, present: true, presenceKnown: true },
    fingerprint: "sha256:fresh",
    freshPresent: true
  };
}

describe("stream management state", () => {
  beforeEach(() => {
    Object.values(closeMocks).forEach(mock => mock.mockReset());
  });

  it("keeps the complete MediaIdentity in route and server-side filters", () => {
    expect(streamRouteFilter({ nodeId: "7", ...media })).toEqual({ nodeId: 7, ...media });
    expect(buildStreamQuery({ nodeId: 7, ...media, page: 3, pageSize: 50 })).toEqual({
      nodeId: 7,
      ...media,
      page: 3,
      pageSize: 50
    });
    expect(streamIdentityKey(7, media)).toContain("live/main");
  });

  it("allows ordinary close only for backend-confirmed managed streams", () => {
    expect(streamCloseDecision(preflight("managed"), false, false)).toMatchObject({ allowed: true, mode: "normal" });
    expect(streamCloseDecision(preflight("owned"), false, true)).toMatchObject({ allowed: false, mode: "blocked" });
    expect(streamCloseDecision(preflight("unknown"), false, true)).toMatchObject({ allowed: false, mode: "blocked" });
  });

  it("requires the force permission and treats an absent readback as no-op", () => {
    expect(streamCloseDecision(preflight("owned"), true, false)).toMatchObject({ allowed: false, mode: "blocked" });
    expect(streamCloseDecision(preflight("owned"), true, true)).toMatchObject({ allowed: true, mode: "force" });
    expect(streamCloseDecision({ ...preflight("managed"), freshPresent: false }, false, false)).toMatchObject({
      allowed: false,
      mode: "absent"
    });
  });

  it("reuses PlayWindow and typed preview/snapshot/close APIs without direct ZLM access", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/StreamManagement.vue"), "utf8");
    expect(source).toContain("PlayWindow");
    expect(source).toContain("issueZLMPreviewGrant");
    expect(source).toContain("fetchZLMStreamSnapshot");
    expect(source).toContain("ZLMStreamCloseDialog");
    expect(source).not.toContain("index/api");
    expect(source).not.toContain("fetch(");
    expect(source).not.toContain("http.request");
  });

  it("executes ordinary close only after preflight and reuses the exact fingerprint", async () => {
    const preview = preflight("managed");
    closeMocks.preflightOne.mockResolvedValue({ code: 0, data: preview });
    closeMocks.closeOne.mockResolvedValue({
      code: 0,
      data: { target: preview.target, closed: true, alreadyAbsent: false, force: false, uncertain: false, retryable: false }
    });
    const DangerStub = defineComponent({
      props: ["visible", "nodeId", "targetKey", "fingerprint"],
      emits: ["confirm"],
      setup(props, { emit }) {
        return () => props.visible
          ? h("button", { class: "confirm-danger", onClick: () => emit("confirm", { nodeId: props.nodeId, targetKey: props.targetKey, fingerprint: props.fingerprint, reason: "" }) }, "confirm")
          : null;
      }
    });
    const wrapper = mount(ZLMStreamCloseDialog, {
      props: { visible: true, nodeName: "zlm-a", targets: [preview.target] },
      global: { stubs: { ZLMDangerActionDialog: DangerStub, "a-modal": true, "a-spin": true } }
    });
    await flushPromises();
    expect(closeMocks.preflightOne).toHaveBeenCalledWith(7, media);
    await wrapper.get(".confirm-danger").trigger("click");
    await flushPromises();
    expect(closeMocks.closeOne).toHaveBeenCalledWith(7, media, "sha256:fresh");
    expect(wrapper.emitted("done")?.[0]?.[0]).toMatchObject({ closed: 1, uncertain: false });
  });

  it("uses the force endpoint only with permission and an auditable reason", async () => {
    const preview = preflight("owned");
    closeMocks.preflightOne.mockResolvedValue({ code: 0, data: preview });
    closeMocks.forceClose.mockResolvedValue({
      code: 0,
      data: { target: preview.target, closed: true, alreadyAbsent: false, force: true, uncertain: false, retryable: false }
    });
    const DangerStub = defineComponent({
      props: ["visible", "nodeId", "targetKey", "fingerprint"],
      emits: ["confirm"],
      setup(props, { emit }) {
        return () => props.visible
          ? h("button", { class: "confirm-force", onClick: () => emit("confirm", { nodeId: props.nodeId, targetKey: props.targetKey, fingerprint: props.fingerprint, reason: "incident-42" }) }, "force")
          : null;
      }
    });
    const wrapper = mount(ZLMStreamCloseDialog, {
      props: { visible: true, nodeName: "zlm-a", targets: [preview.target], force: true, canForce: true },
      global: { stubs: { ZLMDangerActionDialog: DangerStub, "a-modal": true, "a-spin": true } }
    });
    await flushPromises();
    await wrapper.get(".confirm-force").trigger("click");
    await flushPromises();
    expect(closeMocks.forceClose).toHaveBeenCalledWith(7, media, "sha256:fresh", "incident-42");
    expect(closeMocks.closeOne).not.toHaveBeenCalled();
  });
});
