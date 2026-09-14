<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat console-shell">
      <div class="toolbar">
        <div class="toolbar-left">
          <span class="page-title">实时日志控制台</span>
          <a-tag :color="statusColor" size="small">
            <span class="status-content"><CircleDot :size="11" :class="{ pulse: connected }" />{{ statusText }}</span>
          </a-tag>
          <span class="head-meta">当前实例 {{ currentInstance || "等待连接" }}</span>
        </div>
        <div class="toolbar-right">
          <span>缓冲 {{ events.length }} 条</span>
          <span v-if="dropped" class="drop-count">丢弃 {{ dropped }} 条</span>
        </div>
      </div>

      <s-layout-search class="console-search">
        <template #fields>
          <a-input v-model="filter.keyword" allow-clear placeholder="搜索消息、事件或任意结构化字段" style="width: 340px">
            <template #prefix><Search :size="14" /></template>
          </a-input>
          <a-select v-model="filter.level" allow-clear placeholder="全部级别" style="width: 150px">
            <a-option value="debug">DEBUG</a-option>
            <a-option value="info">INFO</a-option>
            <a-option value="warn">WARN</a-option>
            <a-option value="error">ERROR</a-option>
            <a-option value="dpanic">DPANIC</a-option>
            <a-option value="panic">PANIC</a-option>
            <a-option value="fatal">FATAL</a-option>
          </a-select>
          <a-input v-model="filter.module" allow-clear placeholder="筛选模块 / component" style="width: 240px" />
        </template>
        <template #actions>
          <a-button type="primary" @click="restart">
            <template #icon><RefreshCcw :size="14" /></template>
            {{ connected ? "重新连接" : "连接" }}
          </a-button>
          <a-button @click="resetFilters">
            <template #icon><RotateCcw :size="14" /></template>
            重置筛选
          </a-button>
        </template>
      </s-layout-search>

      <a-alert v-if="gapMessage" type="warning" closable @close="gapMessage = ''">
        {{ gapMessage }}
      </a-alert>

      <div class="terminal-view">
        <div class="terminal-head">
          <div class="terminal-dot-group" aria-hidden="true">
            <span class="term-dot red" />
            <span class="term-dot yellow" />
            <span class="term-dot green" />
          </div>
          <span class="terminal-title mono">uvp-console.log — {{ visibleEvents.length }} / {{ events.length }} lines</span>
          <div class="terminal-actions">
            <span class="live-indicator" :class="{ paused: !following }">
              <CircleDot :size="10" :class="{ pulse: connected && following }" />
              {{ following ? "FOLLOW" : "PAUSED" }}
            </span>
            <a-button size="mini" class="term-action" @click="toggleFollowing">
              <template #icon><component :is="following ? Pause : Play" :size="12" /></template>
              {{ following ? "暂停跟随" : "继续跟随" }}
            </a-button>
            <a-button size="mini" class="term-action" title="清空页面缓冲" @click="clearEvents">
              <template #icon><Eraser :size="12" /></template>
            </a-button>
            <a-button size="mini" class="term-action" title="下载当前筛选结果" @click="downloadLogs">
              <template #icon><Download :size="12" /></template>
            </a-button>
          </div>
        </div>

        <div ref="listRef" class="terminal-body mono" @scroll="onScroll">
          <div v-if="!visibleEvents.length" class="terminal-empty">
            $ tail -f uvp-console.log
            <br />
            <span class="dim"># {{ events.length ? "当前筛选条件下没有匹配日志" : "等待控制台日志流入..." }}</span>
          </div>
          <div
            v-for="item in visibleEvents"
            :key="`${item.instanceId}-${item.sequence}`"
            :class="['log-entry', `level-${levelClass(item.level)}`]"
          >
            <div class="log-line">
              <span class="log-time">{{ formatTime(item.occurredAt || item.observedAt) }}</span>
              <span class="log-level">{{ item.level.toUpperCase() }}</span>
              <span class="log-module">{{ item.module || "app" }}</span>
              <span class="log-message">{{ item.message || item.event }}</span>
              <span v-if="formatFields(item.fields)" class="log-fields">{{ formatFields(item.fields) }}</span>
              <span v-if="item.truncated" class="truncated">[TRUNCATED]</span>
            </div>
            <pre v-if="item.stack" class="log-stack">{{ item.stack }}</pre>
            <div class="line-actions">
              <button type="button" title="复制此行完整 JSON" @click="copyEvent(item)"><Copy :size="13" /></button>
              <button v-if="item.callId" type="button" title="查看对应 SIP Trace" @click="openTrace(item.callId)"><ExternalLink :size="13" /></button>
            </div>
          </div>
        </div>

        <div class="terminal-footer">
          <span class="mono">$ uvp-server --logs=console --follow</span>
          <span>滚动离开底部会暂停自动跟随 · 页面保留最近 2000 条</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import dayjs from "dayjs";
import { Message } from "@arco-design/web-vue";
import { CircleDot, Copy, Download, Eraser, ExternalLink, Pause, Play, RefreshCcw, RotateCcw, Search } from "lucide-vue-next";
import { openRealtimeLogStream, type RealtimeBusinessLogEvent } from "@/api/realtime-business-log";

