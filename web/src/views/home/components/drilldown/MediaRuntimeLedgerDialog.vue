<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { CircleStop, LogOut, Network, Radio, RotateCcw, Search, ShieldAlert, Users, Video } from "lucide-vue-next";
import { stopPlay } from "@/api/gb28181";
import { forceCloseZLMRTPServer, preflightCloseZLMRTPServer, type ZLMRTPServerCloseRequest } from "@/api/gb28181-zlm-ingress";
import {
  listZLMNetworkSessions,
  type ZLMMediaIdentity,
  type ZLMNetworkSession,
  type ZLMNetworkSessionQuery,
  type ZLMOwnershipPreflight,
  type ZLMStreamViewer
} from "@/api/gb28181-zlm-runtime";
import { hasPermission } from "@/hooks/usePermission";
import SNumberField from "@/components/s-number-field/index.vue";
import ZLMDangerActionDialog from "@/views/gb28181/zlm/components/ZLMDangerActionDialog.vue";
import { formatZLMByteRate, formatZLMDuration, zlmErrorPresentation } from "@/views/gb28181/zlm/components/zlmFormatters";
import ZLMSessionKickDialog from "@/views/gb28181/zlm/ZLMSessionKickDialog.vue";
import type { MediaBusinessStream, MediaRuntimeLedger, MediaRuntimeLedgerKind } from "../../dashboardDrilldownState";
import { useZLMSessionTypeLabel } from "./useZLMSessionTypeDict";

/**
 * 观众明细行：一条播放器连接。
 * ⛔ 这张表不再是主表，主表按远端 IP 聚合（一人一行），连接只出现在展开行里。
 */
interface AudienceConnectionRow {
  key: string;
  nodeId: number;
  nodeName: string;
  media: ZLMMediaIdentity;
  identifier: string;
  typeId: string;
  peerIp: string;
  peerPort: number;
  localIp: string;
  localPort: number;
  businessStream: MediaBusinessStream | null;
  kickable: boolean;
}

/** 主表行：同一个远端 IP 算一位观众，看几路由 streams 决定。 */
interface AudienceViewerRow {
  key: string;
  peerIp: string;
  connections: number;
  streams: MediaBusinessStream[];
  details: AudienceConnectionRow[];
}

