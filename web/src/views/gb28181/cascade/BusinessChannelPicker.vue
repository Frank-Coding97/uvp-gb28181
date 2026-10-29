<template>
  <section class="business-channel-picker" :aria-label="targetCode ? '添加组织通道' : '添加共享通道'">
    <!--
      ⛔ 目标组织**必须显示出来**（2026-10-14 补）：弹窗标题里放不下完整路径（Arco 标题是
        `nowrap + ellipsis`），而"这些通道到底加进哪个节点"是整个操作最关键的一条信息。
        只在有目标节点（目录模式）时出现；「按通道共享」不归属组织，不显示。
    -->
    <p v-if="targetCode" class="picker-target" data-testid="picker-target">
      <span>目标组织</span>
      <strong>{{ targetPath }}</strong>
      <code>{{ targetCode }}</code>
    </p>
    <s-layout-search>
      <template #fields>
        <span class="picker-fields">
          <span class="picker-field picker-field--scope">
            <a-select :model-value="scope" data-action="scope-select" aria-label="通道范围" @change="onScopeChange">
              <a-option value="local">本地通道</a-option>
              <a-option value="shared">已共享通道</a-option>
            </a-select>
          </span>
          <span class="picker-field picker-field--status">
            <a-select
              :model-value="statusFilter"
              data-action="status-select"
              aria-label="在线状态"
              placeholder="全部状态"
              allow-clear
              @change="onStatusChange"
            >
              <a-option value="online">在线</a-option>
              <a-option value="offline">离线</a-option>
            </a-select>
          </span>
          <span class="picker-field picker-field--search">
            <a-input
              v-model="keyword"
              data-action="search-input"
              aria-label="搜索通道"
              :placeholder="scope === 'local' ? '通道名称或源国标编号' : '通道名称或发布国标编号'"
              allow-clear
              @press-enter="commitSearch"
            />
          </span>
        </span>
      </template>
      <template #actions><a-button type="primary" data-action="search" @click="commitSearch">查询</a-button></template>
    </s-layout-search>
    <p v-if="selectionNotice" class="picker-notice" role="status">{{ selectionNotice }}</p>

    <div v-if="scope === 'local' && loadError" class="picker-state-panel picker-error" role="alert">
      <span>通道加载失败：{{ loadError }}</span>
      <a-button size="small" data-action="retry" @click="loadLocalPage">重试</a-button>
    </div>
    <div v-else-if="!loading && !visibleCandidates.length" class="picker-state-panel" :data-action="emptyAction">
      <strong>{{ emptyTitle }}</strong>
      <span>{{ emptyDescription }}</span>
    </div>
    <!--
      ⭐⭐ 列宽策略（2026-10-14 用真实 Arco 在浏览器里量过，不是推算）：
      **「通道」「所属设备」两列不给 width**（auto），勾选 46 / 在线 84 / 当前归属 160 给死。
      Arco 的 table-layout 是 auto ⇒ 余量全部由 auto 列吃掉，写死的那三列**永远保持自己需要的宽度**。
      反面教材（改前）：五列都写 width，余量按声明值**等比摊**给所有列 ⇒ 只剩一颗药丸的
      「当前归属」也被摊到 260px，右侧一大片空白（就是那张截图的来源）。
      scroll.x=900 只作软下限（窄窗才出横向滚动）；容器更宽时靠 min-width:100% 撑满。
    -->
    <a-table
      v-else
      class="uvp-data-table picker-table-wrap"
      row-key="sourceChannelId"
      :data="visibleCandidates"
      :bordered="false"
      :pagination="false"
      :loading="{ loading, tip: '正在加载通道…' }"
      :scroll="{ x: 900 }"
    >
      <template #columns>
        <a-table-column :width="46"
          ><template #title>
            <a-checkbox
              data-action="select-page"
              aria-label="全选本页可选通道"
              :model-value="allSelectablePageSelected"
              :indeterminate="pageSelectionIndeterminate"
              :disabled="disabled || !selectablePageCandidates.length"
              @update:model-value="togglePageSelection(Boolean($event))"
            /> </template
          ><template #cell="{ record: candidate }">
            <a-checkbox
              :data-channel-id="candidate.sourceChannelId"
              :aria-label="`${candidate.name}选择`"
              :model-value="selectedById.has(candidate.sourceChannelId)"
              :disabled="disabled || !candidate.selectable"
              :title="candidate.disabledReason"
              @update:model-value="toggleCandidate(candidate, Boolean($event))"
            /> </template
        ></a-table-column>
        <a-table-column title="通道"
          ><template #cell="{ record: candidate }">
            <span class="picker-channel">
              <strong>{{ candidate.name || candidate.channelCode || `通道 ${candidate.sourceChannelId}` }}</strong>
              <small>{{ candidate.codeLabel }}：{{ candidate.channelCode || "暂无编号" }}</small>
              <small
                v-if="
                  candidate.projection && candidate.sourceChannelCode && candidate.sourceChannelCode !== candidate.channelCode
                "
              >
                源国标编号：{{ candidate.sourceChannelCode }}
              </small>
              <small v-if="candidate.disabledReason" class="picker-disabled-reason">{{ candidate.disabledReason }}</small>
            </span>
          </template></a-table-column
        >
        <a-table-column title="所属设备"
          ><template #cell="{ record: candidate }">
            <span class="picker-device">
              <span class="picker-mobile-label">所属设备</span>
              <strong>{{ candidate.deviceName || candidate.deviceCode || "设备信息暂不可读取" }}</strong>
              <small v-if="candidate.deviceName && candidate.deviceCode">{{ candidate.deviceCode }}</small>
            </span>
          </template></a-table-column
        >
        <a-table-column title="在线" :width="84"
          ><template #cell="{ record: candidate }">
            <span v-if="candidate.online === undefined" class="picker-online is-unknown" :title="'该通道的在线状态暂不可读取'"
              >未知</span
            >
            <span v-else class="picker-online" :class="candidate.online ? 'is-online' : 'is-offline'">
              <i class="picker-online-dot" aria-hidden="true"></i>
              {{ candidate.online ? "在线" : "离线" }}
            </span>
          </template></a-table-column
        >
        <a-table-column title="当前归属" :width="160"
          ><template #cell="{ record: candidate }">
            <span class="picker-membership">
              <span class="picker-badge" :class="`is-${candidate.membershipKind}`">{{ candidate.membership }}</span>
              <small v-if="candidate.originPath" class="picker-origin">原组织：{{ candidate.originPath }}</small>
            </span>
          </template></a-table-column
        >
      </template>
      <!--
        ⛔ 表格只会在「加载中」或「有数据」时渲染（空态由上面的独立面板接管），
        所以这里必须把 Arco 自带的空占位换成一块留白：否则首次加载时遮罩后面会透出
        「暂无数据」，看着像出错了。留白同时给遮罩撑出正常高度。
      -->
      <template #empty><span class="picker-table-hold" aria-hidden="true"></span></template>
    </a-table>
    <div class="picker-pagination" v-if="scope === 'local' && total > 0">
      <a-pagination
        :current="page"
        :total="total"
        :page-size="pageSize"
        :show-total="true"
        :disabled="loading"
        @change="changePage($event)"
      />
    </div>

    <footer class="picker-footer">
      <div class="picker-summary" aria-live="polite">
        <div class="picker-counts">
          <strong>已选择 {{ selectedCandidates.length }} 路</strong>
          <span>新增共享 {{ newShareCount }} 路</span>
          <span v-if="targetCode">移动 {{ moveCount }} 路</span>
        </div>
        <p v-if="moveConfirmation" class="picker-move-confirmation" role="status">
          再次确认将 {{ moveCount }} 路通道从 {{ moveSourcePaths }} 移出并加入目标组织。
        </p>
      </div>
      <div class="picker-actions">
        <a-button size="small" data-action="cancel" @click="$emit('cancel')">取消</a-button>
        <a-button
          size="small"
          type="primary"
          data-action="apply"
          :disabled="disabled || !selectedCandidates.length"
          @click="applySelection"
        >
          {{ applyButtonLabel }}
        </a-button>
      </div>
    </footer>
  </section>
