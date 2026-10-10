<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat console-shell">
      <div class="toolbar">
        <div class="toolbar-left">
          <span class="page-title">运行日志</span>
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
              <span v-if="fieldsOf(item).length" class="log-fields"
                ><span v-for="field in fieldsOf(item)" :key="field.key" :class="['field', { muted: field.muted }]"
                  ><span class="field-key">{{ field.key }}</span
                  ><span class="field-eq">=</span><span class="field-value">{{ field.display }}</span></span
                ></span
              >
              <span v-if="item.truncated" class="truncated">[TRUNCATED]</span>
            </div>
            <pre v-if="item.stack" class="log-stack">{{ item.stack }}</pre>
            <div class="line-actions">
              <button type="button" title="复制此行纯文本" @click="copyLine(item)"><Copy :size="13" /></button>
              <button type="button" title="复制此行完整 JSON" @click="copyEvent(item)"><Braces :size="13" /></button>
              <button v-if="item.callId" type="button" title="查看对应 SIP Trace" @click="openTrace(item.callId)">
                <ExternalLink :size="13" />
              </button>
            </div>
          </div>
        </div>

        <div class="terminal-footer">
          <span class="mono">$ uvp-server --logs=console --follow</span>
          <span>离开底部暂停跟随 · 滚到底部自动恢复 · 页面保留最近 2000 条</span>
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
import {
  Braces,
  CircleDot,
  Copy,
  Download,
  Eraser,
  ExternalLink,
  Pause,
  Play,
  RefreshCcw,
  RotateCcw,
  Search
} from "lucide-vue-next";
import { openRealtimeLogStream, type RealtimeBusinessLogEvent } from "@/api/realtime-business-log";
import { displayFields, renderLogLine, type DisplayField } from "./log-line";
import { resolveFollowAction } from "./follow-scroll";

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

const statusText = computed(
  () =>
    ({
      idle: "未连接",
      connecting: "连接中",
      connected: "已连接",
      reconnecting: "重连中",
      limited: "连接受限",
      expired: "鉴权失效"
    })[status.value]
);
const statusColor = computed(
  () =>
    ({ idle: "gray", connecting: "orange", connected: "green", reconnecting: "orange", limited: "red", expired: "red" })[
      status.value
    ]
);

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
      await openRealtimeLogStream(
        { since: lastSequence || undefined },
        item => {
          if (item.type === "ready") {
            if (currentInstance.value && item.data.instanceId && currentInstance.value !== item.data.instanceId)
              gapMessage.value = "服务实例已变化，已从新实例的内存快照恢复";
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
        },
        connection.signal
      );
    } catch (error: any) {
      if (error?.name === "AbortError") break;
      if (error?.message?.includes("(401)") || error?.message?.includes("(403)")) {
        status.value = "expired";
        gapMessage.value = "登录或日志查看权限已失效，请重新登录";
        break;
      }
      if (error?.message?.includes("(429)")) status.value = "limited";
      gapMessage.value = error?.message || "运行日志连接失败";
    }
    if (controller === connection) connected.value = false;
    if (connection.signal.aborted) break;
    await new Promise(resolve => setTimeout(resolve, delay));
    delay = Math.min(delay * 2, 30000);
  }
}

function restart() {
  connect();
}
function resetFilters() {
  filter.keyword = "";
  filter.level = "";
  filter.module = "";
}
function clearEvents() {
  events.value = [];
  dropped.value = 0;
}
function levelClass(level: string) {
  return level.toLowerCase();
}
function formatTime(value: string) {
  return value ? dayjs(value).format("YYYY-MM-DD HH:mm:ss.SSS") : "---- -- -- --:--:--.---";
}