const props = defineProps<{ visible: boolean; kind: MediaRuntimeLedgerKind; ledger: MediaRuntimeLedger }>();
const emit = defineEmits<{ close: []; kind: [value: MediaRuntimeLedgerKind]; refresh: [] }>();
const query = ref("");
const stopTarget = ref<MediaBusinessStream | null>(null);
const stopping = ref(false);
const forceTarget = ref<MediaBusinessStream | null>(null);
const forcePreview = ref<ZLMOwnershipPreflight | null>(null);
const forceLoading = ref(false);
const forceError = ref("");
const forceBusy = ref(false);
const expandedViewerKeys = ref<string[]>([]);
const kickVisible = ref(false);
const kickTarget = ref<ZLMStreamViewer | null>(null);
const pageSize = 10;
const networkRows = ref<ZLMNetworkSession[]>([]);
const networkLoading = ref(false);
const networkError = ref("");
const networkTruncated = ref(false);
const networkNodeId = ref<number>();
const networkPeerIp = ref("");
const networkLocalPort = ref<number | null>(null);
const networkPage = ref(1);
const networkPageSize = ref(10);
const networkTotal = ref(0);
let networkGeneration = 0;
const kinds = [
  { key: "streams" as const, label: "在线流", icon: Radio },
  { key: "viewers" as const, label: "观看者", icon: Users },
  { key: "sessions" as const, label: "网络会话", icon: Network },
  { key: "recordings" as const, label: "录制中", icon: Video }
];
const title = computed(() => `流媒体运行态 · ${kinds.find(item => item.key === props.kind)?.label ?? "在线流"}`);
const sourceRows = computed(() => (props.kind === "recordings" ? props.ledger.recordings : props.ledger.streams));
const streamRows = computed(() => {
  const keyword = query.value.trim().toLowerCase();
  if (!keyword) return sourceRows.value;
  return sourceRows.value.filter(item =>
    [item.deviceName, item.deviceId, item.channelName, item.channelId, item.streamId, item.app, item.nodeName, ...item.protocols]
      .join(" ")
      .toLowerCase()
      .includes(keyword)
  );
});
const viewerRows = computed<AudienceViewerRow[]>(() =>
  props.ledger.audience.viewers.map(viewer => {
    const details: AudienceConnectionRow[] = viewer.details.map(detail => {
      const media = { ...detail.media };
      const matched = streamIndex.value.get(businessStreamKey(detail.nodeId, media));
      return {
        key: `${detail.nodeId}\u001f${detail.media.schema}\u001f${detail.identifier}`,
        nodeId: detail.nodeId,
        nodeName: matched?.nodeName ?? `节点 #${detail.nodeId}`,
        media,
        identifier: detail.identifier,
        typeId: detail.typeId ?? "",
        peerIp: detail.peerIp,
        peerPort: detail.peerPort,
        localIp: detail.localIp ?? "",
        localPort: detail.localPort,
        businessStream: matched ?? null,
        // 与后端 viewerKickable 同判据：无会话标识或 udp 类型不可踢
        kickable:
          !!detail.identifier &&
          !String(detail.typeId ?? "")
            .toLowerCase()
            .includes("udp")
      };
    });
    const streams: MediaBusinessStream[] = [];
    for (const ref of viewer.streams) {
      const matched = streamIndex.value.get(businessStreamKey(ref.nodeId, ref.media));
      if (matched && !streams.includes(matched)) streams.push(matched);
    }
    return {
      key: viewer.peerIp || `unknown:${details[0]?.identifier ?? viewer.connections}`,
      peerIp: viewer.peerIp,
      connections: viewer.connections,
      streams,
      details
    };
  })
);
const filteredViewerRows = computed(() => {
  const keyword = query.value.trim().toLowerCase();
  if (!keyword) return viewerRows.value;
  return viewerRows.value.filter(item =>
    [
      item.peerIp,
      ...item.streams.flatMap(stream => [
        stream.deviceName,
        stream.deviceId,
        stream.channelName,
        stream.channelId,
        stream.streamId
      ]),
      ...item.details.flatMap(detail => [detail.identifier, detail.typeId, detail.media.schema, String(detail.peerPort)])
    ]
      .join(" ")
      .toLowerCase()
      .includes(keyword)
  );
});
const filteredCount = computed(() => streamRows.value.length);
const pagination = computed(() => ({
  pageSize,
  showTotal: true,
  showJumper: filteredCount.value > pageSize,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50, 100]
}));
const networkPagination = computed(() => ({
  current: networkPage.value,
  pageSize: networkPageSize.value,
  total: networkTotal.value,
  showTotal: true,
  showJumper: networkTotal.value > networkPageSize.value,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50, 100]
}));
const viewerPagination = computed(() => ({
  pageSize,
  showTotal: true,
  showJumper: filteredViewerRows.value.length > pageSize,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50, 100]
}));
const canForce = computed(() => hasPermission("gb28181:zlm:rtp:force-close"));
const canKick = computed(() => hasPermission("gb28181:zlm:session:kick"));
const forceImpacts = computed(
  () =>
    (forcePreview.value?.snapshot.impacts
      ?.map(impact => impact.reason || impact.owner || [impact.resourceType, impact.resourceKey].filter(Boolean).join(" / "))
      .filter(Boolean) as string[]) ?? []
);

watch(
  () => [props.kind, props.visible],
  () => {
    query.value = "";
    closeSecondaryDialogs();
    expandedViewerKeys.value = [];
    networkGeneration += 1;
    networkLoading.value = false;
    if (!props.visible) return;
    // 观众表直接读 ledger.audience（后端已按远端 IP 聚合），不再逐流拉会话。
    if (props.kind !== "sessions") return;
    const preferredNode = props.ledger.sessions.find(item => item.sampled) ?? props.ledger.sessions[0];
    networkNodeId.value = preferredNode?.nodeId;
    networkPeerIp.value = "";
    networkLocalPort.value = null;
    networkPage.value = 1;
    void loadNetworkSessions();
  },
  { immediate: true }
);

function closeSecondaryDialogs() {
  stopTarget.value = null;
  forceTarget.value = null;
  forcePreview.value = null;
  forceError.value = "";
  kickVisible.value = false;
  kickTarget.value = null;
}