</template>

<script setup lang="ts">
import {
  Table as ATable,
  TableColumn as ATableColumn,
  Checkbox as ACheckbox,
  Pagination as APagination
} from "@arco-design/web-vue";
import SLayoutSearch from "@/components/s-layout-search/index.vue";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import type { BusinessBinding, BusinessNode, CascadeChannelProjection } from "@/api/gb28181";
import { listChannels as listChannelPage, listDevices, type ChannelVO } from "@/views/gb28181/device-mgmt/api";
import { formatBusinessNodePath } from "./businessGroupState";

defineOptions({ name: "BusinessChannelPicker" });

const PAGE_SIZE = 20;
const GB_CODE = /^\d{20}$/;

interface Props {
  platformName: string;
  targetPath: string;
  targetCode: string;
  channels: CascadeChannelProjection[];
  bindings: BusinessBinding[];
  nodes: BusinessNode[];
  disabled?: boolean;
}

interface PickerCandidate {
  sourceChannelId: number;
  name: string;
  channelCode: string;
  codeLabel: string;
  sourceChannelCode: string;
  deviceCode: string;
  deviceName: string;
  online?: boolean;
  membership: string;
  membershipKind: "joined" | "shared" | "unshared" | "moving";
  originPath: string;
  projection?: CascadeChannelProjection;
  disabledReason: string;
  selectable: boolean;
}