// 字段排序与转义规则在 ./log-line.ts（纯函数，带单测），组件只负责画。
// 每条记录只算一次：SSE 连续推流时避免每帧对全部缓冲记录重复排序。
const fieldCache = new WeakMap<object, DisplayField[]>();
function fieldsOf(item: RealtimeBusinessLogEvent): DisplayField[] {
  let cached = fieldCache.get(item);
  if (!cached) {
    cached = displayFields(item.fields);
    fieldCache.set(item, cached);
  }
  return cached;
}
function eventDetails(item: RealtimeBusinessLogEvent) {
  return JSON.stringify(item, null, 2);
}
async function copyText(text: string, label: string) {
  try {
    await navigator.clipboard.writeText(text);
    Message.success(label);
  } catch {
    Message.error("复制失败，请检查浏览器剪贴板权限");
  }
}
function copyLine(item: RealtimeBusinessLogEvent) {
  return copyText(renderLine(item), "日志行已复制");
}
function copyEvent(item: RealtimeBusinessLogEvent) {
  return copyText(eventDetails(item), "日志 JSON 已复制");
}
function openTrace(callId: string) {
  router.push({ path: "/gb28181/sip-traces", query: { callId } });
}
function jumpToBottom() {
  if (listRef.value) listRef.value.scrollTop = listRef.value.scrollHeight;
}
function toggleFollowing() {
  following.value = !following.value;
  if (following.value) nextTick(jumpToBottom);
}
// 判定规则（含滞回阈值）在 ./follow-scroll.ts（纯函数，带单测），组件只负责执行。
// 滚到底部自动恢复跟随：新日志照旧贴底追加，不必再点一次「继续跟随」。
function onScroll() {
  if (!listRef.value) return;
  const { scrollHeight, scrollTop, clientHeight } = listRef.value;
  const action = resolveFollowAction(scrollHeight - scrollTop - clientHeight, following.value);
  if (action === "pause") {
    following.value = false;
  } else if (action === "resume") {
    following.value = true;
    nextTick(jumpToBottom);
  }
}
// 与后端 console 编码器同版式，保证「页面看到的」和「文件里 grep 到的」是同一条文本。
function renderLine(item: RealtimeBusinessLogEvent): string {
  return renderLogLine({
    time: formatTime(item.occurredAt || item.observedAt),
    level: item.level,
    module: item.module,
    message: item.message,
    event: item.event,
    stack: item.stack,
    fields: item.fields
  });
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
.console-shell {
  display: flex;
  flex-direction: column;
  gap: 12px;
  height: 100%;
  min-height: 0;
}
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 36px;
  padding: 0 4px;
}
.toolbar-left,
.toolbar-right,
.status-content {
  display: flex;
  gap: 10px;
  align-items: center;
}
.page-title {
  font-size: 17px;
  font-weight: 650;
  color: var(--uvp-text-primary);
}
.head-meta,
.toolbar-right {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.drop-count {
  color: var(--uvp-warning);
}
.console-search {
  margin-bottom: 0;
}

/* 级别下拉必须和同区输入框同色。
   ⛔ 根因：styles/arco-overrides.scss 里的 `.arco-select-view` 整组声明都带 !important
   （浅蓝底 --uvp-dialog-control-bg + 实线边框 --uvp-panel-border + hover 变蓝边），
   而 s-layout-search 组件给搜索控件的同款声明没有 !important ⇒ 被全局抢走，
   于是同一条搜索栏里「输入框是白底内描边、下拉框是浅蓝底实线边框」。
   这里按组件那套 token 提权覆盖（含 hover/focus 两态），把下拉框拉回与输入框一致。 */
.console-search :deep(.arco-select-view) {
  box-sizing: border-box;
  background: var(--uvp-search-control-bg) !important;
  border: 1px solid transparent !important;
  border-radius: 10px !important;
  box-shadow: var(--uvp-search-control-shadow) !important;
}

.console-search :deep(.arco-select-view:hover),
.console-search :deep(.arco-select-view.arco-select-view-focus) {
  border-color: transparent !important;
  box-shadow: var(--uvp-search-control-focus-shadow) !important;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, "JetBrains Mono", Consolas, monospace;
}
.pulse {
  color: #3fb950;
  animation: pulse 1.5s ease-in-out infinite;
}

@keyframes pulse {
  50% {
    opacity: 0.35;
  }
}
.terminal-view {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 360px;
  overflow: hidden;
  color: #d4d4d8;
  background: #0d1117;
  border: 1px solid #21262d;
  border-radius: 10px;
}
.terminal-head {
  display: flex;
  gap: 12px;
  align-items: center;
  min-height: 42px;
  padding: 0 14px;
  background: linear-gradient(180deg, #1a1f27 0%, #131820 100%);
  border-bottom: 1px solid #21262d;
}
.terminal-dot-group {
  display: inline-flex;
  gap: 6px;
}
.term-dot {
  width: 11px;
  height: 11px;
  border-radius: 50%;
  opacity: 0.9;
}
.term-dot.red {
  background: #ff5f57;
}
.term-dot.yellow {
  background: #febc2e;
}
.term-dot.green {
  background: #28c840;
}
.terminal-title {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  color: #9ca3af;
  white-space: nowrap;
}
.terminal-actions {
  display: flex;
  gap: 7px;
  align-items: center;
  margin-left: auto;
}
.live-indicator {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  font-size: 10px;
  font-weight: 700;
  color: #58d68d;
  letter-spacing: 0.06em;
}
.live-indicator.paused {
  color: #d29922;
}
.term-action {
  color: #c9d1d9 !important;
  background: #21262d !important;
  border-color: #30363d !important;
}
.term-action:hover {
  color: #ffffff !important;
  background: #30363d !important;
}
.terminal-body {
  flex: 1;
  min-height: 0;
  padding: 8px 0;
  overflow: auto;
  font-size: 12px;
  line-height: 1.65;
}
.terminal-empty {
  padding: 18px 20px;
  color: #8b949e;
}
.dim {
  color: #484f58;
}
.log-entry {
  position: relative;
  padding: 2px 76px 2px 12px;
  border-left: 2px solid transparent;
}
.log-entry:hover {
  background: rgb(255 255 255 / 4%);
}
.log-entry.level-debug {
  color: #8b949e;
}
.log-entry.level-info {
  color: #c9d1d9;
}
.log-entry.level-warn {
  color: #e3b341;
  border-left-color: #d29922;
}
.log-entry.level-error,
.log-entry.level-dpanic,
.log-entry.level-panic,
.log-entry.level-fatal {
  color: #ff7b72;
  border-left-color: #f85149;
}
.log-line {
  display: flex;
  gap: 10px;
  align-items: baseline;
  min-width: max-content;
}
.log-time {
  color: #7d8590;
  white-space: nowrap;
}
.log-level {
  width: 48px;
  font-weight: 700;
}
.log-module {
  width: 150px;
  overflow: hidden;
  text-overflow: ellipsis;
  color: #79c0ff;
  white-space: nowrap;
}
.log-message {
  color: inherit;
  overflow-wrap: break-word;
  white-space: pre-wrap;
}
.log-fields {
  color: #a5d6ff;
  word-break: break-all;
  white-space: pre-wrap;
}
.field + .field {
  margin-left: 11px;
}
.field-key {
  color: #6f8296;
}
.field-eq {
  color: #4d5b69;
}
.field-value {
  color: #a5d6ff;
}
.field.muted .field-key {
  color: #4d5b69;
}
.field.muted .field-value {
  color: #6e7681;
}
.truncated {
  color: #d29922;
}
.log-stack {
  margin: 2px 0 4px 250px;
  font: inherit;
  color: #ffa198;
  word-break: break-all;
  white-space: pre-wrap;
}
.line-actions {
  position: absolute;
  top: 1px;
  right: 12px;
  display: none;
  gap: 4px;
}
.log-entry:hover .line-actions {
  display: flex;
}
.line-actions button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 25px;
  height: 25px;
  padding: 0;
  color: #8b949e;
  cursor: pointer;
  background: #21262d;
  border: 1px solid #30363d;
  border-radius: 5px;
}
.line-actions button:hover {
  color: #ffffff;
  background: #30363d;
}
.terminal-footer {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  min-height: 30px;
  padding: 0 12px;
  font-size: 10px;
  color: #6e7681;
  background: #0b0f14;
  border-top: 1px solid #21262d;
}

@media (width <= 900px) {
  .toolbar {
    gap: 8px;
    align-items: flex-start;
  }
  .toolbar-left {
    flex-wrap: wrap;
  }
  .head-meta {
    display: none;
  }
  .terminal-head {
    flex-wrap: wrap;
    height: auto;
    padding: 9px 12px;
  }
  .terminal-title {
    order: 2;
    width: calc(100% - 54px);
  }
  .terminal-actions {
    order: 3;
    width: 100%;
    margin-left: 0;
  }
  .log-module {
    width: 100px;
  }
  .log-stack {
    margin-left: 200px;
  }
  .terminal-footer span:last-child {
    display: none;
  }
}
</style>
