import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import MediaRuntimeLedgerDialog from "./MediaRuntimeLedgerDialog.vue";

const listNetworkSessionsMock = vi.hoisted(() => vi.fn());
const listStreamViewersMock = vi.hoisted(() => vi.fn());

vi.mock("@/api/gb28181-zlm-runtime", async importOriginal => ({
  ...await importOriginal<typeof import("@/api/gb28181-zlm-runtime")>(),
  listZLMNetworkSessions: listNetworkSessionsMock,
  listZLMStreamViewers: listStreamViewersMock
}));

vi.mock("@/store/modules/user", () => ({
  useUserStoreHook: () => ({ account: { permissions: [] } }),
  registerUserLogoutCleanup: vi.fn()
}));

const AModal = { props: ["visible", "modalClass"], emits: ["cancel"], template: `<section v-if="visible" class="modal-stub" :data-modal-class="modalClass"><slot name="title" /><slot /></section>` };
const stream = {
  key: "1/v/rtp/stream-1", nodeId: 1, nodeName: "node-a", vhost: "v", app: "rtp", streamId: "stream-1",
  deviceId: "device-1", deviceName: "南门摄像机", channelId: "channel-1", channelName: "南门通道",
  protocols: ["hls", "rtmp", "rtsp"], viewers: 2, totalReaders: 4, bytesSpeed: 381_397, aliveSecond: 61,
  recordingMp4: true, recordingHls: false,
  mediaTargets: [
    { schema: "hls", vhost: "v", app: "rtp", stream: "stream-1" },
    { schema: "rtmp", vhost: "v", app: "rtp", stream: "stream-1" },
    { schema: "rtsp", vhost: "v", app: "rtp", stream: "stream-1" }
  ]
};
const ledger = {
  streams: [stream], viewers: [stream], recordings: [stream], sessions: [{ nodeId: 1, nodeName: "node-a", sessions: 3, sampled: true }],
  totals: { streams: 1, viewers: 2, sessions: 3, recordings: 1 }, partial: false, warnings: [], asOf: "now"
};

