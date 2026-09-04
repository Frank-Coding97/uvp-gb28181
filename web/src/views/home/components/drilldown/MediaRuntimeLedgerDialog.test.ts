import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import MediaRuntimeLedgerDialog from "./MediaRuntimeLedgerDialog.vue";

const AModal = { props: ["visible"], emits: ["cancel"], template: `<section v-if="visible"><slot /></section>` };
function stream(index: number) {
  return { nodeId: 1, media: { schema: "rtmp", vhost: "v", app: "live", stream: `stream-${index}` }, online: true, aliveSecond: 1, bytesSpeed: index, readerCount: 1, totalReaderCount: 1, originType: 1, recordingMp4: index === 0, recordingHls: false, trackCount: 2 };
}
const streams = Array.from({ length: 21 }, (_, index) => stream(index));
const ledger = {
  streams, viewers: streams, recordings: [streams[0]], sessions: [{ nodeId: 1, nodeName: "node-a", sessions: 3, sampled: true }],
  totals: { streams: 21, viewers: 21, sessions: 3, recordings: 1 }, partial: true, warnings: ["节点明细不完整"], asOf: "now"
};

describe("MediaRuntimeLedgerDialog", () => {
  it("renders at most one page and exposes all four ledgers without a network dependency", async () => {
    const wrapper = mount(MediaRuntimeLedgerDialog, { props: { visible: true, kind: "streams", ledger }, global: { stubs: { AModal } } });
    expect(wrapper.findAll(".media-ledger-list article")).toHaveLength(20);
    expect(wrapper.text()).toContain("节点明细不完整");
    expect(wrapper.findAll("[role='tab']")).toHaveLength(4);
    await wrapper.findAll("[role='tab']")[1].trigger("click");
    expect(wrapper.emitted("kind")?.[0]).toEqual(["viewers"]);
  });

  it("filters the current snapshot instead of rendering every row", async () => {
    const wrapper = mount(MediaRuntimeLedgerDialog, { props: { visible: true, kind: "streams", ledger }, global: { stubs: { AModal } } });
    await wrapper.get("input").setValue("stream-20");
    expect(wrapper.findAll(".media-ledger-list article")).toHaveLength(1);
    expect(wrapper.text()).toContain("stream-20");
  });
});
