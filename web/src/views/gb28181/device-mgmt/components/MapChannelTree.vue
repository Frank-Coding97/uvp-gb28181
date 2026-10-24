<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ChevronRight, Circle, MapPin, MapPinned, RefreshCcw, Search } from "@lucide/vue";
import { Message } from "@arco-design/web-vue";
import { getMapChannelTree, listDirectoryTree, type DirectoryNode, type MapTreeChannel, type MapTreeDevice } from "../api";

/**
 * 地图视角左栏的「可定位通道树」。
 *
 * 为什么地图视角要换掉设备目录树：地图是**点位目录** —— 它只认"有经纬度的通道"，
 * 而目录树里大部分节点（枪机、球机、没采过点的执法仪）在图上根本没有点位，
 * 列出来只会让人一路点下去发现"点了没反应"。所以这里只列真正能飞过去的通道。
 *
 * ⛔ 数据必须走后端 `map/channel-tree`，**不能拿地图上已有的 marker 反推**：
 *    markers 是按当前视野查的，用户一拖地图左侧就会凭空出现/消失一批设备。
 * ⛔ 搜索/状态/范围三个条件的口径必须与 markers 完全一致（后端共用同一套过滤），
 *    否则会出现"地图上有点、左侧树里没有"的两套口径。
 * ⛔ 搜索走后端（`q`），不做页内过滤：这棵树有 1000 条上限，页内过滤会让
 *    "被上限挤掉的通道"与"不存在"表现完全一样。
 */
export type MapScopeSelection = { view: "department" | "business"; node: DirectoryNode };

type ScopeOption = { value: string; label: string; view: "department" | "business"; node: DirectoryNode };

const props = defineProps<{
  /** 当前生效的范围，形如 `department|<key>`；空串=全部。由父级按 directoryState 派生。 */
  scopeValue: string;
  /** 当前范围的可读名（范围是行政区划时下拉里没有对应项，靠它补一个选项）。 */
  scopeLabel?: string;
  /** 地图上当前选中的通道 id，用于高亮。 */
  selectedChannelId?: number | null;
  /** 与页面顶部一致的在线状态筛选。 */
  status?: "online" | "offline" | "";
  /**
   * 与顶部搜索框**共用同一份关键词**（v-model:keyword）。
   * ⛔ 不要各存一份：顶部搜"西门"而左栏还列着全部，就是两套口径。
   */
  keyword?: string;
}>();

const emit = defineEmits<{
  locate: [channel: MapTreeChannel];
  focus: [device: MapTreeDevice];
  scopeChange: [payload: MapScopeSelection | null];
  "update:keyword": [value: string];
}>();

const devices = ref<MapTreeDevice[]>([]);
const total = ref(0);
const truncated = ref(false);
const noCoordCount = ref(0);
const loading = ref(false);
const keyword = ref(props.keyword ?? "");
const collapsed = ref<Set<string>>(new Set());
const scopeGroups = ref<Array<{ label: string; options: ScopeOption[] }>>([]);

let requestSeq = 0;
let keywordTimer: ReturnType<typeof setTimeout> | undefined;
let disposed = false;

/** 范围里如果有"下拉表达不了"的取值（例如行政区划），补一个选项：
 *  否则下拉会显示"全部通道"，而地图其实已被过滤 —— 界面自相矛盾。 */
const externalScopeLabel = computed(() => {
  if (!props.scopeValue || !props.scopeLabel) return "";
  const known = scopeGroups.value.some(group => group.options.some(option => option.value === props.scopeValue));
  return known ? "" : props.scopeLabel;
});

function channelName(channel: MapTreeChannel) {
  return channel.alias?.trim() || channel.name?.trim() || channel.channelId;
}

function deviceName(device: MapTreeDevice) {
  return device.alias?.trim() || device.name?.trim() || device.deviceId;
}

function isExpanded(deviceId: string) {
  // 搜索时强制展开：命中项被折叠藏起来等于搜不到
  if (keyword.value.trim()) return true;
  return !collapsed.value.has(deviceId);
}

function toggle(deviceId: string) {
  const next = new Set(collapsed.value);
  if (next.has(deviceId)) next.delete(deviceId);
  else next.add(deviceId);
  collapsed.value = next;
}

function scopeQuery() {
  if (!props.scopeValue) return {};
  const [view, ...rest] = props.scopeValue.split("|");
  const key = rest.join("|");
  // 三种目录视图都要透传（行政区划也能筛地图），否则会出现
  // "地图被行政区划收窄、左栏还列着全国通道"的两套口径。
  if (!key || (view !== "administrative" && view !== "department" && view !== "business")) return {};
  return { directoryView: view as "administrative" | "department" | "business", directoryKey: key };
}