describe("MediaRuntimeLedgerDialog", () => {
  beforeEach(() => {
    listStreamViewersMock.mockReset().mockImplementation((nodeId, media) => Promise.resolve({
      code: 0,
      data: {
        nodeId,
        nodeUuid: "node-uuid",
        target: media,
        asOf: "now",
        list: [{ nodeId, nodeUuid: "node-uuid", media, identifier: `${media.schema}-viewer`, peerIp: "10.0.0.8", peerPort: 52000, localIp: "10.0.0.2", localPort: 554, typeId: "RtspSession", kickable: true }],
        total: 1,
        page: 1,
        pageSize: 100,
        truncated: false
      }
    }));
    listNetworkSessionsMock.mockReset().mockResolvedValue({
      code: 0,
      data: {
        nodeId: 1,
        nodeUuid: "node-uuid",
        asOf: "now",
        list: [{ nodeId: 1, nodeUuid: "node-uuid", id: "session-1", peerIp: "10.0.0.8", peerPort: 52000, localIp: "10.0.0.2", localPort: 554, identifier: "viewer-1", type: "TcpSession", typeId: "RtspSession" }],
        total: 1,
        page: 1,
        pageSize: 10,
        truncated: false
      }
    });
  });

  it("uses the shared dialog and exposes all four business ledgers", async () => {
    const wrapper = mount(MediaRuntimeLedgerDialog, {
      props: { visible: true, kind: "streams", ledger },
      global: { stubs: { "a-modal": AModal, "a-table": { template: "<div class='table-stub'><slot name='columns' /></div>" }, "a-table-column": true } }
    });
    expect(wrapper.get(".modal-stub").attributes("data-modal-class")).toContain("uvp-system-dialog");
    expect(wrapper.findAll("[role='tab']")).toHaveLength(4);
    expect(wrapper.text()).toContain("在线流1");
    expect(wrapper.get(".media-ledger-tab-count").text()).toBe("1");
    await wrapper.findAll("[role='tab']")[1].trigger("click");
    expect(wrapper.emitted("kind")?.[0]).toEqual(["viewers"]);
  });

  it("renders a professional table with business identity and safe operations", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/home/components/drilldown/MediaRuntimeLedgerDialog.vue"), "utf8");
    for (const label of ["设备 / 通道", "流 ID", "协议", "观看者", "实时码率", "在线时长", "停止点播", "强制结束"]) {
      expect(source).toContain(label);
    }
    expect(source).toContain("<a-table");
    expect(source).toContain("stopPlay");
    expect(source).toContain("preflightCloseZLMRTPServer");
    expect(source).toContain("forceCloseZLMRTPServer");
    expect(source).toContain("listZLMStreamViewers");
    expect(source).toContain("ZLMSessionKickDialog");
    expect(source).not.toContain("media-ledger-list");
  });

  it("uses unified search, icon labels and bounded pagination", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/home/components/drilldown/MediaRuntimeLedgerDialog.vue"), "utf8");
    expect(source).toContain('from "lucide-vue-next"');
    expect(source).toContain('<component :is="item.icon"');
    expect(source).toContain('class="media-ledger-search media-ledger-keyword-search"');
    expect(source).toContain('class="uvp-table-actions media-ledger-actions"');
    expect(source).toContain('class="uvp-table-action uvp-table-action--delete"');
    expect(source).not.toContain('<template #icon><Users :size="13" /></template>');
    expect(source).toContain('<template #icon><CircleStop :size="13" /></template>');
    expect(source).toContain('<template #icon><ShieldAlert :size="13" /></template>');
    expect(source).toContain('<template #icon><LogOut :size="13" /></template>');
    expect(source).toContain("showPageSize: true");
    expect(source).toContain("const pageSize = 10;");
    expect(source).toContain("const networkPageSize = ref(10);");
    expect(source).toContain("pageSizeOptions: [10, 20, 50, 100]");
    expect(source).toContain('y: 480');
    expect(source).not.toContain(".media-ledger-table{overflow:hidden;border:");
    expect(source).toContain('title="操作" :width="220" align="center" fixed="right"');
    expect(source).toContain('title="操作" :width="100" align="center" fixed="right"');
    expect(source).not.toContain('<a-button size="small" :disabled="record.viewers <= 0"');
    expect(source).not.toContain("快照时间");
    expect(source).not.toContain("ledger.asOf");
  });

  it("loads viewer sessions directly into the viewers ledger", async () => {
    mount(MediaRuntimeLedgerDialog, {
      props: { visible: true, kind: "viewers", ledger },
      global: {
        stubs: {
          "a-modal": AModal,
          "s-layout-search": { template: "<div><slot name='fields' /></div>" },
          "a-table": { template: "<div class='table-stub'><slot name='columns' /></div>" },
          "a-table-column": true
        }
      }
    });
    await flushPromises();
    expect(listStreamViewersMock).toHaveBeenCalledTimes(3);
    expect(listStreamViewersMock).toHaveBeenCalledWith(1, stream.mediaTargets[0], { page: 1, pageSize: 100 });

    const source = readFileSync(resolve(process.cwd(), "src/views/home/components/drilldown/MediaRuntimeLedgerDialog.vue"), "utf8");
    expect(source).toContain('v-else-if="kind === \'viewers\'"');
    expect(source).toContain(':scroll="{ x: 1080, y: 480 }"');
    for (const label of ["设备 / 通道", "流 / 协议", "远端地址", "本地地址", "会话类型", "会话标识"]) expect(source).toContain(label);
    expect(source).not.toContain("viewerVisible");
    expect(source).not.toContain("openViewers");
    expect(source).not.toContain(">观看会话</span>");
  });

  it("loads paged network-session details for the first sampled node", async () => {
    mount(MediaRuntimeLedgerDialog, {
      props: { visible: true, kind: "sessions", ledger },
      global: {
        stubs: {
          "a-modal": AModal,
          "s-layout-search": { template: "<div><slot name='fields' /><slot name='actions' /></div>" },
          "a-table": { template: "<div class='table-stub'><slot name='columns' /></div>" },
          "a-table-column": true
        }
      }
    });
    await flushPromises();
    expect(listNetworkSessionsMock).toHaveBeenCalledWith(1, { page: 1, pageSize: 10 });

    const source = readFileSync(resolve(process.cwd(), "src/views/home/components/drilldown/MediaRuntimeLedgerDialog.vue"), "utf8");
    for (const label of ["会话 ID", "远端地址", "本地地址", "连接类型", "会话类型", "会话标识"]) expect(source).toContain(label);
    expect(source).toContain('"mediakit::HttpSession": "HTTP 会话"');
    expect(source).toContain('"mediakit::RtpSession": "RTP 会话"');
    expect(source).toContain("networkSessionTypeLabel(record.typeId)");
    expect(source).toContain(':title="record.typeId || undefined"');
    expect(source).toContain('@page-change="changeNetworkPage"');
    expect(source).toContain('@page-size-change="changeNetworkPageSize"');
    expect(source).toContain('class="media-ledger-search network-session-search"');
    expect(source).toContain('.network-session-search :deep(.uvp-search-panel__fields){flex:1;width:auto}');
  });
});
