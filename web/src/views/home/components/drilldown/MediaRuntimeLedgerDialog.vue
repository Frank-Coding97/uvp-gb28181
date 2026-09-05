<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { CircleStop, LogOut, Network, Radio, RotateCcw, Search, ShieldAlert, Users, Video } from "lucide-vue-next";
import { stopPlay } from "@/api/gb28181";
import {
  forceCloseZLMRTPServer,
  preflightCloseZLMRTPServer,
  type ZLMRTPServerCloseRequest
} from "@/api/gb28181-zlm-ingress";
import {
  listZLMNetworkSessions,
  listZLMStreamViewers,
  type ZLMNetworkSession,
  type ZLMNetworkSessionQuery,
  type ZLMOwnershipPreflight,
  type ZLMStreamViewer
} from "@/api/gb28181-zlm-runtime";
import { useUserStoreHook } from "@/store/modules/user";
import ZLMDangerActionDialog from "@/views/gb28181/zlm/components/ZLMDangerActionDialog.vue";
import { formatZLMByteRate, formatZLMDuration, zlmErrorPresentation } from "@/views/gb28181/zlm/components/zlmFormatters";
import ZLMSessionKickDialog from "@/views/gb28181/zlm/ZLMSessionKickDialog.vue";
import type { MediaBusinessStream, MediaRuntimeLedger, MediaRuntimeLedgerKind } from "../../dashboardDrilldownState";

interface ViewerLedgerRow extends ZLMStreamViewer {
  key: string;
  businessStream: MediaBusinessStream;
}

const props = defineProps<{ visible: boolean; kind: MediaRuntimeLedgerKind; ledger: MediaRuntimeLedger }>();
const emit = defineEmits<{ close: []; kind: [value: MediaRuntimeLedgerKind]; refresh: [] }>();
const userStore = useUserStoreHook();
const query = ref("");
const stopTarget = ref<MediaBusinessStream | null>(null);
const stopping = ref(false);
const forceTarget = ref<MediaBusinessStream | null>(null);
const forcePreview = ref<ZLMOwnershipPreflight | null>(null);
const forceLoading = ref(false);
const forceError = ref("");
const forceBusy = ref(false);
const viewerRows = ref<ViewerLedgerRow[]>([]);
const viewerLoading = ref(false);
const viewerError = ref("");
const viewerTruncated = ref(false);
let viewerGeneration = 0;
const kickVisible = ref(false);
const kickTarget = ref<ViewerLedgerRow | null>(null);
const pageSize = 10;
const networkRows = ref<ZLMNetworkSession[]>([]);
const networkLoading = ref(false);
const networkError = ref("");
const networkTruncated = ref(false);
const networkNodeId = ref<number>();
const networkPeerIp = ref("");
const networkLocalPort = ref<number>();
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
const networkSessionTypeLabels: Record<string, string> = {
  "mediakit::HttpSession": "HTTP 会话",
  "mediakit::RtspSession": "RTSP 会话",
  "mediakit::RtmpSession": "RTMP 会话",
  "mediakit::RtpSession": "RTP 会话",
  "mediakit::SrtSession": "SRT 会话",
  "mediakit::WebSocketSession": "WebSocket 会话",
  "mediakit::WebRtcSession": "WebRTC 会话",
  "mediakit::TcpSession": "TCP 会话",
  "mediakit::UdpSession": "UDP 会话"
};