const props = withDefaults(defineProps<Props>(), { disabled: false });
const emit = defineEmits<{
  apply: [rows: CascadeChannelProjection[]];
  cancel: [];
}>();

const scope = ref<"local" | "shared">("local");
const keyword = ref("");
const searchTerm = ref("");
const statusFilter = ref<"" | "online" | "offline">("");
const rows = ref<ChannelVO[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = PAGE_SIZE;
const loading = ref(false);
const loadError = ref("");
const selectionNotice = ref("");
const selectedById = ref(new Map<number, PickerCandidate>());
const localChannelCache = new Map<number, ChannelVO>();
const deviceNameByCode = ref<Record<string, string>>({});
const pendingDeviceCodes = new Set<string>();
const moveConfirmation = ref(false);
let requestSequence = 0;
let disposed = false;

const bindingByChannel = computed(() => {
  const result = new Map<number, BusinessBinding>();
  for (const binding of props.bindings) {
    if (!result.has(binding.sourceChannelId)) result.set(binding.sourceChannelId, binding);
  }
  return result;
});
const projectionByChannel = computed(() => new Map(props.channels.map(channel => [channel.sourceChannelId, channel])));
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)));

function originPath(code: string) {
  return formatBusinessNodePath(props.nodes, code) || code || "未知组织";
}

function membershipFor(sourceChannelId: number, projection?: CascadeChannelProjection) {
  if (!props.targetCode) {
    if (projection && projection.active !== false) {
      return { membership: "已共享", membershipKind: "shared" as const, originPath: "" };
    }
    return {
      membership: projection ? "已停用共享" : "未共享",
      membershipKind: "unshared" as const,
      originPath: ""
    };
  }
  const binding = bindingByChannel.value.get(sourceChannelId);
  if (binding?.nodeCode === props.targetCode) {
    return { membership: "已加入本组织", membershipKind: "joined" as const, originPath: "" };
  }
  if (binding) {
    return {
      membership: "待从其他组织移动",
      membershipKind: "moving" as const,
      originPath: originPath(binding.nodeCode)
    };
  }
  if (projection) return { membership: "已共享未分组", membershipKind: "shared" as const, originPath: "" };
  return { membership: "未共享", membershipKind: "unshared" as const, originPath: "" };
}

/** 状态筛选：在线/离线只看读到的状态；读不到状态的行（未知）只在「全部状态」下出现。 */
function matchStatus(candidate: PickerCandidate, status: "" | "online" | "offline") {
  if (!status) return true;
  return status === "online" ? candidate.online === true : candidate.online === false;
}