function displayName(value: string, fallback: string) {
  return value.trim() || fallback;
}

/** 业务流索引：观众明细只有 media 身份，设备/通道名要从 ledger.streams 里反查。 */
function businessStreamKey(nodeId: number, media: { vhost: string; app: string; stream: string }) {
  return `${nodeId}\u001f${media.vhost}\u001f${media.app}\u001f${media.stream}`;
}
const streamIndex = computed(() => {
  const index = new Map<string, MediaBusinessStream>();
  for (const item of props.ledger.streams) {
    index.set(businessStreamKey(item.nodeId, { vhost: item.vhost, app: item.app, stream: item.streamId }), item);
  }
  return index;
});
function recordingText(row: MediaBusinessStream) {
  return row.recordingMp4 ? "MP4" : "未录制";
}
function rtpRequest(row: MediaBusinessStream): ZLMRTPServerCloseRequest {
  return { nodeId: row.nodeId, vhost: row.vhost, app: row.app, stream: row.streamId };
}
/** 会话类型文案：字典 `zlm_session_type` 驱动（兜底见 `zlmSessionType.ts`）。 */
const networkSessionTypeLabel = useZLMSessionTypeLabel();

async function confirmStop() {
  const target = stopTarget.value;
  if (!target?.streamId || !target.channelId) return;
  stopping.value = true;
  try {
    const response = await stopPlay(target.streamId);
    if (response.code !== 0 || !response.data?.released) throw new Error(response.message || "后端未确认点播已停止");
    stopTarget.value = null;
    Message.success("点播已停止");
    emit("refresh");
  } catch (error) {
    Message.error(zlmErrorPresentation(error).label);
  } finally {
    stopping.value = false;
  }
}

async function openForce(row: MediaBusinessStream) {
  if (!canForce.value) return;
  forceTarget.value = row;
  forcePreview.value = null;
  forceError.value = "";
  forceLoading.value = true;
  try {
    const response = await preflightCloseZLMRTPServer(row.nodeId, rtpRequest(row));
    if (response.code !== 0 || !response.data) throw new Error(response.message || "强制结束预检失败");
    if (!response.data.snapshot.presenceKnown || !response.data.snapshot.present)
      throw new Error("后端未确认该业务流仍有 RTP 接收服务，未执行强制结束");
    forcePreview.value = response.data;
  } catch (error) {
    forceError.value = zlmErrorPresentation(error).label;
  } finally {
    forceLoading.value = false;
  }
}

function closeForce() {
  if (forceBusy.value) return;
  forceTarget.value = null;
  forcePreview.value = null;
  forceError.value = "";
}

async function confirmForce(payload: { nodeId: number; targetKey: string; fingerprint: string; reason: string }) {
  const target = forceTarget.value;
  const preview = forcePreview.value;
  if (
    !target ||
    !preview ||
    payload.nodeId !== target.nodeId ||
    payload.targetKey !== target.key ||
    payload.fingerprint !== preview.fingerprint
  ) {
    Message.error("业务流快照已变化，未执行强制结束");
    closeForce();
    return;
  }
  forceBusy.value = true;
  try {
    const response = await forceCloseZLMRTPServer(target.nodeId, rtpRequest(target), payload.reason);
    if (response.code !== 0 || !response.data || (!response.data.released && !response.data.alreadyReleased))
      throw new Error(response.message || "后端未确认业务流已结束");
    forceTarget.value = null;
    forcePreview.value = null;
    Message.success(response.data.alreadyReleased ? "业务流已结束" : "业务流已强制结束");
    emit("refresh");
  } catch (error) {
    Message.error(zlmErrorPresentation(error).label);
  } finally {
    forceBusy.value = false;
  }
}

function openKick(detail: AudienceConnectionRow) {
  if (!canKick.value || !detail.kickable) return;
  // ⛔ 强退接口按「一条会话」定位，聚合成一行后必须传具体连接，不能传整位观众。
  kickTarget.value = {
    nodeId: detail.nodeId,
    nodeName: detail.nodeName,
    nodeUuid: "",
    media: detail.media,
    identifier: detail.identifier,
    peerIp: detail.peerIp,
    peerPort: detail.peerPort,
    localIp: detail.localIp,
    localPort: detail.localPort,
    typeId: detail.typeId,
    kickable: detail.kickable
  };
  kickVisible.value = true;
}

