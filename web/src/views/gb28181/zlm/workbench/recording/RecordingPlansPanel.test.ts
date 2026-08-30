import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, shallowMount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ listRecordingPlans: vi.fn() }));

vi.mock("@/api/gb28181-recording-plan", async importOriginal => ({
  ...(await importOriginal<typeof import("@/api/gb28181-recording-plan")>()),
  listRecordingPlans: api.listRecordingPlans
}));
vi.mock("@/store/modules/user", () => ({
  useUserStoreHook: () => ({ account: { permissions: ["*:*:*"] } })
}));

import RecordingPlansPanel from "@/views/gb28181/recording-schedules/RecordingPlansPanel.vue";

const implementation = readFileSync(
  resolve(process.cwd(), "src/views/gb28181/recording-schedules/RecordingPlansPanel.vue"),
  "utf8"
);
const bridge = readFileSync(
  resolve(process.cwd(), "src/views/gb28181/zlm/workbench/recording/RecordingPlansPanel.vue"),
  "utf8"
);

describe("RecordingPlansPanel contract", () => {
  beforeEach(() => {
    api.listRecordingPlans.mockReset();
    api.listRecordingPlans.mockResolvedValue({
      code: 0,
      message: "",
      data: { list: [], total: 0, page: 1, pageSize: 10 }
    });
  });

  it("does not request while inactive and loads only the current internal view on activation", async () => {
    const wrapper = shallowMount(RecordingPlansPanel, { props: { active: false } });
    await flushPromises();
    expect(api.listRecordingPlans).not.toHaveBeenCalled();

    await wrapper.setProps({ active: true });
    await flushPromises();
    expect(api.listRecordingPlans).toHaveBeenCalledOnce();
    expect(api.listRecordingPlans).toHaveBeenCalledWith(expect.objectContaining({ page: 1, pageSize: 10 }));
  });

  it("is route independent and starts data only while active", () => {
    expect(implementation).toContain("active?: boolean");
    expect(implementation).toContain("watch(");
    expect(implementation).toContain("props.active");
    expect(implementation).not.toContain("useRoute");
    expect(implementation).not.toContain("snow-fill");
  });

  it("keeps one business implementation for old and canonical routes", () => {
    expect(bridge).toContain("recording-schedules/RecordingPlansPanel.vue");
    expect(bridge).not.toContain("listRecordingPlans");
    expect(bridge).not.toContain("RecordingScheduleEditorDialog");
  });

  it("cuts plan actions by the existing permission anchors", () => {
    expect(implementation).toContain("gb28181:recording-plan:view");
    expect(implementation).toContain("gb28181:recording-plan:maintain");
    expect(implementation).toContain("gb28181:recording-plan:assign");
  });
});