function localCandidate(channel: ChannelVO): PickerCandidate {
  const projection = projectionByChannel.value.get(channel.id);
  const membership = membershipFor(channel.id, projection);
  const disabledReason =
    !props.targetCode && projection && projection.active !== false
      ? "已共享，不能重复添加"
      : membership.membershipKind === "joined"
        ? "该通道已加入当前组织"
        : !projection && !GB_CODE.test(channel.channelId || "")
          ? "来源通道编号不是 20 位数字，不能新增共享"
          : "";
  return {
    sourceChannelId: channel.id,
    name: channel.alias?.trim() || channel.name?.trim() || channel.channelId,
    channelCode: projection?.publishedChannelId || channel.channelId,
    codeLabel: projection ? "发布国标编号" : "源国标编号",
    sourceChannelCode: channel.channelId,
    deviceCode: channel.deviceId,
    deviceName: deviceNameByCode.value[channel.deviceId] || "",
    online: channel.status === 1,
    ...membership,
    projection,
    disabledReason,
    selectable: !disabledReason
  };
}

function sharedCandidate(projection: CascadeChannelProjection): PickerCandidate {
  const source = localChannelCache.get(projection.sourceChannelId);
  const membership = membershipFor(projection.sourceChannelId, projection);
  const disabledReason =
    !props.targetCode && projection.active !== false
      ? "已共享，不能重复添加"
      : membership.membershipKind === "joined"
        ? "该通道已加入当前组织"
        : "";
  const deviceCode = source?.deviceId || "";
  return {
    sourceChannelId: projection.sourceChannelId,
    name: projection.name || source?.alias?.trim() || source?.name || projection.publishedChannelId,
    channelCode: projection.publishedChannelId,
    codeLabel: "发布国标编号",
    sourceChannelCode: source?.channelId || "",
    deviceCode: deviceCode || `设备记录 #${projection.sourceDeviceId}`,
    deviceName: deviceCode ? deviceNameByCode.value[deviceCode] || "" : "",
    online: source ? source.status === 1 : undefined,
    ...membership,
    projection,
    disabledReason,
    selectable: !disabledReason
  };
}

const localCandidates = computed(() => rows.value.map(localCandidate));
const sharedCandidates = computed(() => {
  const normalized = searchTerm.value.trim().toLocaleLowerCase();
  const status = statusFilter.value;
  return props.channels
    .map(sharedCandidate)
    .filter(candidate => matchStatus(candidate, status))
    .filter(candidate => !normalized || `${candidate.name} ${candidate.channelCode}`.toLocaleLowerCase().includes(normalized));
});
const visibleCandidates = computed(() => (scope.value === "local" ? localCandidates.value : sharedCandidates.value));
const selectablePageCandidates = computed(() => visibleCandidates.value.filter(candidate => candidate.selectable));
const selectedCandidates = computed(() => [...selectedById.value.values()]);
const newShareCount = computed(
  () => selectedCandidates.value.filter(candidate => !candidate.projection || candidate.projection.active === false).length
);
const movingCandidates = computed(() => selectedCandidates.value.filter(candidate => candidate.membershipKind === "moving"));
const moveCount = computed(() => movingCandidates.value.length);
const moveSourcePaths = computed(() => [...new Set(movingCandidates.value.map(candidate => candidate.originPath))].join("、"));
const applyButtonLabel = computed(() => {
  if (!props.targetCode) return "加入共享";
  if (moveConfirmation.value) return "确认移动并加入";
  if (moveCount.value) return `检查并继续（移动 ${moveCount.value} 路）`;
  return "加入组织";
});
const allSelectablePageSelected = computed(
  () =>
    selectablePageCandidates.value.length > 0 &&
    selectablePageCandidates.value.every(candidate => selectedById.value.has(candidate.sourceChannelId))
);
const pageSelectionIndeterminate = computed(
  () =>
    !allSelectablePageSelected.value &&
    selectablePageCandidates.value.some(candidate => selectedById.value.has(candidate.sourceChannelId))
);
const emptyTitle = computed(() => {
  if (scope.value === "shared") return searchTerm.value ? "没有匹配的已共享通道" : "当前没有已共享通道";
  return searchTerm.value ? "没有匹配的本地通道" : "当前账号没有可访问的本地通道";
});
const emptyDescription = computed(() => {
  if (scope.value === "shared") return "已共享通道按当前发布编号和名称搜索。";
  return searchTerm.value ? "试试通道名称或源国标编号。" : "本地通道列表由账号权限范围决定。";
});
const emptyAction = computed(() => (scope.value === "local" ? "empty-channels" : "empty-shared-channels"));