function kickDone() {
  kickVisible.value = false;
  emit("refresh");
}

async function loadNetworkSessions() {
  const nodeId = networkNodeId.value;
  if (!nodeId) {
    networkRows.value = [];
    networkTotal.value = 0;
    networkError.value = "没有可读取的媒体节点";
    return;
  }
  const request: ZLMNetworkSessionQuery = { page: networkPage.value, pageSize: networkPageSize.value };
  const peerIp = networkPeerIp.value.trim();
  if (peerIp) request.peerIp = peerIp;
  if (networkLocalPort.value) request.localPort = networkLocalPort.value;
  const generation = ++networkGeneration;
  networkLoading.value = true;
  networkError.value = "";
  try {
    const response = await listZLMNetworkSessions(nodeId, request);
    if (generation !== networkGeneration) return;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "网络会话读取失败");
    networkRows.value = response.data.list;
    networkTotal.value = response.data.total;
    networkPage.value = response.data.page;
    networkPageSize.value = response.data.pageSize;
    networkTruncated.value = response.data.truncated;
  } catch (error) {
    if (generation !== networkGeneration) return;
    networkRows.value = [];
    networkTotal.value = 0;
    networkTruncated.value = false;
    networkError.value = zlmErrorPresentation(error).label;
  } finally {
    if (generation === networkGeneration) networkLoading.value = false;
  }
}

function queryNetworkSessions() {
  networkPage.value = 1;
  void loadNetworkSessions();
}

function resetNetworkSessions() {
  networkPeerIp.value = "";
  networkLocalPort.value = null;
  networkPage.value = 1;
  void loadNetworkSessions();
}

function changeNetworkNode() {
  networkPage.value = 1;
  void loadNetworkSessions();
}

function changeNetworkPage(page: number) {
  networkPage.value = page;
  void loadNetworkSessions();
}

function changeNetworkPageSize(value: number) {
  networkPageSize.value = value;
  networkPage.value = 1;
  void loadNetworkSessions();
}
</script>