async function load() {
  const seq = ++requestSeq;
  loading.value = true;
  try {
    const response = await getMapChannelTree({
      ...scopeQuery(),
      q: keyword.value.trim() || undefined,
      status: props.status || undefined
    });
    if (disposed || seq !== requestSeq) return;
    if (response.code !== 0) throw new Error(response.message || "点位加载失败");
    devices.value = response.data?.devices || [];
    total.value = response.data?.total || 0;
    truncated.value = !!response.data?.truncated;
    noCoordCount.value = response.data?.noCoordCount || 0;
  } catch (error: any) {
    if (disposed || seq !== requestSeq) return;
    Message.error(error?.message || "点位加载失败");
  } finally {
    if (seq === requestSeq) loading.value = false;
  }
}

/**
 * 范围下拉候选：部门 + 业务分组，各取前两层。
 * 再深的层级不是"范围"而是"定位到某个节点"，塞进下拉只会让人翻半分钟。
 */
async function loadScopeOptions() {
  const groups: Array<{ label: string; options: ScopeOption[] }> = [];
  const flatten = (nodes: DirectoryNode[], view: "department" | "business", depth: number, out: ScopeOption[]) => {
    for (const node of nodes) {
      out.push({ value: `${view}|${node.key}`, label: `${"— ".repeat(depth)}${node.name}`, view, node });
      if (depth < 1 && node.children?.length) flatten(node.children, view, depth + 1, out);
    }
    return out;
  };
  for (const [view, label] of [
    ["department", "部门"],
    ["business", "业务分组"]
  ] as const) {
    try {
      const response = await listDirectoryTree(view);
      if (disposed) return;
      if (response.code !== 0) continue;
      const options = flatten(response.data?.list || [], view, 0, []);
      if (options.length) groups.push({ label, options });
    } catch {
      // 范围只是辅助条件：拿不到就少一个下拉分组，不影响点位列表本身
    }
  }
  scopeGroups.value = groups;
}

function onScopeSelect(value: unknown) {
  const next = typeof value === "string" ? value : "";
  if (!next) {
    emit("scopeChange", null);
    return;
  }
  for (const group of scopeGroups.value) {
    const option = group.options.find(item => item.value === next);
    if (option) {
      emit("scopeChange", { view: option.view, node: option.node });
      return;
    }
  }
  // 选中"当前外部范围"（行政区划，下拉里没有这一项）：不做切换，保持原样
}

async function refresh() {
  await load();
}

defineExpose({ refresh });

watch(
  () => [props.scopeValue, props.status],
  () => void load()
);

// 顶部搜索框改了关键词，左栏要跟着走（双向同步，见 props.keyword 注释）
watch(
  () => props.keyword,
  value => {
    const next = value ?? "";
    if (next !== keyword.value) keyword.value = next;
  }
);

watch(keyword, value => {
  emit("update:keyword", value);
  clearTimeout(keywordTimer);
  keywordTimer = setTimeout(() => void load(), 300);
});

onMounted(async () => {
  await loadScopeOptions();
  if (!disposed) await load();
});

onBeforeUnmount(() => {
  disposed = true;
  clearTimeout(keywordTimer);
});
</script>