const title = computed(() => `流媒体运行态 · ${kinds.find(item => item.key === props.kind)?.label ?? "在线流"}`);
const sourceRows = computed(() => props.kind === "recordings" ? props.ledger.recordings : props.ledger.streams);
const streamRows = computed(() => {
  const keyword = query.value.trim().toLowerCase();
  if (!keyword) return sourceRows.value;
  return sourceRows.value.filter(item => [item.deviceName, item.deviceId, item.channelName, item.channelId, item.streamId, item.app, item.nodeName, ...item.protocols].join(" ").toLowerCase().includes(keyword));
});
const filteredViewerRows = computed(() => {
  const keyword = query.value.trim().toLowerCase();
  if (!keyword) return viewerRows.value;
  return viewerRows.value.filter(item => {
    const stream = item.businessStream;
    return [stream.deviceName, stream.deviceId, stream.channelName, stream.channelId, stream.streamId, stream.app, stream.nodeName, item.media.schema, item.peerIp, item.localIp, item.identifier, item.typeId].join(" ").toLowerCase().includes(keyword);
  });
});
const filteredCount = computed(() => streamRows.value.length);
const pagination = computed(() => ({
  pageSize,
  showJumper: filteredCount.value > pageSize,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50, 100]
}));
const networkPagination = computed(() => ({
  current: networkPage.value,
  pageSize: networkPageSize.value,
  total: networkTotal.value,
  showJumper: networkTotal.value > networkPageSize.value,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50, 100]
}));
const viewerPagination = computed(() => ({
  pageSize,
  showJumper: filteredViewerRows.value.length > pageSize,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50, 100]
}));
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canForce = computed(() => hasPermission("gb28181:zlm:rtp:force-close"));
const canKick = computed(() => hasPermission("gb28181:zlm:session:kick"));
const forceImpacts = computed(() => forcePreview.value?.snapshot.impacts?.map(impact => impact.reason || impact.owner || [impact.resourceType, impact.resourceKey].filter(Boolean).join(" / ")).filter(Boolean) as string[] ?? []);

watch(() => [props.kind, props.visible], () => {
  query.value = "";
  closeSecondaryDialogs();
  viewerGeneration += 1;
  networkGeneration += 1;
  viewerLoading.value = false;
  networkLoading.value = false;
  if (!props.visible) return;
  if (props.kind === "viewers") {
    void loadViewerSessions();
    return;
  }
  if (props.kind !== "sessions") return;
  const preferredNode = props.ledger.sessions.find(item => item.sampled) ?? props.ledger.sessions[0];
  networkNodeId.value = preferredNode?.nodeId;
  networkPeerIp.value = "";
  networkLocalPort.value = undefined;
  networkPage.value = 1;
  void loadNetworkSessions();
}, { immediate: true });

function closeSecondaryDialogs() {
  stopTarget.value = null;
  forceTarget.value = null;
  forcePreview.value = null;
  forceError.value = "";
  kickVisible.value = false;
  kickTarget.value = null;
}