/**
 * 「所属设备」列要显示设备名称，而通道接口只给设备编码 —— 按编码回查设备列表补齐。
 * ⛔ 名称只是展示信息：查不到就退回编码，绝不能因为查名失败而挡住选通道。
 */
async function ensureDeviceNames(codes: string[]) {
  const missing = [...new Set(codes)].filter(code => code && !(code in deviceNameByCode.value) && !pendingDeviceCodes.has(code));
  if (!missing.length) return;
  missing.forEach(code => pendingDeviceCodes.add(code));
  await Promise.all(
    missing.map(async code => {
      try {
        const response = await listDevices({ q: code, page: 1, pageSize: 20 });
        const matched = response.data?.list?.find(device => device.deviceId === code);
        deviceNameByCode.value = {
          ...deviceNameByCode.value,
          [code]: matched?.alias?.trim() || matched?.name?.trim() || ""
        };
      } catch {
        // 读取失败不落空标记，翻页或重查时会再试一次
      } finally {
        pendingDeviceCodes.delete(code);
      }
    })
  );
}

watch(
  () => visibleCandidates.value.map(candidate => candidate.deviceCode).join(","),
  () => {
    ensureDeviceNames(visibleCandidates.value.map(candidate => candidate.deviceCode).filter(code => GB_CODE.test(code)));
  },
  { immediate: true }
);

async function loadLocalPage() {
  const sequence = ++requestSequence;
  loading.value = true;
  loadError.value = "";
  try {
    const response = await listChannelPage({
      q: searchTerm.value || undefined,
      status: statusFilter.value || undefined,
      page: page.value,
      pageSize
    });
    if (disposed || sequence !== requestSequence) return;
    if (response.code !== 0) throw new Error(response.message || "接口返回失败");
    rows.value = response.data?.list || [];
    total.value = response.data?.total || 0;
    rows.value.forEach(channel => localChannelCache.set(channel.id, channel));
  } catch (cause) {
    if (disposed || sequence !== requestSequence) return;
    loadError.value = cause instanceof Error ? cause.message : "未知错误";
    rows.value = [];
    total.value = 0;
  } finally {
    if (!disposed && sequence === requestSequence) loading.value = false;
  }
}

function changePage(nextPage: number) {
  page.value = Math.min(pageCount.value, Math.max(1, nextPage));
  loadLocalPage();
}

function clearSelection(notice: string) {
  selectedById.value.clear();
  moveConfirmation.value = false;
  selectionNotice.value = notice;
}

function changeScope(nextScope: "local" | "shared") {
  if (scope.value === nextScope) return;
  scope.value = nextScope;
  keyword.value = "";
  searchTerm.value = "";
  page.value = 1;
  loadError.value = "";
  loading.value = false;
  requestSequence += 1;
  clearSelection("范围已切换，已清除之前的选择。");
  if (nextScope === "local") loadLocalPage();
}

function onScopeChange(value: unknown) {
  changeScope(value === "shared" ? "shared" : "local");
}

function onStatusChange(value: unknown) {
  statusFilter.value = value === "online" || value === "offline" ? value : "";
  commitFilter();
}

function commitFilter() {
  page.value = 1;
  clearSelection("筛选条件已变更，已清除之前的选择。");
  if (scope.value === "local") loadLocalPage();
}