<template>
  <a-modal
    :visible="visible"
    modal-class="uvp-system-dialog media-runtime-ledger-modal"
    :title="title"
    :width="1180"
    :footer="false"
    unmount-on-close
    @cancel="emit('close')"
  >
    <div class="media-ledger-dialog">
      <div class="media-ledger-tabs" role="tablist" aria-label="运行态台账类型">
        <button
          v-for="item in kinds"
          :key="item.key"
          type="button"
          role="tab"
          :aria-selected="kind === item.key"
          :class="{ active: kind === item.key }"
          @click="emit('kind', item.key)"
        >
          <component :is="item.icon" :size="15" />
          <span>{{ item.label }}</span>
          <span class="media-ledger-tab-count">{{ ledger.totals[item.key] }}</span>
        </button>
      </div>
      <s-layout-search v-if="kind === 'sessions'" class="media-ledger-search network-session-search">
        <template #fields>
          <a-select
            v-model="networkNodeId"
            class="network-node-filter"
            placeholder="选择媒体节点"
            aria-label="选择媒体节点"
            @change="changeNetworkNode"
          >
            <a-option v-for="node in ledger.sessions" :key="node.nodeId" :value="node.nodeId"
              >{{ node.nodeName }}{{ node.sampled ? "" : "（未采样）" }}</a-option
            >
          </a-select>
          <a-input-search
            v-model="networkPeerIp"
            class="network-peer-filter"
            allow-clear
            placeholder="远端 IP"
            aria-label="按远端 IP 筛选"
            @search="queryNetworkSessions"
          />
          <s-number-field
            v-model="networkLocalPort"
            class="network-port-filter"
            :min="1"
            :max="65535"
            placeholder="本地端口"
            aria-label="按本地端口筛选"
            @keydown.enter="queryNetworkSessions"
          />
        </template>
        <template #actions
          ><a-button type="primary" :loading="networkLoading" @click="queryNetworkSessions"
            ><template #icon><Search :size="14" /></template>查询</a-button
          ><a-button class="uvp-btn-neutral" :disabled="networkLoading" @click="resetNetworkSessions"
            ><template #icon><RotateCcw :size="14" /></template>重置</a-button
          ></template
        >
      </s-layout-search>
      <s-layout-search v-else class="media-ledger-search media-ledger-keyword-search">
        <template #fields
          ><a-input-search
            v-model="query"
            class="media-ledger-keyword"
            allow-clear
            :placeholder="kind === 'viewers' ? '搜索设备、通道、流 ID、会话标识或地址' : '搜索设备、通道、流 ID 或节点'"
            aria-label="搜索运行态台账"
        /></template>
      </s-layout-search>
      <div v-if="ledger.partial" class="media-ledger-warning" role="status">
        当前快照覆盖不完整<span v-if="ledger.warnings.length">：{{ ledger.warnings.join("；") }}</span>
      </div>
      <div v-if="kind === 'viewers' && !ledger.audience.available" class="media-ledger-warning" role="alert">
        当前后端未提供按地址聚合的观众口径，已回退为「观看连接数」。
      </div>
      <div v-if="kind === 'viewers' && ledger.audience.truncated" class="media-ledger-warning" role="status">
        有人的流超过单轮采集上限，当前只统计已采集部分。
      </div>
      <div v-if="kind === 'sessions' && networkError" class="media-ledger-warning" role="alert">{{ networkError }}</div>
      <div v-if="kind === 'sessions' && networkTruncated" class="media-ledger-warning" role="status">
        节点返回的会话列表已达到采集上限，当前分页仅覆盖已采集数据。
      </div>

      <a-table
        v-if="kind === 'streams' || kind === 'recordings'"
        :data="streamRows"
        :pagination="pagination"
        row-key="key"
        class="uvp-data-table media-ledger-table"
        :scroll="{ x: kind === 'recordings' ? 1320 : 1220, y: 480 }"
      >
        <template #columns>
          <a-table-column title="设备" :width="200"
            ><template #cell="{ record }"
              ><strong>{{ displayName(record.deviceName, "未绑定设备") }}</strong
              ><small class="mono">{{ record.deviceId || "—" }}</small></template
            ></a-table-column
          >
          <a-table-column title="通道" :width="200"
            ><template #cell="{ record }"
              ><strong>{{ displayName(record.channelName, "未绑定通道") }}</strong
              ><small class="mono">{{ record.channelId || "—" }}</small></template
            ></a-table-column
          >
          <a-table-column title="流 ID" :width="150"
            ><template #cell="{ record }"
              ><a-tooltip :content="record.streamId" position="top"
                ><strong class="mono">{{ record.streamId }}</strong></a-tooltip
              ><small>{{ record.app }} · {{ record.nodeName }}</small></template
            ></a-table-column
          >
          <a-table-column title="协议" :width="150"
            ><template #cell="{ record }"
              ><div class="protocols">
                <a-tag v-for="protocol in record.protocols" :key="protocol" size="small">{{ protocol.toUpperCase() }}</a-tag>
              </div></template
            ></a-table-column
          >
          <a-table-column title="观看者" :width="75"
            ><template #cell="{ record }"
              ><span class="numeric">{{ record.viewers }}</span></template
            ></a-table-column
          >
          <a-table-column title="实时码率" :width="105"
            ><template #cell="{ record }"
              ><span class="numeric">{{ formatZLMByteRate(record.bytesSpeed) }}</span></template
            ></a-table-column
          >
          <a-table-column title="在线时长" :width="100"
            ><template #cell="{ record }">{{ formatZLMDuration(record.aliveSecond) }}</template></a-table-column
          >
          <a-table-column v-if="kind === 'recordings'" title="录制" :width="100"
            ><template #cell="{ record }">{{ recordingText(record) }}</template></a-table-column
          >
          <a-table-column title="操作" :width="220" align="center" fixed="right"
            ><template #cell="{ record }"
              ><div class="uvp-table-actions media-ledger-actions">
                <a-link class="uvp-table-action" status="warning" :disabled="!record.channelId" @click="stopTarget = record"
                  ><template #icon><CircleStop :size="13" /></template><span>停止点播</span></a-link
                >
                <a-link
                  :disabled="!canForce"
                  class="uvp-table-action uvp-table-action--delete"
                  status="danger"
                  @click="openForce(record)"
                  ><template #icon><ShieldAlert :size="13" /></template><span>强制结束</span></a-link
                >
              </div></template
            ></a-table-column
          >
        </template>
        <template #empty><div class="media-ledger-empty">当前条件下暂无业务流</div></template>
      </a-table>

      <a-table
        v-else-if="kind === 'viewers'"
        v-model:expanded-keys="expandedViewerKeys"
        :data="filteredViewerRows"
        :pagination="viewerPagination"
        row-key="key"
        :expandable="{ width: 36 }"
        class="uvp-data-table media-ledger-table"
        :scroll="{ x: 960, y: 480 }"
      >
        <template #columns>
          <a-table-column title="远端地址" :width="200"
            ><template #cell="{ record }"
              ><span class="mono">{{ record.peerIp || "来源未知" }}</span
              ><small v-if="!record.peerIp">上游未返回对端地址</small></template
            ></a-table-column
          >
          <a-table-column title="观看路数" :width="100" align="center"
            ><template #cell="{ record }"
              ><span class="numeric">{{ record.streams.length }}</span></template
            ></a-table-column
          >
          <a-table-column title="连接数" :width="90" align="center"
            ><template #cell="{ record }"
              ><span class="numeric">{{ record.connections }}</span></template
            ></a-table-column
          >
          <a-table-column title="设备 / 通道" :width="380"
            ><template #cell="{ record }"
              ><div v-if="record.streams.length" class="audience-streams">
                <div v-for="stream in record.streams.slice(0, 2)" :key="stream.key" class="audience-stream">
                  <strong>{{ displayName(stream.deviceName, "未绑定设备") }}</strong
                  ><small>{{ displayName(stream.channelName, "未绑定通道") }} · {{ stream.streamId }}</small>
                </div>
                <small v-if="record.streams.length > 2">另有 {{ record.streams.length - 2 }} 路</small>
              </div>
              <small v-else class="mono">未匹配到业务流</small></template
            ></a-table-column
          >
          <a-table-column title="展开" :width="120" align="center"
            ><template #cell="{ record }"
              ><span class="audience-hint">查看 {{ record.connections }} 条连接</span></template
            ></a-table-column
          >
        </template>
        <template #expand-row="{ record }">
          <div class="audience-connections">
            <div v-for="detail in record.details" :key="detail.key" class="audience-connection">
              <span class="mono audience-schema">{{ detail.media.schema.toUpperCase() }}</span>
              <span class="mono audience-stream-id">{{ detail.businessStream?.streamId || detail.media.stream }}</span>
              <span class="mono">端口 {{ detail.peerPort || "—" }}</span>
              <a-tag size="small" class="media-ledger-session-type" :title="detail.typeId || undefined">{{
                networkSessionTypeLabel(detail.typeId)
              }}</a-tag>
              <span class="mono audience-identifier">{{ detail.identifier }}</span>
              <a-link
                v-if="canKick"
                class="uvp-table-action uvp-table-action--delete"
                status="danger"
                :disabled="!detail.kickable"
                @click="openKick(detail)"
                ><template #icon><LogOut :size="13" /></template><span>强退</span></a-link
              >
            </div>
          </div>
        </template>
        <template #empty><div class="media-ledger-empty">当前没有正在观看的客户端</div></template>
      </a-table>

      <a-table
        v-else
        :data="networkRows"
        :loading="networkLoading"
        :pagination="networkPagination"
        row-key="id"
        class="uvp-data-table media-ledger-table"
        :scroll="{ x: 1140, y: 480 }"
        @page-change="changeNetworkPage"
        @page-size-change="changeNetworkPageSize"
      >
        <template #columns>
          <a-table-column title="会话 ID" :width="240" :ellipsis="true" :tooltip="true"
            ><template #cell="{ record }"
              ><span class="mono">{{ record.id || "—" }}</span></template
            ></a-table-column
          >
          <a-table-column title="远端地址" :width="190"
            ><template #cell="{ record }"
              ><span class="mono">{{ record.peerIp || "—" }}:{{ record.peerPort || "—" }}</span></template
            ></a-table-column
          >
          <a-table-column title="本地地址" :width="190"
            ><template #cell="{ record }"
              ><span class="mono">{{ record.localIp || "—" }}:{{ record.localPort || "—" }}</span></template
            ></a-table-column
          >
          <a-table-column title="连接类型" :width="170"
            ><template #cell="{ record }">{{ record.type || "—" }}</template></a-table-column
          >
          <a-table-column title="会话类型" :width="170"
            ><template #cell="{ record }"
              ><a-tag size="small" class="media-ledger-session-type" :title="record.typeId || undefined">{{
                networkSessionTypeLabel(record.typeId)
              }}</a-tag></template
            ></a-table-column
          >
          <a-table-column title="会话标识" :width="180" :ellipsis="true" :tooltip="true"
            ><template #cell="{ record }"
              ><span class="mono">{{ record.identifier || "—" }}</span></template
            ></a-table-column
          >
        </template>
        <template #empty><div class="media-ledger-empty">当前节点暂无符合条件的网络会话</div></template>
      </a-table>
    </div>
  </a-modal>

  <a-modal
    :visible="!!stopTarget"
    modal-class="uvp-system-dialog"
    title="停止点播"
    :width="520"
    :footer="false"
    :mask-closable="false"
    @cancel="stopTarget = null"
  >
    <div v-if="stopTarget" class="action-confirm">
      <p>将向设备发送 BYE 并释放该通道当前媒体会话。</p>
      <dl>
        <div>
          <dt>设备 / 通道</dt>
          <dd>
            {{ displayName(stopTarget.deviceName, stopTarget.deviceId) }} /
            {{ displayName(stopTarget.channelName, stopTarget.channelId) }}
          </dd>
        </div>
        <div>
          <dt>流 ID</dt>
          <dd class="mono">{{ stopTarget.streamId }}</dd>
        </div>
      </dl>
      <footer>
        <a-button :disabled="stopping" @click="stopTarget = null">取消</a-button
        ><a-button type="primary" status="danger" :loading="stopping" @click="confirmStop">确认停止</a-button>
      </footer>
    </div>
  </a-modal>

  <ZLMDangerActionDialog
    v-if="forceTarget && forcePreview"
    :visible="true"
    :node-id="forceTarget.nodeId"
    :node-name="forceTarget.nodeName"
    :target-key="forceTarget.key"
    :target-label="`${forceTarget.app}/${forceTarget.streamId}`"
    :fingerprint="forcePreview.fingerprint"
    :impacts="forceImpacts"
    :confirm-phrase="`强制结束 ${forceTarget.streamId}`"
    :require-reason="true"
    :context-bound="false"
    action-label="强制结束"
    :busy="forceBusy"
    @confirm="confirmForce"
    @stale="closeForce"
    @update:visible="
      value => {
        if (!value) closeForce();
      }
    "
  />
  <a-modal
    v-else
    :visible="!!forceTarget"
    modal-class="uvp-system-dialog"
    title="强制结束预检"
    :width="540"
    :footer="false"
    :mask-closable="false"
    @cancel="closeForce"
    ><div class="preflight-state">
      <a-spin v-if="forceLoading" /><strong>{{ forceLoading ? "正在核对 RTP 接收服务和业务持有…" : forceError }}</strong
      ><a-button v-if="!forceLoading" @click="closeForce" class="uvp-btn-neutral">关闭</a-button>
    </div></a-modal
  >

  <ZLMSessionKickDialog
    v-model:visible="kickVisible"
    :node-name="kickTarget?.nodeName || ''"
    :viewer="kickTarget"
    @done="kickDone"
  />