const router = useRouter();
const filter = reactive({ keyword: "", level: "", module: "" });
const events = ref<RealtimeBusinessLogEvent[]>([]);
const dropped = ref(0);
const following = ref(true);
const connected = ref(false);
const status = ref<"idle" | "connecting" | "connected" | "reconnecting" | "limited" | "expired">("idle");
const gapMessage = ref("");
const listRef = ref<HTMLElement>();
let controller: AbortController | null = null;
let lastSequence = 0;
const currentInstance = ref("");

const statusText = computed(() => ({ idle: "未连接", connecting: "连接中", connected: "已连接", reconnecting: "重连中", limited: "连接受限", expired: "鉴权失效" }[status.value]));
const statusColor = computed(() => ({ idle: "gray", connecting: "orange", connected: "green", reconnecting: "orange", limited: "red", expired: "red" }[status.value]));

const visibleEvents = computed(() => {
  const keyword = filter.keyword.trim().toLowerCase();
  const module = filter.module.trim().toLowerCase();
  return events.value.filter(item => {
    if (filter.level && item.level !== filter.level) return false;
    if (module && !item.module.toLowerCase().includes(module)) return false;
    if (!keyword) return true;
    return eventSearchText(item).includes(keyword);
  });
});

function eventSearchText(item: RealtimeBusinessLogEvent): string {
  return [item.message, item.module, item.event, item.stack, JSON.stringify(item.fields || {})].join(" ").toLowerCase();
}
function append(event: RealtimeBusinessLogEvent) {
  events.value.push(event);
  if (events.value.length > 2000) events.value.splice(0, events.value.length - 2000);
  lastSequence = event.sequence;
  if (following.value) nextTick(jumpToBottom);
}
async function connect() {
  controller?.abort();
  const connection = new AbortController();
  controller = connection;
  connected.value = false;
  gapMessage.value = "";
  let delay = 1000;
  while (!connection.signal.aborted) {
    status.value = delay === 1000 ? "connecting" : "reconnecting";
    try {
      await openRealtimeLogStream({ since: lastSequence || undefined }, item => {
        if (item.type === "ready") {
          if (currentInstance.value && item.data.instanceId && currentInstance.value !== item.data.instanceId) gapMessage.value = "服务实例已变化，已从新实例的内存快照恢复";
          currentInstance.value = item.data.instanceId || currentInstance.value;
          connected.value = true;
          status.value = "connected";
          delay = 1000;
        } else if (item.type === "message") append(item.data);
        else if (item.type === "gap") gapMessage.value = `日志存在缺口：${item.data.reason}`;
        else if (item.type === "dropped") dropped.value = item.data.count;
        else if (item.type === "auth_expired") {
          status.value = "expired";
          gapMessage.value = "连接已到期，请重新连接以刷新认证";
          connection.abort();
        }
      }, connection.signal);
    } catch (error: any) {
      if (error?.name === "AbortError") break;
      if (error?.message?.includes("(401)") || error?.message?.includes("(403)")) {
        status.value = "expired";
        gapMessage.value = "登录或日志查看权限已失效，请重新登录";
        break;
      }
      if (error?.message?.includes("(429)")) status.value = "limited";
      gapMessage.value = error?.message || "实时日志连接失败";
    }
    if (controller === connection) connected.value = false;
    if (connection.signal.aborted) break;
    await new Promise(resolve => setTimeout(resolve, delay));
    delay = Math.min(delay * 2, 30000);
  }
}

