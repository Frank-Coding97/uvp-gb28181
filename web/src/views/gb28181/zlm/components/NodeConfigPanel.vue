<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { storeToRefs } from "pinia";
import { Message, Modal } from "@arco-design/web-vue";
import { useSystemStore } from "@/store/modules/system";
import {
  getZLMNodeConfig,
  testZLMNodeConnection,
  updateZLMNodeConfig,
  type ConfigGroup,
  type ConfigItem,
  type UpdateConfigResp
} from "@/api/gb28181-zlm";
import {
  buildConfigChanges,
  configDictionaryOptions,
  configModePresentation,
  isConfigEditable,
  isHookRoutingChange,
  isNetworkPortChange,
  isWebRTCPortChange,
  orderConfigGroups,
  updateResultRows,
  type ConfigUpdateResultRow
} from "../nodeConfigState";
import { zlmErrorPresentation } from "./zlmFormatters";

const props = withDefaults(
  defineProps<{
    nodeId: number;
    editable?: boolean;
    /**
     * 当前账号能否重启节点；重启权限是 gb28181:zlm:restart，与配置修改权限是两回事。
     * 刻意不给默认值：undefined 表示"宿主没接重启入口"（旧页面），
     * false 才是"接了但没权限" —— 两种情况该说的话不一样。
     */
    canRestart?: boolean;
    /** 重启流程进行中（已受理但还没确认恢复）。由外层持有重启状态，面板只借它做禁用。 */
    restartBusy?: boolean;
  }>(),
  { editable: true, restartBusy: false }
);
const { dict } = storeToRefs(useSystemStore());
const emit = defineEmits<{
  dirtyChange: [dirty: boolean];
  pollingSkipped: [];
  saved: [result: UpdateConfigResp];
  /** 请求外层发起重启（确认弹窗、受理、轮询恢复都由外层负责，面板不重复一套）。 */
  restartRequested: [];
}>();

const groups = ref<ConfigGroup[]>([]);
const loading = ref(false);
const saving = ref(false);
const testing = ref(false);
const dirty = ref<Record<string, string>>({});
const searchKey = ref("");
const activeGroup = ref("");
const saveResults = ref<ConfigUpdateResultRow[]>([]);
// 保存后还需要人做的事（改端口 ⇒ 防火墙放行）。常驻在回读区上方，而不是一次性
// 弹窗：放行端口是离开页面之后才做的动作，弹窗一关就再也找不到了。
const saveHints = ref<string[]>([]);
// 上次保存里「已写入但重启才生效」的 key。单独存一份、不跟着 saveResults 走：
// saveResults 会被下一次轮询 refresh 清空，而「要重启才生效」这件事在重启发生前
// 一直成立 —— 10 秒后按钮自己消失比没有按钮更糟。
const pendingRestartKeys = ref<string[]>([]);
const loadError = ref("");

const dirtyCount = computed(() => Object.keys(dirty.value).length);
// 重启入口的禁用理由：能点但点了没反应是最差的状态，所以把"为什么现在不能重启"
// 直接写在按钮旁边，而不是让外层安静地 return 掉。
const restartBlockedReason = computed(() => {
  if (!pendingRestartKeys.value.length) return "";
  if (props.restartBusy) return "重启已受理，正在等待节点离线、心跳恢复和配置收敛";
  if (dirtyCount.value > 0) return `还有 ${dirtyCount.value} 项改动未保存，先保存或放弃草稿再重启`;
  return "";
});
const allItems = computed(() => groups.value.flatMap(group => group.items));
const filteredGroups = computed<ConfigGroup[]>(() => {
  const query = searchKey.value.trim().toLowerCase();
  if (!query) return groups.value;
  return groups.value
    .map(group => ({
      ...group,
      items: group.items.filter(item => item.key.toLowerCase().includes(query) || item.comment?.toLowerCase().includes(query))
    }))
    .filter(group => group.items.length > 0);
});
const currentGroup = computed(
  () => filteredGroups.value.find(group => group.name === activeGroup.value) ?? filteredGroups.value[0] ?? null
);
const configTableScroll = computed(() => ({ x: 980, ...(currentGroup.value?.items.length ? { y: "100%" } : {}) }));