function commitSearch() {
  const nextSearch = keyword.value.trim();
  if (nextSearch === searchTerm.value) return;
  searchTerm.value = nextSearch;
  page.value = 1;
  clearSelection("搜索条件已变更，已清除之前的选择。");
  if (scope.value === "local") loadLocalPage();
}

function toggleCandidate(candidate: PickerCandidate, checked: boolean) {
  if (props.disabled || !candidate.selectable) return;
  const next = new Map(selectedById.value);
  if (checked) next.set(candidate.sourceChannelId, candidate);
  else next.delete(candidate.sourceChannelId);
  selectedById.value = next;
  moveConfirmation.value = false;
  selectionNotice.value = "";
}

/** 表头勾选框：勾上=把本页可选通道全选，取消=把本页已选通道摘掉（不影响其他页的已选）。 */
function togglePageSelection(checked: boolean) {
  const next = new Map(selectedById.value);
  selectablePageCandidates.value.forEach(candidate => {
    if (checked) next.set(candidate.sourceChannelId, candidate);
    else next.delete(candidate.sourceChannelId);
  });
  selectedById.value = next;
  moveConfirmation.value = false;
  selectionNotice.value = "";
}

function toProjection(candidate: PickerCandidate): CascadeChannelProjection {
  if (candidate.projection) return { ...candidate.projection, active: true };
  const channel = localChannelCache.get(candidate.sourceChannelId);
  return {
    // ⛔ 草稿行**没有**设备主键（通道接口只给设备国标编号），所以这里恒为 0。
    sourceDeviceId: 0,
    // ⛔⛔ 正因如此，**必须把设备国标编号带出去**：共享通道表的「在线状态」列
    //   要靠它去回查该设备的通道清单。少这一行，草稿行的在线状态会永远显示「未知」，
    //   而且一个请求都不会发（2026-10-14 实测踩过，见 BusinessGroupManager.ensureChannelStatuses）。
    sourceDeviceCode: channel?.deviceId || candidate.deviceCode || "",
    sourceChannelId: candidate.sourceChannelId,
    publishedChannelId: channel?.channelId || candidate.sourceChannelCode,
    name: channel?.alias?.trim() || channel?.name?.trim() || candidate.name,
    parentOverride: "",
    ptzAllowed: false,
    active: true
  };
}

function applySelection() {
  if (props.disabled || !selectedCandidates.value.length) return;
  if (moveCount.value > 0 && !moveConfirmation.value) {
    moveConfirmation.value = true;
    return;
  }
  emit("apply", selectedCandidates.value.map(toProjection));
  moveConfirmation.value = false;
}

onMounted(loadLocalPage);
onUnmounted(() => {
  disposed = true;
  requestSequence += 1;
});
</script>

<style scoped>
.business-channel-picker {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
}

/** 目标组织提示行：一条信息条，不抢焦点，但要一眼看到"加到哪里"。 */
.picker-target {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: baseline;
  padding: 8px 12px;
  margin: 0;
  font-size: 12px;
  line-height: 18px;
  color: var(--uvp-text-secondary);
  background: rgba(var(--arcoblue-6), 0.08);
  border-radius: 6px;
}
.picker-target strong {
  font-size: 13px;
  font-weight: 600;
  color: var(--uvp-text-primary);
  word-break: break-all;
}
.picker-target code {
  color: var(--uvp-text-tertiary);
}
.picker-pagination,
.picker-counts,
.picker-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.picker-footer {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
  padding-top: 12px;
}
.picker-pagination {
  justify-content: flex-end;
}

/**
 * ⛔ 三个筛选控件**必须先包在一个自己的一层容器 `.picker-fields` 里**，再各自套壳定宽。
 * 原因（踩过两次）：`s-layout-search` 的 fields 槽是 `flex-wrap: wrap` 的弹性容器，它的
 * 可用宽度 = **各子项 max-content 之和**；搜索框 flex-basis(190) 一旦大于自身的 max-content(≈183)，
 * 就必然换行 ⇒ 输入框单独掉到第二行（截图现象）。
 * 现在三个控件是 `.picker-fields` 的**内部**子项，外层容器只有一个子项、永不拆散，
 * 一行这件事由结构保证，不再依赖"把 basis 调到刚好够用"这种脆弱巧合。
 */