function restart() { connect(); }
function resetFilters() { filter.keyword = ""; filter.level = ""; filter.module = ""; }
function clearEvents() { events.value = []; dropped.value = 0; }
function levelClass(level: string) { return level.toLowerCase(); }
function formatTime(value: string) { return value ? dayjs(value).format("YYYY-MM-DD HH:mm:ss.SSS") : "---- -- -- --:--:--.---"; }
function formatFields(fields?: Record<string, unknown>) { return fields && Object.keys(fields).length ? JSON.stringify(fields) : ""; }
function eventDetails(item: RealtimeBusinessLogEvent) { return JSON.stringify(item, null, 2); }
async function copyEvent(item: RealtimeBusinessLogEvent) {
  try {
    await navigator.clipboard.writeText(eventDetails(item));
    Message.success("日志已复制");
  } catch {
    Message.error("复制失败，请检查浏览器剪贴板权限");
  }
}
function openTrace(callId: string) { router.push({ path: "/gb28181/sip-traces", query: { callId } }); }
function jumpToBottom() { if (listRef.value) listRef.value.scrollTop = listRef.value.scrollHeight; }
function toggleFollowing() { following.value = !following.value; if (following.value) nextTick(jumpToBottom); }
function onScroll() {
  if (!listRef.value) return;
  const distance = listRef.value.scrollHeight - listRef.value.scrollTop - listRef.value.clientHeight;
  if (distance > 80) following.value = false;
}
function renderLine(item: RealtimeBusinessLogEvent): string {
  const fields = formatFields(item.fields);
  const line = `${formatTime(item.occurredAt || item.observedAt)} ${item.level.toUpperCase().padEnd(6)} ${item.module || "app"} ${item.message || item.event}${fields ? ` ${fields}` : ""}`;
  return item.stack ? `${line}\n${item.stack}` : line;
}
function downloadLogs() {
  const blob = new Blob([visibleEvents.value.map(renderLine).join("\n")], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `uvp-console-${dayjs().format("YYYYMMDD-HHmmss")}.log`;
  anchor.click();
  URL.revokeObjectURL(url);
}

onMounted(connect);
onBeforeUnmount(() => controller?.abort());
</script>

<style scoped>
.console-shell { display: flex; flex-direction: column; gap: 12px; height: 100%; min-height: 0; }
.toolbar { display: flex; align-items: center; justify-content: space-between; min-height: 36px; padding: 0 4px; }
.toolbar-left, .toolbar-right, .status-content { display: flex; align-items: center; gap: 10px; }
.page-title { color: var(--uvp-text-primary); font-size: 17px; font-weight: 650; }
.head-meta, .toolbar-right { color: var(--uvp-text-tertiary); font-size: 12px; }
.drop-count { color: var(--uvp-warning); }.console-search { margin-bottom: 0; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, "JetBrains Mono", Consolas, monospace; }
.pulse { color: #3fb950; animation: pulse 1.5s ease-in-out infinite; }
@keyframes pulse { 50% { opacity: .35; } }
.terminal-view { display: flex; flex: 1; flex-direction: column; min-height: 360px; color: #d4d4d8; background: #0d1117; border: 1px solid #21262d; border-radius: 10px; overflow: hidden; }
.terminal-head { display: flex; align-items: center; gap: 12px; min-height: 42px; padding: 0 14px; background: linear-gradient(180deg, #1a1f27 0%, #131820 100%); border-bottom: 1px solid #21262d; }
.terminal-dot-group { display: inline-flex; gap: 6px; }.term-dot { width: 11px; height: 11px; border-radius: 50%; opacity: .9; }
.term-dot.red { background: #ff5f57; }.term-dot.yellow { background: #febc2e; }.term-dot.green { background: #28c840; }
.terminal-title { min-width: 0; color: #9ca3af; font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.terminal-actions { display: flex; align-items: center; gap: 7px; margin-left: auto; }
.live-indicator { display: inline-flex; align-items: center; gap: 5px; color: #58d68d; font-size: 10px; font-weight: 700; letter-spacing: .06em; }
.live-indicator.paused { color: #d29922; }.term-action { color: #c9d1d9 !important; background: #21262d !important; border-color: #30363d !important; }.term-action:hover { color: #fff !important; background: #30363d !important; }
.terminal-body { flex: 1; min-height: 0; padding: 8px 0; overflow: auto; font-size: 12px; line-height: 1.65; }
.terminal-empty { padding: 18px 20px; color: #8b949e; }.dim { color: #484f58; }
.log-entry { position: relative; padding: 2px 76px 2px 12px; border-left: 2px solid transparent; }.log-entry:hover { background: rgb(255 255 255 / 4%); }
.log-entry.level-debug { color: #8b949e; }.log-entry.level-info { color: #c9d1d9; }.log-entry.level-warn { color: #e3b341; border-left-color: #d29922; }.log-entry.level-error, .log-entry.level-dpanic, .log-entry.level-panic, .log-entry.level-fatal { color: #ff7b72; border-left-color: #f85149; }
.log-line { display: flex; align-items: baseline; gap: 10px; min-width: max-content; }.log-time { color: #7d8590; white-space: nowrap; }.log-level { width: 48px; font-weight: 700; }.log-module { width: 150px; color: #79c0ff; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.log-message { color: inherit; white-space: pre-wrap; word-break: break-word; }.log-fields { color: #a5d6ff; white-space: pre-wrap; word-break: break-all; }.truncated { color: #d29922; }
.log-stack { margin: 2px 0 4px 250px; color: #ffa198; font: inherit; white-space: pre-wrap; word-break: break-all; }
.line-actions { position: absolute; top: 1px; right: 12px; display: none; gap: 4px; }.log-entry:hover .line-actions { display: flex; }
.line-actions button { display: inline-flex; align-items: center; justify-content: center; width: 25px; height: 25px; padding: 0; color: #8b949e; background: #21262d; border: 1px solid #30363d; border-radius: 5px; cursor: pointer; }.line-actions button:hover { color: #fff; background: #30363d; }
.terminal-footer { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 30px; padding: 0 12px; color: #6e7681; background: #0b0f14; border-top: 1px solid #21262d; font-size: 10px; }
@media (max-width: 900px) {
  .toolbar { align-items: flex-start; gap: 8px; }.toolbar-left { flex-wrap: wrap; }.head-meta { display: none; }
  .terminal-head { flex-wrap: wrap; height: auto; padding: 9px 12px; }.terminal-title { order: 2; width: calc(100% - 54px); }.terminal-actions { order: 3; width: 100%; margin-left: 0; }
  .log-module { width: 100px; }.log-stack { margin-left: 200px; }.terminal-footer span:last-child { display: none; }
}
</style>