function dirtyCountInGroup(group: ConfigGroup) {
  return group.items.filter(item => dirty.value[item.key] !== undefined).length;
}

async function refresh(options: { force?: boolean } = {}) {
  if (dirtyCount.value > 0 && !options.force) {
    emit("pollingSkipped");
    return false;
  }
  if (!Number.isSafeInteger(props.nodeId) || props.nodeId <= 0) return false;
  loading.value = true;
  loadError.value = "";
  try {
    const response = await getZLMNodeConfig(props.nodeId);
    if (response.code !== 0) throw new Error(response.message || "节点配置加载失败");
    groups.value = orderConfigGroups(response.data?.groups ?? []);
    dirty.value = {};
    saveResults.value = [];
    // force 只会由「进页面 / 换节点 / 重启已恢复」触发，这三种情况下上一次保存的
    // 重启待办都已经失效：换节点是别人的账，重启恢复后新值已经读回来了。
    // 15 秒轮询走的是非 force 路径，不会把待办冲掉。
    if (options.force) pendingRestartKeys.value = [];
    if (!groups.value.some(group => group.name === activeGroup.value)) activeGroup.value = groups.value[0]?.name ?? "";
    return true;
  } catch (error) {
    loadError.value = zlmErrorPresentation(error).label;
    return false;
  } finally {
    loading.value = false;
  }
}

function onChange(item: ConfigItem, value: string) {
  if (!props.editable || !isConfigEditable(item)) return;
  const next = { ...dirty.value };
  if (value === item.value) delete next[item.key];
  else next[item.key] = value;
  dirty.value = next;
  saveResults.value = saveResults.value.filter(row => row.key !== item.key);
}

function dictionaryOptions(item: ConfigItem) {
  const dictionary = dict.value?.find((entry: { code?: string }) => entry.code === item.dictCode);
  return configDictionaryOptions(item, dictionary?.list ?? []);
}

function resetItem(key: string) {
  const next = { ...dirty.value };
  delete next[key];
  dirty.value = next;
  saveResults.value = saveResults.value.filter(row => row.key !== key);
}

function discardDrafts() {
  dirty.value = {};
  saveResults.value = [];
  // 放弃草稿等于「我现在什么都不想改」，重启待办也跟着收掉；否则会出现
  // 「没有未保存改动、也没有回读记录，却还挂着一个重启按钮」的悬空状态。
  pendingRestartKeys.value = [];
  Message.info("未保存草稿已放弃");
}

function applyConfirmedValues(rows: ConfigUpdateResultRow[]) {
  const confirmed = new Map(rows.filter(row => row.tone !== "danger").map(row => [row.key, row.actualValue]));
  if (confirmed.size === 0) return;
  groups.value = groups.value.map(group => ({
    ...group,
    items: group.items.map(item => (confirmed.has(item.key) ? { ...item, value: confirmed.get(item.key) ?? item.value } : item))
  }));
  const next = { ...dirty.value };
  for (const key of confirmed.keys()) delete next[key];
  dirty.value = next;
}