function displayName(value: string, fallback: string) { return value.trim() || fallback; }
function recordingText(row: MediaBusinessStream) { return row.recordingMp4 ? "MP4" : "未录制"; }
function rtpRequest(row: MediaBusinessStream): ZLMRTPServerCloseRequest { return { nodeId: row.nodeId, vhost: row.vhost, app: row.app, stream: row.streamId }; }
function networkSessionTypeLabel(value: string) {
  const typeId = value.trim();
  if (!typeId) return "未知";
  const qualifiedTypeId = typeId.includes("::") ? typeId : `mediakit::${typeId}`;
  return networkSessionTypeLabels[qualifiedTypeId] ?? typeId;
}

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
    if (!response.data.snapshot.presenceKnown || !response.data.snapshot.present) throw new Error("后端未确认该业务流仍有 RTP 接收服务，未执行强制结束");
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
  if (!target || !preview || payload.nodeId !== target.nodeId || payload.targetKey !== target.key || payload.fingerprint !== preview.fingerprint) {
    Message.error("业务流快照已变化，未执行强制结束");
    closeForce();
    return;
  }
  forceBusy.value = true;
  try {
    const response = await forceCloseZLMRTPServer(target.nodeId, rtpRequest(target), payload.reason);
    if (response.code !== 0 || !response.data || (!response.data.released && !response.data.alreadyReleased)) throw new Error(response.message || "后端未确认业务流已结束");
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

async function loadViewerSessions() {
  const requests = props.ledger.viewers.flatMap(businessStream => businessStream.mediaTargets.map(media => ({ businessStream, media })));
  const generation = ++viewerGeneration;
  viewerRows.value = [];
  viewerError.value = "";
  viewerTruncated.value = false;
  if (!requests.length) return;
  viewerLoading.value = true;
  try {
    const results = await Promise.allSettled(requests.map(({ businessStream, media }) => listZLMStreamViewers(businessStream.nodeId, media, { page: 1, pageSize: 100 })));
    if (generation !== viewerGeneration) return;
    const unique = new Map<string, ViewerLedgerRow>();
    let failed = 0;
    results.forEach((result, index) => {
      const request = requests[index];
      if (result.status !== "fulfilled" || result.value.code !== 0 || !result.value.data) {
        failed += 1;
        return;
      }
      const page = result.value.data;
      viewerTruncated.value ||= page.truncated || page.total > page.list.length;
      for (const viewer of page.list) {
        const key = `${viewer.nodeId}\u001f${viewer.media.schema}\u001f${viewer.identifier}`;
        unique.set(key, { ...viewer, key, businessStream: request.businessStream });
      }
    });
    viewerRows.value = [...unique.values()];
    if (failed) viewerError.value = `${failed} 个协议的观看会话读取失败`;
  } finally {
    if (generation === viewerGeneration) viewerLoading.value = false;
  }
}

function openKick(viewer: ViewerLedgerRow) {
  if (!canKick.value || !viewer.kickable) return;
  kickTarget.value = viewer;
  kickVisible.value = true;
}

function kickDone() {
  kickVisible.value = false;
  if (props.visible && props.kind === "viewers") void loadViewerSessions();
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
  networkLocalPort.value = undefined;
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
  <a-modal :visible="visible" modal-class="uvp-system-dialog media-runtime-ledger-modal" :title="title" :width="1180" :footer="false" unmount-on-close @cancel="emit('close')">
    <div class="media-ledger-dialog">
      <div class="media-ledger-tabs" role="tablist" aria-label="运行态台账类型">
        <button v-for="item in kinds" :key="item.key" type="button" role="tab" :aria-selected="kind === item.key" :class="{ active: kind === item.key }" @click="emit('kind', item.key)">
          <component :is="item.icon" :size="15" />
          <span>{{ item.label }}</span>
          <span class="media-ledger-tab-count">{{ ledger.totals[item.key] }}</span>
        </button>
      </div>
      <s-layout-search v-if="kind === 'sessions'" class="media-ledger-search network-session-search">
        <template #fields>
          <a-select v-model="networkNodeId" class="network-node-filter" placeholder="选择媒体节点" aria-label="选择媒体节点" @change="changeNetworkNode">
            <a-option v-for="node in ledger.sessions" :key="node.nodeId" :value="node.nodeId">{{ node.nodeName }}{{ node.sampled ? '' : '（未采样）' }}</a-option>
          </a-select>
          <a-input-search v-model="networkPeerIp" class="network-peer-filter" allow-clear placeholder="远端 IP" aria-label="按远端 IP 筛选" @search="queryNetworkSessions" />
          <a-input-number v-model="networkLocalPort" class="network-port-filter" :min="1" :max="65535" hide-button allow-clear placeholder="本地端口" aria-label="按本地端口筛选" @press-enter="queryNetworkSessions" />
        </template>
        <template #actions><a-button type="primary" :loading="networkLoading" @click="queryNetworkSessions"><template #icon><Search :size="14" /></template>查询</a-button><a-button :disabled="networkLoading" @click="resetNetworkSessions"><template #icon><RotateCcw :size="14" /></template>重置</a-button></template>
      </s-layout-search>
      <s-layout-search v-else class="media-ledger-search media-ledger-keyword-search">
        <template #fields><a-input-search v-model="query" class="media-ledger-keyword" allow-clear :placeholder="kind === 'viewers' ? '搜索设备、通道、流 ID、会话标识或地址' : '搜索设备、通道、流 ID 或节点'" aria-label="搜索运行态台账" /></template>
      </s-layout-search>
      <div v-if="ledger.partial" class="media-ledger-warning" role="status">当前快照覆盖不完整<span v-if="ledger.warnings.length">：{{ ledger.warnings.join("；") }}</span></div>
      <div v-if="kind === 'viewers' && viewerError" class="media-ledger-warning" role="alert">{{ viewerError }}</div>
      <div v-if="kind === 'viewers' && viewerTruncated" class="media-ledger-warning" role="status">部分协议的观看会话超过单次采集上限，当前台账仅展示已采集数据。</div>
      <div v-if="kind === 'sessions' && networkError" class="media-ledger-warning" role="alert">{{ networkError }}</div>
      <div v-if="kind === 'sessions' && networkTruncated" class="media-ledger-warning" role="status">节点返回的会话列表已达到采集上限，当前分页仅覆盖已采集数据。</div>

      <a-table v-if="kind === 'streams' || kind === 'recordings'" :data="streamRows" :pagination="pagination" row-key="key" class="uvp-data-table media-ledger-table" :scroll="{ x: kind === 'recordings' ? 1120 : 1020, y: 480 }">
        <template #columns>
          <a-table-column title="设备 / 通道" :width="240"><template #cell="{ record }"><strong>{{ displayName(record.deviceName, '未绑定设备') }}</strong><small>{{ displayName(record.channelName, '未绑定通道') }}</small><small class="mono">{{ record.deviceId || '—' }} / {{ record.channelId || '—' }}</small></template></a-table-column>
          <a-table-column title="流 ID" :width="150"><template #cell="{ record }"><strong class="mono">{{ record.streamId }}</strong><small>{{ record.app }} · {{ record.nodeName }}</small></template></a-table-column>
          <a-table-column title="协议" :width="150"><template #cell="{ record }"><div class="protocols"><a-tag v-for="protocol in record.protocols" :key="protocol" size="small">{{ protocol.toUpperCase() }}</a-tag></div></template></a-table-column>
          <a-table-column title="观看者" :width="75"><template #cell="{ record }"><span class="numeric">{{ record.viewers }}</span></template></a-table-column>
          <a-table-column title="实时码率" :width="105"><template #cell="{ record }"><span class="numeric">{{ formatZLMByteRate(record.bytesSpeed) }}</span></template></a-table-column>
          <a-table-column title="在线时长" :width="100"><template #cell="{ record }">{{ formatZLMDuration(record.aliveSecond) }}</template></a-table-column>
          <a-table-column v-if="kind === 'recordings'" title="录制" :width="100"><template #cell="{ record }">{{ recordingText(record) }}</template></a-table-column>
          <a-table-column title="操作" :width="220" align="center" fixed="right"><template #cell="{ record }"><div class="uvp-table-actions media-ledger-actions">
            <a-link class="uvp-table-action" status="warning" :disabled="!record.channelId" @click="stopTarget = record"><template #icon><CircleStop :size="13" /></template><span>停止点播</span></a-link>
            <a-link v-if="canForce" class="uvp-table-action uvp-table-action--delete" status="danger" @click="openForce(record)"><template #icon><ShieldAlert :size="13" /></template><span>强制结束</span></a-link>
          </div></template></a-table-column>
        </template>
        <template #empty><div class="media-ledger-empty">当前条件下暂无业务流</div></template>
      </a-table>

      <a-table v-else-if="kind === 'viewers'" :data="filteredViewerRows" :loading="viewerLoading" :pagination="viewerPagination" row-key="key" class="uvp-data-table media-ledger-table" :scroll="{ x: 1080, y: 480 }">
        <template #columns>
          <a-table-column title="设备 / 通道" :width="180"><template #cell="{ record }"><strong>{{ displayName(record.businessStream.deviceName, '未绑定设备') }}</strong><small>{{ displayName(record.businessStream.channelName, '未绑定通道') }}</small><small class="mono">{{ record.businessStream.deviceId || '—' }} / {{ record.businessStream.channelId || '—' }}</small></template></a-table-column>
          <a-table-column title="流 / 协议" :width="150"><template #cell="{ record }"><strong class="mono">{{ record.businessStream.streamId }}</strong><small>{{ record.businessStream.app }} · {{ record.businessStream.nodeName }}</small><a-tag size="small">{{ record.media.schema.toUpperCase() }}</a-tag></template></a-table-column>
          <a-table-column title="远端地址" :width="195" :ellipsis="true" :tooltip="true"><template #cell="{ record }"><span class="mono">{{ record.peerIp || '—' }}:{{ record.peerPort || '—' }}</span></template></a-table-column>
          <a-table-column title="本地地址" :width="195" :ellipsis="true" :tooltip="true"><template #cell="{ record }"><span class="mono">{{ record.localIp || '—' }}:{{ record.localPort || '—' }}</span></template></a-table-column>
          <a-table-column title="会话类型" :width="120"><template #cell="{ record }"><a-tag size="small" :title="record.typeId || undefined">{{ networkSessionTypeLabel(record.typeId) }}</a-tag></template></a-table-column>
          <a-table-column title="会话标识" :width="140" :ellipsis="true" :tooltip="true"><template #cell="{ record }"><span class="mono">{{ record.identifier || '—' }}</span></template></a-table-column>
          <a-table-column title="操作" :width="100" align="center" fixed="right"><template #cell="{ record }"><div v-if="canKick" class="uvp-table-actions media-ledger-actions"><a-link class="uvp-table-action uvp-table-action--delete" status="danger" :disabled="!record.kickable" @click="openKick(record)"><template #icon><LogOut :size="13" /></template><span>强退</span></a-link></div><span v-else>—</span></template></a-table-column>
        </template>
        <template #empty><div class="media-ledger-empty">当前没有可识别的观看会话</div></template>
      </a-table>

      <a-table v-else :data="networkRows" :loading="networkLoading" :pagination="networkPagination" row-key="id" class="uvp-data-table media-ledger-table" :scroll="{ x: 1140, y: 480 }" @page-change="changeNetworkPage" @page-size-change="changeNetworkPageSize">
        <template #columns>
          <a-table-column title="会话 ID" :width="240" :ellipsis="true" :tooltip="true"><template #cell="{ record }"><span class="mono">{{ record.id || '—' }}</span></template></a-table-column>
          <a-table-column title="远端地址" :width="190"><template #cell="{ record }"><span class="mono">{{ record.peerIp || '—' }}:{{ record.peerPort || '—' }}</span></template></a-table-column>
          <a-table-column title="本地地址" :width="190"><template #cell="{ record }"><span class="mono">{{ record.localIp || '—' }}:{{ record.localPort || '—' }}</span></template></a-table-column>
          <a-table-column title="连接类型" :width="170"><template #cell="{ record }">{{ record.type || '—' }}</template></a-table-column>
          <a-table-column title="会话类型" :width="170"><template #cell="{ record }"><a-tag size="small" :title="record.typeId || undefined">{{ networkSessionTypeLabel(record.typeId) }}</a-tag></template></a-table-column>
          <a-table-column title="会话标识" :width="180" :ellipsis="true" :tooltip="true"><template #cell="{ record }"><span class="mono">{{ record.identifier || '—' }}</span></template></a-table-column>
        </template>
        <template #empty><div class="media-ledger-empty">当前节点暂无符合条件的网络会话</div></template>
      </a-table>
    </div>
  </a-modal>

  <a-modal :visible="!!stopTarget" modal-class="uvp-system-dialog" title="停止点播" :width="520" :footer="false" :mask-closable="false" @cancel="stopTarget = null">
    <div v-if="stopTarget" class="action-confirm"><p>将向设备发送 BYE 并释放该通道当前媒体会话。</p><dl><div><dt>设备 / 通道</dt><dd>{{ displayName(stopTarget.deviceName, stopTarget.deviceId) }} / {{ displayName(stopTarget.channelName, stopTarget.channelId) }}</dd></div><div><dt>流 ID</dt><dd class="mono">{{ stopTarget.streamId }}</dd></div></dl><footer><a-button :disabled="stopping" @click="stopTarget = null">取消</a-button><a-button type="primary" status="danger" :loading="stopping" @click="confirmStop">确认停止</a-button></footer></div>
  </a-modal>

  <ZLMDangerActionDialog v-if="forceTarget && forcePreview" :visible="true" :node-id="forceTarget.nodeId" :node-name="forceTarget.nodeName" :target-key="forceTarget.key" :target-label="`${forceTarget.app}/${forceTarget.streamId}`" :fingerprint="forcePreview.fingerprint" :impacts="forceImpacts" :confirm-phrase="`强制结束 ${forceTarget.streamId}`" :require-reason="true" :context-bound="false" action-label="强制结束" :busy="forceBusy" @confirm="confirmForce" @stale="closeForce" @update:visible="value => { if (!value) closeForce(); }" />
  <a-modal v-else :visible="!!forceTarget" modal-class="uvp-system-dialog" title="强制结束预检" :width="540" :footer="false" :mask-closable="false" @cancel="closeForce"><div class="preflight-state"><a-spin v-if="forceLoading" /><strong>{{ forceLoading ? '正在核对 RTP 接收服务和业务持有…' : forceError }}</strong><a-button v-if="!forceLoading" @click="closeForce">关闭</a-button></div></a-modal>

  <ZLMSessionKickDialog v-model:visible="kickVisible" :node-name="kickTarget?.businessStream.nodeName || ''" :viewer="kickTarget" @done="kickDone" />
</template>

<style scoped>
.media-ledger-dialog{display:grid;gap:12px;color:var(--uvp-text-primary)}.media-ledger-tabs{display:flex;flex-wrap:wrap;gap:8px}.media-ledger-tabs button{display:inline-flex;align-items:center;gap:6px;padding:7px 10px;color:var(--uvp-text-secondary);font:inherit;cursor:pointer;background:var(--uvp-panel-bg);border:1px solid var(--uvp-panel-border);border-radius:8px;transition:color .18s ease,background-color .18s ease,border-color .18s ease,box-shadow .18s ease}.media-ledger-tabs button:hover{color:var(--uvp-brand-strong);border-color:color-mix(in srgb,var(--uvp-brand) 35%,var(--uvp-panel-border))}.media-ledger-tabs button.active{color:#fff;background:var(--uvp-brand);border-color:var(--uvp-brand);box-shadow:0 8px 18px -14px color-mix(in srgb,var(--uvp-brand) 65%,transparent)}.media-ledger-tab-count{display:inline-grid;min-width:20px;height:20px;padding:0 5px;font-size:11px;line-height:20px;background:color-mix(in srgb,currentColor 10%,transparent);border-radius:10px;place-items:center}.media-ledger-tabs button.active .media-ledger-tab-count{background:rgb(255 255 255 / 18%)}.media-ledger-search{margin-bottom:0}.media-ledger-keyword-search :deep(.uvp-search-panel__fields){width:100%}.network-session-search :deep(.uvp-search-panel__fields){flex:1;width:auto}.media-ledger-keyword{width:100%}.network-session-search :deep(.network-node-filter){width:220px!important;flex:0 0 220px}.network-peer-filter{width:200px}.network-port-filter{width:160px}.media-ledger-warning{padding:8px 10px;color:var(--uvp-warning);background:color-mix(in srgb,var(--uvp-warning) 10%,transparent);border-radius:7px}.media-ledger-table strong,.media-ledger-table small{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.media-ledger-table small{margin-top:3px;color:var(--uvp-text-tertiary)}.media-ledger-actions{white-space:nowrap}.protocols{display:flex;flex-wrap:wrap;gap:4px}.numeric,.mono{font-family:var(--zlm-font-mono);font-variant-numeric:tabular-nums}.media-ledger-empty{padding:34px;color:var(--uvp-text-tertiary);text-align:center}.action-confirm{display:grid;gap:16px}.action-confirm>p{margin:0;padding:10px 12px;color:var(--uvp-warning);background:color-mix(in srgb,var(--uvp-warning) 10%,transparent);border-radius:8px}.action-confirm dl{display:grid;gap:10px;margin:0}.action-confirm dl>div{display:grid;grid-template-columns:90px 1fr;gap:12px}.action-confirm dt{color:var(--uvp-text-tertiary)}.action-confirm dd{margin:0;overflow-wrap:anywhere}.action-confirm footer{display:flex;justify-content:flex-end;gap:8px}.preflight-state{display:flex;min-height:170px;flex-direction:column;align-items:center;justify-content:center;gap:12px;color:var(--uvp-text-secondary);text-align:center}
@media(max-width:720px){.media-ledger-tabs button{flex:1;justify-content:center}.media-ledger-search :deep(.uvp-search-panel__surface){padding:10px}.network-session-search :deep(.network-node-filter),.network-peer-filter,.network-port-filter{width:100%!important;flex:1 1 100%}}
</style>
