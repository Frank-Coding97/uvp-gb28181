<template>
  <div class="realtime-log-page">
    <a-space direction="vertical" fill>
      <a-space wrap>
        <a-tag :color="statusColor">{{ statusText }}</a-tag>
        <a-input v-model="filter.deviceId" placeholder="设备 ID" allow-clear style="width: 180px" />
        <a-input v-model="filter.channelId" placeholder="通道 ID" allow-clear style="width: 180px" />
        <a-input v-model="filter.module" placeholder="模块" allow-clear style="width: 160px" />
        <a-input v-model="filter.nodeId" placeholder="节点 ID" allow-clear style="width: 150px" />
        <a-input v-model="filter.streamId" placeholder="流 ID" allow-clear style="width: 180px" />
        <a-input v-model="filter.requestId" placeholder="Request ID" allow-clear style="width: 180px" />
        <a-input v-model="filter.operationId" placeholder="Operation ID" allow-clear style="width: 180px" />
        <a-input v-model="filter.correlationId" placeholder="关联 ID" allow-clear style="width: 180px" />
        <a-input v-model="filter.callId" placeholder="Call-ID" allow-clear style="width: 180px" />
        <a-input v-model="filter.event" placeholder="事件名" allow-clear style="width: 220px" />
        <a-select v-model="filter.level" placeholder="级别" allow-clear style="width: 120px">
          <a-option value="info">INFO</a-option><a-option value="warn">WARN</a-option><a-option value="error">ERROR</a-option>
        </a-select>
        <a-button type="primary" @click="restart">{{ connected ? "重连" : "连接" }}</a-button>
        <a-button @click="following = !following">{{ following ? "暂停跟随" : "继续跟随" }}</a-button>
        <a-button @click="events = []">清空</a-button>
        <a-badge :count="dropped" dot>已接收 {{ events.length }}</a-badge>
      </a-space>
      <a-alert v-if="gapMessage" type="warning">{{ gapMessage }}</a-alert>
      <div ref="listRef" class="event-list">
        <div v-for="item in events" :key="`${item.instanceId}-${item.sequence}`" class="event-row" :class="`level-${item.level}`">
          <span class="time">{{ item.occurredAt || item.observedAt }}</span>
          <span class="level">{{ item.level.toUpperCase() }}</span>
          <span class="module">{{ item.module }}</span>
          <span class="stage">{{ item.stage || "-" }} / {{ item.outcome || "-" }}</span>
          <span class="device">{{ item.deviceId || "-" }}</span>
          <span class="message">{{ item.message || item.event }}</span>
          <span class="sequence">#{{ item.sequence }}</span>
          <span class="actions">
            <a-button type="text" size="mini" title="复制事件" @click="copyEvent(item)"><Copy :size="14" /></a-button>
            <a-button v-if="item.callId" type="text" size="mini" title="查看 SIP Trace" @click="openTrace(item.callId)"><ExternalLink :size="14" /></a-button>
          </span>
          <pre class="details">{{ eventDetails(item) }}</pre>
        </div>
        <a-empty v-if="!events.length" description="等待实时业务事件" />
      </div>
    </a-space>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { Copy, ExternalLink } from "lucide-vue-next";
import { openRealtimeLogStream, type RealtimeBusinessLogEvent, type RealtimeLogFilter } from "@/api/realtime-business-log";

const router = useRouter();
const filter = reactive<RealtimeLogFilter>({});
const events = ref<RealtimeBusinessLogEvent[]>([]);
const dropped = ref(0);
const following = ref(true);
const connected = ref(false);
const status = ref<"idle" | "connecting" | "connected" | "reconnecting" | "limited" | "expired">("idle");
const gapMessage = ref("");
const listRef = ref<HTMLElement>();
let controller: AbortController | null = null;
let lastSequence = 0;
let currentInstance = "";
const statusText = computed(() => ({ idle: "未连接", connecting: "连接中", connected: "已连接", reconnecting: "重连中", limited: "已限流", expired: "鉴权失效" }[status.value]));
const statusColor = computed(() => ({ idle: "gray", connecting: "orange", connected: "green", reconnecting: "orange", limited: "red", expired: "red" }[status.value]));

function append(event: RealtimeBusinessLogEvent) {
  events.value.push(event);
  if (events.value.length > 2000) events.value.splice(0, events.value.length - 2000);
  lastSequence = event.sequence;
  if (following.value) nextTick(() => { if (listRef.value) listRef.value.scrollTop = listRef.value.scrollHeight; });
}
async function connect() {
  controller?.abort(); controller = new AbortController(); connected.value = false; gapMessage.value = "";
  let delay = 1000;
  while (!controller.signal.aborted) {
    status.value = delay === 1000 ? "connecting" : "reconnecting";
    try {
      await openRealtimeLogStream({ ...filter, since: lastSequence || undefined }, item => {
        if (item.type === "ready") {
          if (currentInstance && item.data.instanceId && currentInstance !== item.data.instanceId) gapMessage.value = "服务实例已变化，已从新实例快照恢复";
          currentInstance = item.data.instanceId || currentInstance;
          connected.value = true; status.value = "connected"; delay = 1000;
        }
        else if (item.type === "message") append(item.data);
        else if (item.type === "gap") gapMessage.value = `存在事件缺口：${item.data.reason}`;
        else if (item.type === "dropped") dropped.value = item.data.count;
        else if (item.type === "auth_expired") { status.value = "expired"; gapMessage.value = "连接已到期，请重新连接以刷新认证"; controller?.abort(); }
      }, controller.signal);
    } catch (error: any) {
      if (error?.name === "AbortError") break;
      if (error?.message?.includes("(401)") || error?.message?.includes("(403)")) { status.value = "expired"; gapMessage.value = "登录或权限已失效，请重新登录"; break; }
      if (error?.message?.includes("(429)")) status.value = "limited";
      gapMessage.value = error?.message || "实时日志连接失败";
    }
    connected.value = false;
    if (controller.signal.aborted) break;
    await new Promise(resolve => setTimeout(resolve, delay));
    delay = Math.min(delay * 2, 30000);
  }
}
function restart() { connect(); }
function eventDetails(item: RealtimeBusinessLogEvent) { return JSON.stringify(item, null, 2); }
async function copyEvent(item: RealtimeBusinessLogEvent) { await navigator.clipboard.writeText(eventDetails(item)); }
function openTrace(callId: string) { router.push({ path: "/gb28181/sip-traces", query: { callId } }); }
onMounted(connect); onBeforeUnmount(() => controller?.abort());
</script>

<style scoped>
.realtime-log-page { padding: 16px; height: 100%; box-sizing: border-box; }
.event-list { height: calc(100vh - 170px); overflow: auto; background: var(--color-bg-2); border: 1px solid var(--color-border-2); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.event-row { display: grid; grid-template-columns: 185px 64px 130px 130px 150px minmax(180px, 1fr) 70px 64px; gap: 8px; padding: 7px 10px; border-bottom: 1px solid var(--color-border-2); font-size: 12px; }
.level-warn { background: rgba(255, 196, 0, .08); }.level-error { background: rgba(245, 63, 63, .1); }.time,.sequence { color: var(--color-text-3); }.message { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.actions { display: flex; }.details { display: none; grid-column: 1 / -1; margin: 4px 0 0; padding: 8px; overflow: auto; background: var(--color-fill-2); white-space: pre-wrap; word-break: break-all; }.event-row:focus-within .details { display: block; }
@media (max-width: 900px) { .event-row { grid-template-columns: 150px 58px minmax(120px, 1fr) 64px; }.module,.stage,.device,.sequence { display: none; } }
</style>