async function handleSave() {
  if (!props.editable || saving.value) return;
  const changes = buildConfigChanges(allItems.value, dirty.value);
  if (Object.keys(changes).length === 0) {
    Message.info("没有可提交的配置改动");
    return;
  }
  saving.value = true;
  try {
    if (Object.keys(changes).some(isNetworkPortChange)) {
      const impact = ["网络端口保存后需重启媒体节点生效。"];
      if ("http.port" in changes) impact.push("修改 http.port 后还需同步节点登记的 API 端口。");
      if ("rtp_proxy.port" in changes) impact.push("修改单端口监听后需同步节点登记的收流端口；重启将影响该节点的现有播放。");
      // 平台会在保存后自动把节点登记的 RTP 收流范围对齐到这里的实际值（见后端
      // config_service.reconcileRTPPortRange），所以不再提示"还需手工同步登记范围"；
      // 剩下真正需要人做的就是防火墙放行。
      if ("rtp_proxy.port_range" in changes)
        impact.push("平台会自动同步节点登记的 RTP 收流范围，无需手工改动；请确认防火墙已按新范围放行。");
      const confirmed = await new Promise<boolean>(resolve => {
        Modal.warning({
          title: "修改网络端口配置",
          content: `${impact.join(" ")}请确认新端口可达，否则节点可能失联或无法收流。是否继续保存？`,
          hideCancel: false,
          okText: "继续保存",
          cancelText: "取消",
          onOk: () => resolve(true),
          onCancel: () => resolve(false)
        });
      });
      if (!confirmed) return;
    }
    if (Object.keys(changes).some(isWebRTCPortChange)) {
      const confirmed = await new Promise<boolean>(resolve => {
        Modal.warning({
          title: "修改 WebRTC 端口",
          content:
            "UDP 与 TCP 会自动对齐同号（rtc.port ↔ rtc.tcpPort、rtc.icePort ↔ rtc.iceTcpPort）。" +
            "新值会写回部署端口规划，重启后仍然生效；保存后需重启媒体节点才会真正换端口。" +
            "请确认防火墙已按新端口放行，否则浏览器能连上节点却拿不到画面。是否继续保存？",
          hideCancel: false,
          okText: "继续保存",
          cancelText: "取消",
          onOk: () => resolve(true),
          onCancel: () => resolve(false)
        });
      });
      if (!confirmed) return;
    }
    if (Object.keys(changes).some(isHookRoutingChange)) {
      const confirmed = await new Promise<boolean>(resolve => {
        Modal.warning({
          title: "修改 Hook 回调配置",
          content: "关闭 Hook 或修改回调地址可能导致节点心跳、播放鉴权、按需拉流、录像索引和流量统计失效。是否继续保存？",
          hideCancel: false,
          okText: "继续保存",
          cancelText: "取消",
          onOk: () => resolve(true),
          onCancel: () => resolve(false)
        });
      });
      if (!confirmed) return;
    }
    const response = await updateZLMNodeConfig(props.nodeId, changes);
    if (response.code !== 0) throw new Error(response.message || "配置保存失败");
    const result = response.data;
    const rows = updateResultRows(allItems.value, changes, result);
    saveResults.value = rows;
    saveHints.value = result.hints ?? [];
    // 只认后端返回的 requiresRestart，不从行里的 tone 反推 —— tone 还混着回读不一致，
    // 而"回读不一致"该做的是重查，不是重启。
    pendingRestartKeys.value = result.requiresRestart ?? [];
    applyConfirmedValues(rows);
    emit("saved", result);
    if (rows.some(row => row.tone === "danger")) Message.warning("配置命令已返回，但存在回读不一致或未确认项");
    else if (rows.some(row => row.tone === "warning")) Message.warning("配置已保存，部分配置需重启媒体节点后生效");
    else Message.success("配置已下发并完成实际值回读");
  } catch (error) {
    Message.error(zlmErrorPresentation(error).label);
  } finally {
    saving.value = false;
  }
}

async function handleTest() {
  if (testing.value) return;
  testing.value = true;
  try {
    const response = await testZLMNodeConnection(props.nodeId);
    if (response.code !== 0) throw new Error(response.message || "连通性探测失败");
    if (response.data.online) Message.success(`节点在线，HTTP 端口 ${response.data.httpPort || "未返回"}`);
    else Message.error("节点不可达，请检查候选地址、端口和密钥");
  } catch (error) {
    Message.error(zlmErrorPresentation(error).label);
  } finally {
    testing.value = false;
  }
}

watch(dirtyCount, () => emit("dirtyChange", dirtyCount.value > 0), { immediate: true });
watch(filteredGroups, next => {
  if (!next.some(group => group.name === activeGroup.value)) activeGroup.value = next[0]?.name ?? "";
});
watch(
  () => props.nodeId,
  () => {
    if (dirtyCount.value > 0) {
      emit("pollingSkipped");
      return;
    }
    void refresh({ force: true });
  }
);

onMounted(() => refresh({ force: true }));
defineExpose({ refresh, discardDrafts });
</script>