<template>
  <aside class="directory-panel map-channel-tree" aria-label="可定位通道">
    <div class="directory-head">
      <div class="directory-title"><MapPinned :size="14" /><span>可定位通道</span></div>
      <div class="directory-actions">
        <a-tooltip content="刷新点位">
          <button class="icon-button uvp-refresh-btn" type="button" aria-label="刷新点位" @click="refresh()">
            <RefreshCcw :size="13" :class="{ spin: loading }" />
          </button>
        </a-tooltip>
      </div>
    </div>

    <!-- ⛔ 不能用 <label> 包 Arco 控件：label 的隐式激活会让下拉刚点开就被第二下关掉
         （原生 <select> 不看重复 click，所以以前没暴露）。 -->
    <div class="mct-scope">
      <span class="mct-scope__label">范围</span>
      <a-select
        class="mct-scope__select"
        size="small"
        :model-value="scopeValue"
        :bordered="false"
        :trigger-props="{ autoFitPopupMinWidth: true }"
        data-testid="map-scope-select"
        aria-label="地图范围"
        @change="onScopeSelect"
      >
        <a-option value="">全部通道</a-option>
        <a-option v-if="externalScopeLabel" :value="scopeValue">{{ externalScopeLabel }}</a-option>
        <a-optgroup v-for="group in scopeGroups" :key="group.label" :label="group.label">
          <a-option v-for="option in group.options" :key="option.value" :value="option.value">{{ option.label }}</a-option>
        </a-optgroup>
      </a-select>
    </div>

    <label class="directory-search">
      <Search :size="13" />
      <input
        v-model="keyword"
        data-testid="map-channel-search"
        type="search"
        aria-label="搜索可定位通道"
        placeholder="搜索通道 / 编码"
      />
    </label>

    <a-spin :loading="loading" class="directory-tree-wrap">
      <div class="directory-tree" role="tree">
        <template v-for="device in devices" :key="device.deviceId">
          <div class="directory-row directory-row-device">
            <button
              class="twist-button"
              :class="{ expanded: isExpanded(device.deviceId) }"
              type="button"
              :aria-label="isExpanded(device.deviceId) ? '收起该设备' : '展开该设备'"
              @click.stop="toggle(device.deviceId)"
            >
              <ChevronRight :size="12" />
            </button>
            <button class="directory-node directory-device-node" type="button" @click="emit('focus', device)">
              <span class="device-node-icon" :class="{ online: device.online }"><Circle :size="8" fill="currentColor" /></span>
              <span class="device-node-copy">
                <span class="node-name">{{ deviceName(device) }}</span>
                <span class="device-node-code">{{ device.deviceId }}</span>
              </span>
              <span class="node-count">{{ device.channels.length }} 点</span>
            </button>
          </div>
          <div
            v-for="channel in isExpanded(device.deviceId) ? device.channels : []"
            :key="channel.id"
            class="directory-row mct-channel-row"
            :class="{ active: selectedChannelId === channel.id }"
            role="treeitem"
            :aria-selected="selectedChannelId === channel.id"
          >
            <button class="directory-node" type="button" @click="emit('locate', channel)">
              <MapPin :size="13" />
              <span class="node-name">{{ channelName(channel) }}</span>
              <span class="channel-status" :class="{ online: channel.status === 1 }"
                ><Circle :size="8" fill="currentColor" />{{ channel.status === 1 ? "在线" : "离线" }}</span
              >
            </button>
          </div>
        </template>
        <div v-if="!loading && !devices.length" class="directory-empty">
          <template v-if="keyword.trim()">没有匹配的可定位通道</template>
          <template v-else>当前范围还没有设置位置信息的通道<br />可在通道详情里手动标一个位置</template>
        </div>
      </div>
    </a-spin>

    <div v-if="truncated || noCoordCount > 0" class="mct-foot">
      <a-tooltip v-if="truncated" content="命中点位过多，只列出前 1000 个；用搜索或范围收窄" position="top">
        <span class="mct-hint mct-hint-warn">点位过多，仅显示前 {{ total }} 个</span>
      </a-tooltip>
      <a-tooltip v-if="noCoordCount > 0" content="这些通道没有经纬度，地图上没有点位；可在通道详情里设置位置" position="top">
        <span class="mct-hint">另有 {{ noCoordCount }} 个通道未设置位置</span>
      </a-tooltip>
    </div>
  </aside>
</template>

<style scoped>
.map-channel-tree {
  width: 240px;
  min-width: 240px;
}

.mct-scope {
  display: flex;
  flex: 0 0 auto;
  gap: 7px;
  align-items: center;
  height: 32px;
  min-height: 32px;
  padding: 0 9px;
  margin: 8px 12px 0;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-page-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
}

/* ⛔ 这条不能写成 `.mct-scope > span`：Arco select 的根元素**就是 span**，
 *    会被一起套上 `flex: 0 0 auto` ⇒ 下拉不肯收缩、直接顶出小盒子（实测溢出 21px）。 */
.mct-scope__label {
  flex: 0 0 auto;
}

.mct-scope__select {
  flex: 1 1 auto;
  width: auto;
  min-width: 0;
  font-size: 12px;
}

/* 收起态要和旁边那行「范围」文字同色同高：Arco 默认带灰底和描边，
 * 塞进这个已经画好边框的小盒子里会变成"盒中盒"。
 * ⛔ Arco select 的根元素自己就带 `.arco-select-view`，必须写成同一元素上的两个类；
 *    写成后代选择器（`:deep`）匹配不到自己，灰底会一直在。 */
.mct-scope__select.arco-select-view {
  height: 24px;
  padding: 0;
  font-size: 12px;
  color: var(--uvp-text-primary);
  background: transparent;
}