</template>

<style scoped>
.media-ledger-dialog {
  display: grid;
  gap: 12px;
  color: var(--uvp-text-primary);
}

/* grid 子项默认 min-width:auto，会让表格按内容撑开、把横向滚动挤到外层弹窗；
   放行收缩后 scroll.x 才在表格内部生效。 */
.media-ledger-dialog > * {
  min-width: 0;
}
.media-ledger-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.media-ledger-tabs button {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  padding: 7px 10px;
  font: inherit;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
  transition:
    color 0.18s ease,
    background-color 0.18s ease,
    border-color 0.18s ease,
    box-shadow 0.18s ease;
}
.media-ledger-tabs button:hover {
  color: var(--uvp-brand-strong);
  border-color: color-mix(in srgb, var(--uvp-brand) 35%, var(--uvp-panel-border));
}
.media-ledger-tabs button.active {
  color: var(--uvp-solid-text);
  background: var(--uvp-solid-bg);
  border-color: var(--uvp-solid-bg);
  box-shadow: 0 8px 18px -14px color-mix(in srgb, var(--uvp-solid-bg) 65%, transparent);
}
.media-ledger-tab-count {
  display: inline-grid;
  place-items: center;
  min-width: 20px;
  height: 20px;
  padding: 0 5px;
  font-size: 11px;
  line-height: 20px;
  background: color-mix(in srgb, currentColor 10%, transparent);
  border-radius: 10px;
}
.media-ledger-tabs button.active .media-ledger-tab-count {
  background: rgb(255 255 255 / 18%);
}
.media-ledger-search {
  margin-bottom: 0;
}
.media-ledger-keyword-search :deep(.uvp-search-panel__fields) {
  width: 100%;
}
.network-session-search :deep(.uvp-search-panel__fields) {
  flex: 1;
  width: auto;
}
.media-ledger-keyword {
  flex: 0 1 320px;
  width: 320px !important;
  max-width: 100%;
}
.network-session-search :deep(.network-node-filter) {
  flex: 0 0 220px;
  width: 220px !important;
}
.network-peer-filter {
  width: 200px;
}
.network-port-filter {
  width: 160px;
}
.media-ledger-warning {
  padding: 8px 10px;
  color: var(--uvp-warning);
  background: color-mix(in srgb, var(--uvp-warning) 10%, transparent);
  border-radius: 7px;
}
.media-ledger-table strong,
.media-ledger-table small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.media-ledger-table small {
  margin-top: 3px;
  color: var(--uvp-text-tertiary);
}