<template>
  <section class="node-config-panel">
    <s-layout-search class="config-search-panel">
      <template #fields>
        <a-input-search v-model="searchKey" placeholder="搜索配置 key 或说明" allow-clear class="config-search" />
        <span v-if="dirtyCount" class="draft-badge"><i />{{ dirtyCount }} 项未保存 · 轮询已暂停</span>
      </template>
      <template #actions>
        <a-button :loading="testing" @click="handleTest">测试连通性</a-button>
        <a-button class="uvp-page-action-btn uvp-refresh-btn" :loading="loading" :disabled="dirtyCount > 0" @click="refresh()"
          ><template #icon><icon-refresh /></template>刷新</a-button
        >
        <a-button v-if="dirtyCount" @click="discardDrafts">放弃草稿</a-button>
        <a-button v-if="editable" type="primary" :loading="saving" :disabled="dirtyCount === 0" @click="handleSave"
          >保存配置</a-button
        >
      </template>
    </s-layout-search>

    <a-alert v-if="loadError" type="error" class="config-alert">{{ loadError }}</a-alert>
    <a-alert v-else-if="!editable" type="info" class="config-alert">当前账号可查看配置，但没有配置修改权限。</a-alert>

    <!-- 保存后仍需人做的事（改端口 ⇒ 防火墙放行）。常驻而不是弹窗：放行动作是
         离开页面之后才做的，弹窗关了就没有第二次机会看到具体端口号。 -->
    <a-alert v-for="hint in saveHints" :key="hint" type="warning" class="config-alert">{{ hint }}</a-alert>

    <div v-if="saveResults.length" class="readback-panel">
      <header><strong>最近一次保存与回读</strong><span>命令返回不等于实际生效；以下四列分别保留事实。</span></header>
      <a-table :data="saveResults" :pagination="false" size="small" row-key="key">
        <template #columns>
          <a-table-column title="配置项" data-index="key" :width="260" />
          <a-table-column title="旧值" data-index="oldValue" />
          <a-table-column title="新值" data-index="newValue" />
          <a-table-column title="命令结果" :width="150"
            ><template #cell="{ record }"
              ><span :class="`result-${record.tone}`">{{ record.commandResult }}</span></template
            ></a-table-column
          >
          <a-table-column title="实际值" data-index="actualValue" />
        </template>
      </a-table>
    </div>

    <!-- 「保存完才发现还得去别处点重启」是这条链路上最容易漏的一步：保存结果里
         直接给出重启入口，并把「为什么现在点不了」写在旁边。 -->
    <div v-if="pendingRestartKeys.length" class="restart-cta">
      <div class="restart-cta__text">
        <strong>{{ pendingRestartKeys.length }} 项配置已保存，需重启媒体节点后生效</strong>
        <code class="restart-cta__keys">{{ pendingRestartKeys.join("  ") }}</code>
        <span v-if="restartBlockedReason" class="restart-cta__blocked">{{ restartBlockedReason }}</span>
        <span v-else-if="canRestart === false" class="restart-cta__blocked"
          >当前账号没有重启媒体的权限，请让有重启权限的账号执行；重启后这些项才会生效。</span
        >
        <span v-else-if="canRestart !== true" class="restart-cta__tip"
          >请到节点管理页对该节点执行重启，重启完成后这些项才会生效。</span
        >
        <span v-else class="restart-cta__tip">重启会中断该节点上全部媒体会话，页面会持续等到离线、心跳恢复和配置收敛。</span>
      </div>
      <a-button
        v-if="canRestart"
        status="danger"
        :loading="restartBusy"
        :disabled="!!restartBlockedReason"
        @click="emit('restartRequested')"
        >重启媒体节点</a-button
      >
    </div>

    <a-spin :loading="loading && groups.length === 0" class="config-spin">
      <div class="config-body">
        <aside class="category-tree" aria-label="配置分类">
          <div class="tree-title">配置分类</div>
          <button
            v-for="group in filteredGroups"
            :key="group.name"
            type="button"
            :class="['tree-switch', { active: group.name === currentGroup?.name }]"
            @click="activeGroup = group.name"
          >
            <span>{{ group.name }}</span
            ><span
              ><b v-if="dirtyCountInGroup(group)">{{ dirtyCountInGroup(group) }}</b
              >{{ group.items.length }}</span
            >
          </button>
          <a-empty v-if="filteredGroups.length === 0" description="未匹配到配置" />
        </aside>

        <section class="category-detail">
          <header v-if="currentGroup" class="detail-header">
            <div>
              <h2>{{ currentGroup.name }}</h2>
              <p>{{ currentGroup.items.length }} 项</p>
            </div>
          </header>
          <a-table
            v-if="currentGroup"
            :data="currentGroup.items"
            :pagination="false"
            :scroll="configTableScroll"
            row-key="key"
            class="uvp-data-table config-table"
          >
            <template #columns>
              <a-table-column title="配置项" :width="310"
                ><template #cell="{ record }"
                  ><div class="config-key">
                    <strong>{{ record.key }}</strong
                    ><span>{{ record.comment || "暂无说明" }}</span>
                  </div></template
                ></a-table-column
              >
              <a-table-column title="当前 / 草稿值"
                ><template #cell="{ record }">
                  <div v-if="dictionaryOptions(record).length" class="config-input">
                    <a-select
                      :model-value="dirty[record.key] ?? record.value"
                      :disabled="!editable || !isConfigEditable(record)"
                      :options="dictionaryOptions(record)"
                      :class="{ dirty: dirty[record.key] !== undefined }"
                      @change="(value: string | number) => onChange(record, String(value))"
                    />
                    <a-button v-if="dirty[record.key] !== undefined" size="mini" type="text" @click="resetItem(record.key)"
                      >还原</a-button
                    >
                  </div>
                  <div v-else-if="isConfigEditable(record)" class="config-input">
                    <a-input
                      :model-value="dirty[record.key] ?? record.value"
                      allow-clear
                      :disabled="!editable"
                      :class="{ dirty: dirty[record.key] !== undefined }"
                      @input="(value: string) => onChange(record, value)"
                    />
                    <a-button v-if="dirty[record.key] !== undefined" size="mini" type="text" @click="resetItem(record.key)"
                      >还原</a-button
                    >
                  </div>
                  <code v-else class="readonly-value">{{ record.value || "—" }}</code>
                </template></a-table-column
              >
              <a-table-column title="默认值" :width="140"
                ><template #cell="{ record }"
                  ><code>{{ record.default || "—" }}</code></template
                ></a-table-column
              >
              <a-table-column title="编辑模式" :width="230"
                ><template #cell="{ record }"
                  ><div class="mode-cell">
                    <span :class="`mode-${configModePresentation(record.mode).tone}`">{{
                      configModePresentation(record.mode).label
                    }}</span
                    ><small>{{ configModePresentation(record.mode).reason }}</small>
                  </div></template
                ></a-table-column
              >
            </template>
          </a-table>
        </section>
      </div>
    </a-spin>
  </section>