.picker-fields {
  display: flex;
  flex: 1 1 auto;
  gap: 12px;
  align-items: center;
  min-width: 0;
}

/* 让 fields 容器吃掉整行剩余宽度 ⇒ 查询按钮贴右（与设备管理页等基准页观感一致） */
.business-channel-picker :deep(.uvp-search-panel__fields) {
  flex: 1 1 auto;
  width: auto;
}
.picker-field {
  display: block;
  min-width: 0;
}
.picker-field :deep(.arco-select),
.picker-field :deep(.arco-input-wrapper) {
  width: 100%;
}
.picker-field--scope {
  flex: 0 0 136px;
}
.picker-field--status {
  flex: 0 0 112px;
}

/* 三个控件最小宽度合计 136+112+140+2×12 = 412px，弹窗内可用宽度远大于它 ⇒ 稳定一行 */
.picker-field--search {
  flex: 1 1 150px;
  min-width: 140px;
  max-width: 260px;
}
.picker-channel,
.picker-device,
.picker-membership {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.picker-channel small,
.picker-membership small,
.picker-counts span {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.picker-table-wrap {
  flex: 1;
  min-height: 180px;
  max-height: 55vh;
  overflow: auto;
}
.picker-table-hold {
  display: block;
  height: 120px;
}
.picker-mobile-label {
  display: none;
}
.picker-summary {
  min-width: 0;
}
.picker-notice,
.picker-move-confirmation {
  margin: 0;
  font-size: 12px;
  color: rgb(var(--warning-6));
}
.picker-disabled-reason {
  color: rgb(var(--danger-6)) !important;
}
.picker-device strong {
  font-weight: 500;
}
.picker-device small {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.picker-online {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 12px;
  line-height: 20px;
}
.picker-online-dot {
  width: 6px;
  height: 6px;
  background: currentColor;
  border-radius: 50%;
  box-shadow: 0 0 0 3px rgb(0 0 0 / 6%);
}
.picker-online.is-online {
  color: rgb(var(--success-6));
}
.picker-online.is-offline,
.picker-online.is-unknown {
  color: var(--uvp-text-tertiary);
}
.picker-badge {
  align-self: flex-start;
  padding: 0 10px;
  font-size: 12px;
  line-height: 20px;
  white-space: nowrap;
  border: 1px solid transparent;
  border-radius: 999px;
}
.picker-badge.is-joined {
  color: rgb(var(--success-6));
  background: rgba(var(--success-6), 0.12);
  border-color: rgba(var(--success-6), 0.3);
}
.picker-badge.is-moving {
  color: rgb(var(--warning-6));
  background: rgba(var(--warning-6), 0.12);
  border-color: rgba(var(--warning-6), 0.3);
}
.picker-badge.is-shared {
  color: rgb(var(--arcoblue-6));
  background: rgba(var(--arcoblue-6), 0.12);
  border-color: rgba(var(--arcoblue-6), 0.3);
}
.picker-badge.is-unshared {
  color: var(--uvp-text-tertiary);
  background: rgba(var(--gray-6), 0.12);
  border-color: rgba(var(--gray-6), 0.28);
}
.picker-origin {
  word-break: break-all;
}
.picker-state-panel {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 8px;
  align-items: center;
  justify-content: center;
  min-height: 180px;
  color: var(--uvp-text-tertiary);
}
.picker-error {
  color: rgb(var(--danger-6));
}

@media (width <= 700px) {
  /* 极窄（手机弹窗）才允许三个控件拆行，平时一律一行 */
  .picker-fields {
    flex-wrap: wrap;
  }
  .picker-field--search {
    flex: 1 1 100%;
    max-width: none;
  }
  .picker-footer {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