.mct-scope__select :deep(.arco-select-view-value) {
  padding: 0;
  font-size: 12px;
}

.mct-channel-row {
  height: 30px;
  padding-left: 22px;
}

.mct-foot {
  display: flex;
  flex: 0 0 auto;
  flex-direction: column;
  gap: 4px;
  padding: 8px 12px 10px;
  border-top: 1px solid var(--uvp-panel-border);
}

.mct-hint {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11px;
  line-height: 1.4;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}

.mct-hint-warn {
  color: var(--uvp-warning);
}

/* 与设备目录树共用一套观感（类名、间距、颜色变量都沿用 DirectoryPanel） */
.directory-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
  color: var(--uvp-text-secondary);
  background: var(--uvp-panel-bg);
  border-right: 1px solid var(--uvp-panel-border);
}

.directory-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 42px;
  min-height: 42px;
  padding: 0 10px 0 14px;
  border-bottom: 1px solid var(--uvp-panel-border);
}

.directory-title,
.directory-actions,
.directory-node {
  display: flex;
  align-items: center;
}

.directory-title {
  gap: 7px;
  font-size: 13px;
  font-weight: 620;
  color: var(--uvp-text-primary);
}

.directory-actions {
  gap: 2px;
}

.icon-button,
.twist-button {
  display: inline-grid;
  place-items: center;
  width: 26px;
  height: 26px;
  padding: 0;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 5px;
}

.icon-button:hover,
.twist-button:hover {
  color: var(--uvp-brand);
  background: var(--uvp-sidebar-active-bg);
}

.directory-search {
  display: flex;
  gap: 7px;
  align-items: center;
  height: 32px;
  min-height: 32px;
  padding: 0 9px;
  margin: 8px 12px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-page-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
}

.directory-search:focus-within {
  border-color: var(--uvp-brand);
}

.directory-search input {
  width: 100%;
  min-width: 0;
  font-size: 12px;
  color: var(--uvp-text-primary);
  outline: none;
  background: transparent;
  border: 0;
}

.directory-tree-wrap {
  flex: 1;
  min-height: 0;
}

.directory-tree {
  box-sizing: border-box;
  height: 100%;
  padding: 2px 6px 10px;
  overflow: auto;
  scrollbar-gutter: stable;
}

.directory-row {
  display: flex;
  align-items: center;
  min-width: 0;
  height: 32px;
  border-radius: 5px;
}

.directory-row-device {
  height: 42px;
}

.directory-row:hover,
.directory-row.active {
  color: var(--uvp-text-primary);
  background: var(--uvp-sidebar-active-bg);
}

.directory-row.active {
  box-shadow: inset 2px 0 0 var(--uvp-brand);
}

.twist-button {
  flex: 0 0 22px;
  width: 22px;
  height: 22px;
  transition: transform 0.15s ease;
}

.twist-button.expanded {
  transform: rotate(90deg);
}

.directory-node {
  flex: 1;
  gap: 7px;
  min-width: 0;
  height: 100%;
  padding: 0 4px 0 2px;
  color: inherit;
  text-align: left;
  cursor: pointer;
  background: transparent;
  border: 0;
}

.directory-node > svg {
  flex: 0 0 auto;
  color: var(--uvp-brand);
}

.device-node-icon {
  display: inline-grid;
  flex: 0 0 16px;
  place-items: center;
  color: var(--uvp-danger);
}

.device-node-icon.online {
  color: #158052;
}

:global(body[arco-theme="dark"] .map-channel-tree .device-node-icon.online) {
  color: #86efac;
}

.device-node-copy {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  line-height: 1.15;
}

.device-node-code {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}

.node-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.node-count {
  flex: 0 0 auto;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}

.channel-status {
  display: inline-flex;
  flex: 0 0 auto;
  gap: 4px;
  align-items: center;
  font-size: 11px;
  color: #f53f3f;
  white-space: nowrap;
}

.channel-status.online {
  color: #00b42a;
}

.directory-empty {
  display: grid;
  place-items: center;
  min-height: 120px;
  padding: 0 14px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--uvp-text-tertiary);
  text-align: center;
}

.spin {
  animation: map-channel-tree-spin 0.9s linear infinite;
}

@keyframes map-channel-tree-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (width <= 900px) {
  .map-channel-tree {
    width: 100%;
    min-width: 0;
    max-height: 320px;
    border-right: 0;
    border-bottom: 1px solid var(--uvp-panel-border);
  }

  .directory-tree-wrap {
    min-height: 160px;
  }
}
</style>