</template>

<style scoped>
.node-config-panel {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  color: var(--zlm-text-2);
}
.config-search-panel {
  flex: none;
}
.config-search {
  width: 300px;
}
.draft-badge {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-warn-600);
}
.draft-badge i {
  width: 7px;
  height: 7px;
  background: var(--zlm-warn-500);
  border-radius: 50%;
}
.config-alert {
  flex: none;
}
.readback-panel {
  flex: none;
  max-height: 220px;
  overflow: auto;
  background: var(--zlm-card);
  border: 1px solid var(--zlm-border);
  border-radius: var(--zlm-radius-lg);
}
.readback-panel header {
  display: flex;
  gap: 10px;
  align-items: baseline;
  padding: 12px 14px;
  border-bottom: 1px solid var(--zlm-border);
}
.readback-panel header strong {
  color: var(--zlm-text-1);
}
.readback-panel header span {
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-text-3);
}
.result-success {
  color: var(--zlm-success-600);
}
.result-warning {
  color: var(--zlm-warn-600);
}
.result-danger {
  color: var(--zlm-danger-600);
}
.restart-cta {
  display: flex;
  flex: none;
  gap: 14px;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px;
  background: var(--zlm-fill-1);
  border: 1px solid var(--zlm-warn-500);
  border-radius: var(--zlm-radius-lg);
}
.restart-cta__text {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.restart-cta__text strong {
  font-size: var(--zlm-fs-body);
  color: var(--zlm-text-1);
}
.restart-cta__keys {
  font-family: var(--zlm-font-mono);
  font-size: 11px;
  color: var(--zlm-text-3);
  word-break: break-all;
}
.restart-cta__tip,
.restart-cta__blocked {
  font-size: var(--zlm-fs-caption);
  line-height: 1.5;
}
.restart-cta__tip {
  color: var(--zlm-text-3);
}
.restart-cta__blocked {
  color: var(--zlm-warn-600);
}
.config-spin {
  display: flex;
  flex: 1;
  min-height: 0;
}
.config-body {
  display: grid;
  flex: 1;
  grid-template-columns: 220px minmax(0, 1fr);
  height: 100%;
  min-height: 0;
  overflow: hidden;
  background: var(--zlm-card);
  border: 1px solid var(--zlm-border);
  border-radius: var(--zlm-radius-lg);
}
.category-tree {
  display: flex;
  flex-direction: column;
  gap: 5px;
  min-height: 0;
  padding: 14px 10px;
  overflow-y: auto;
  background: var(--zlm-fill-1);
  border-right: 1px solid var(--zlm-border);
}
.tree-title {
  padding: 0 8px 8px;
  font-size: var(--zlm-fs-caption);
  font-weight: var(--zlm-fw-medium);
  color: var(--zlm-text-3);
}
.tree-switch {
  display: flex;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
  padding: 9px 10px;
  color: var(--zlm-text-2);
  text-align: left;
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: var(--zlm-radius-md);
}
.tree-switch:hover {
  background: var(--zlm-fill-2);
}
.tree-switch.active {
  color: var(--zlm-brand-600);
  background: var(--zlm-brand-50);
}
.tree-switch > span:last-child {
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-text-4);
}
.tree-switch b {
  margin-right: 7px;
  color: var(--zlm-warn-600);
}
.category-detail {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}
.detail-header {
  flex: none;
  padding: 15px 18px;
  border-bottom: 1px solid var(--zlm-border);
}
.detail-header h2 {
  margin: 0;
  font-size: 16px;
  color: var(--zlm-text-1);
}
.detail-header p {
  margin: 4px 0 0;
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-text-3);
}
.config-table {
  flex: 1;
  min-width: 900px;
  min-height: 0;
  overflow: hidden;
}
.config-table :deep(.arco-table-container) {
  height: 100%;
  border: 0;
  border-radius: 0;
  box-shadow: none;
}
.config-key {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.config-key strong,
code {
  font-family: var(--zlm-font-mono);
}
.config-key strong {
  color: var(--zlm-text-1);
  overflow-wrap: anywhere;
}
.config-key span {
  margin-top: 3px;
  font-size: 11px;
  color: var(--zlm-text-3);
}
.config-input {
  display: flex;
  gap: 5px;
  align-items: center;
}
.config-input :deep(.dirty) {
  border-color: var(--zlm-warn-500);
}
.readonly-value {
  color: var(--zlm-text-3);
  overflow-wrap: anywhere;
}
.mode-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.mode-cell > span {
  width: fit-content;
  padding: 2px 7px;
  font-size: 11px;
  border-radius: 999px;
}
.mode-cell small {
  line-height: 1.45;
  color: var(--zlm-text-3);
}
.mode-success {
  color: var(--zlm-success-600);
  background: var(--zlm-success-50);
}
.mode-warning {
  color: var(--zlm-warn-600);
  background: var(--zlm-warn-50);
}
.mode-danger {
  color: var(--zlm-danger-600);
  background: var(--zlm-danger-50);
}
.mode-neutral {
  color: var(--zlm-text-3);
  background: var(--zlm-fill-2);
}

@media (width <= 820px) {
  .config-search {
    width: 100%;
  }
  .readback-panel {
    overflow-x: auto;
  }
  .config-body {
    grid-template-columns: 1fr;
  }
  .category-tree {
    flex-direction: row;
    overflow-x: auto;
    border-right: 0;
    border-bottom: 1px solid var(--zlm-border);
  }
  .tree-title {
    display: none;
  }
  .tree-switch {
    flex: 0 0 auto;
  }
  .category-detail {
    min-height: 420px;
  }
}
</style>