/* 会话类型兜底：未知类型会原样回显 ZLM 的 C++ 类型名（可能带 SSL 模板包装），
   a-tag 默认不省略 ⇒ 会横向压到「会话标识」列上。这里就地省略，全文走 title。 */
.media-ledger-session-type {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  vertical-align: middle;
  white-space: nowrap;
}
.media-ledger-actions {
  white-space: nowrap;
}

/* 观众表：主行是「一个人」，展开行才是逐条播放连接 */
.audience-streams {
  display: grid;
  gap: 2px;
}
.audience-stream {
  display: flex;
  flex-direction: column;
  line-height: 1.35;
}
.audience-stream small,
.audience-hint {
  color: var(--uvp-text-tertiary);
}
.audience-connections {
  display: grid;
  gap: 6px;
  padding: 4px 12px 4px 44px;
}
.audience-connection {
  display: flex;
  gap: 12px;
  align-items: center;
  padding: 6px 10px;
  background: var(--uvp-bg-subtle);
  border-radius: 6px;
}
.audience-schema {
  min-width: 46px;
  color: var(--uvp-text-secondary);
}
.audience-stream-id {
  min-width: 120px;
}
.audience-identifier {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
}
.protocols {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.numeric,
.mono {
  font-family: var(--zlm-font-mono);
  font-variant-numeric: tabular-nums;
}
.media-ledger-empty {
  padding: 34px;
  color: var(--uvp-text-tertiary);
  text-align: center;
}
.action-confirm {
  display: grid;
  gap: 16px;
}
.action-confirm > p {
  padding: 10px 12px;
  margin: 0;
  color: var(--uvp-warning);
  background: color-mix(in srgb, var(--uvp-warning) 10%, transparent);
  border-radius: 8px;
}
.action-confirm dl {
  display: grid;
  gap: 10px;
  margin: 0;
}
.action-confirm dl > div {
  display: grid;
  grid-template-columns: 90px 1fr;
  gap: 12px;
}
.action-confirm dt {
  color: var(--uvp-text-tertiary);
}
.action-confirm dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.action-confirm footer {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
}
.preflight-state {
  display: flex;
  flex-direction: column;
  gap: 12px;
  align-items: center;
  justify-content: center;
  min-height: 170px;
  color: var(--uvp-text-secondary);
  text-align: center;
}

@media (width <=720px) {
  .media-ledger-tabs button {
    flex: 1;
    justify-content: center;
  }
  .media-ledger-search :deep(.uvp-search-panel__surface) {
    padding: 10px;
  }
  .media-ledger-keyword,
  .network-session-search :deep(.network-node-filter),
  .network-peer-filter,
  .network-port-filter {
    flex: 1 1 100%;
    width: 100% !important;
  }
}
</style>
