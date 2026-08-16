<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import {
    ArrowRight,
    Building2,
    ChevronDown,
    CircleAlert,
    Cctv,
    Folder,
    FolderOpen,
    RefreshCcw,
    Search,
    Share2,
    UserRoundCog,
    X
} from "lucide-vue-next";
import { getDivisionAPI, type DivisionItem } from "@/api/department";
import { useUserStoreHook } from "@/store/modules/user";
import type { DeviceVO, OnlineStatus } from "@/views/gb28181/device-mgmt/api";
import { assignDeptDevices, assignDevices, listAssignmentDevices, type AssignmentFilter } from "./api";
import SharePanel, { type ShareDeviceBrief } from "./components/SharePanel.vue";

// ---- 权限 ----
const permissions = computed(() => useUserStoreHook().account?.permissions ?? []);
const canAssign = computed(() => permissions.value.includes("*:*:*") || permissions.value.includes("gb28181:device:assign"));
const canShare = computed(() => permissions.value.includes("*:*:*") || permissions.value.includes("gb28181:device:share"));

// ---- 左侧部门树 ----
const deptTree = ref<DivisionItem[]>([]);
const deptSearchKeyword = ref("");
const filteredDeptTree = computed(() => {
    if (!deptSearchKeyword.value.trim()) return deptTree.value;
    const keyword = deptSearchKeyword.value.trim();
    const match = (nodes: DivisionItem[]): DivisionItem[] =>
        nodes
            .map((node) => ({ ...node, children: node.children?.length ? match(node.children) : [] }))
            .filter((node) => node.name.includes(keyword) || (node.children?.length ?? 0) > 0);
    return match(deptTree.value);
});
const selectedDeptId = ref<number | undefined>(undefined);

const deptNameById = computed(() => {
    const map = new Map<number, string>();
    const walk = (nodes: DivisionItem[]) => {
        for (const node of nodes) {
            map.set(node.id, node.name);
            if (node.children?.length) walk(node.children);
        }
    };
    walk(deptTree.value);
    return map;
});

const loadDeptTree = async () => {
    try {
        const { data } = await getDivisionAPI();
        deptTree.value = data?.list ?? [];
    } catch (error: unknown) {
        Message.error(error instanceof Error ? error.message : "加载部门列表失败");
    }
};

const onSelectDept = (keys: Array<string | number>) => {
    selectedDeptId.value = keys.length ? Number(keys[0]) : undefined;
    page.value = 1;
    void loadDevices();
};

// ---- 分配状态 Tab ----
const assignmentFilter = ref<AssignmentFilter>("all");
const assignmentTabs: Array<{ value: AssignmentFilter; label: string }> = [
    { value: "all", label: "全部" },
    { value: "unassigned", label: "未分配" },
    { value: "assigned", label: "已分配" }
];

const switchAssignment = (value: AssignmentFilter) => {
    assignmentFilter.value = value;
    page.value = 1;
    selectedRowKeys.value = [];
    void loadDevices();
};

// ---- 设备列表 ----
const keyword = ref("");
const statusFilter = ref<OnlineStatus | undefined>(undefined);
const devices = ref<DeviceVO[]>([]);
const rowsLoading = ref(false);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const selectedRowKeys = ref<number[]>([]);

const tablePagination = computed(() => ({
    total: total.value,
    current: page.value,
    pageSize: pageSize.value,
    showTotal: true,
    showPageSize: true
}));

const loadDevices = async () => {
    rowsLoading.value = true;
    try {
        const { data } = await listAssignmentDevices({
            page: page.value,
            pageSize: pageSize.value,
            q: keyword.value || undefined,
            status: statusFilter.value,
            assignment: assignmentFilter.value,
            deptId: selectedDeptId.value
        });
        devices.value = data?.list ?? [];
        total.value = data?.total ?? 0;
    } catch (error: unknown) {
        Message.error(error instanceof Error ? error.message : "加载设备列表失败");
    } finally {
        rowsLoading.value = false;
    }
};

const onSearch = () => {
    page.value = 1;
    void loadDevices();
};

const clearKeyword = () => {
    keyword.value = "";
    onSearch();
};

const onPageChange = (nextPage: number) => {
    page.value = nextPage;
    void loadDevices();
};

const onPageSizeChange = (nextSize: number) => {
    pageSize.value = nextSize;
    page.value = 1;
    void loadDevices();
};

const deptNameText = (device: DeviceVO & { ownerDeptId?: number }): string => {
    const deptId = device.ownerDeptId;
    if (!deptId) return "-";
    return deptNameById.value.get(deptId) ?? `部门 #${deptId}`;
};

// ---- 分配弹窗(整部门 / 批量 / 单台 三来源共用) ----
type AssignMode = "dept" | "devices";
const assignVisible = ref(false);
const assignMode = ref<AssignMode>("devices");
const assignSourceDeptName = ref("");
const assignSourceDeptId = ref<number | undefined>(undefined);
const assignPendingIds = ref<number[]>([]);
const assignTargetDeptId = ref<number | undefined>(undefined);
const assignSubmitting = ref(false);

const assignSummary = computed(() => {
    if (assignMode.value === "dept") {
        return `源部门「${assignSourceDeptName.value}」的全部设备将流转到目标部门`;
    }
    return `已选 ${assignPendingIds.value.length} 台设备将流转到目标部门`;
});

/** 源栏设备清单:全部展示,超出高度出现垂直滚动条(跨页勾选的设备名回退为编号) */
const assignPendingDevices = computed(() => {
    const byId = new Map(devices.value.map((d) => [d.id, d]));
    return assignPendingIds.value.map((id) => ({
        id,
        name: byId.get(id)?.name || byId.get(id)?.deviceId || `设备 #${id}`
    }));
});

const assignTargetDeptName = computed(() => {
    if (!assignTargetDeptId.value) return "";
    return deptNameById.value.get(assignTargetDeptId.value) ?? "";
});

const confirmAssignText = computed(() =>
    assignTargetDeptName.value ? `确认流转到「${assignTargetDeptName.value}」` : "请先选择目标部门"
);

const onSelectTargetDept = (keys: Array<string | number>) => {
    assignTargetDeptId.value = keys.length ? Number(keys[0]) : undefined;
};

const openDeptAssign = (node: DivisionItem) => {
    assignMode.value = "dept";
    assignSourceDeptName.value = node.name;
    assignSourceDeptId.value = node.id;
    assignTargetDeptId.value = undefined;
    assignVisible.value = true;
};

const openBatchAssign = () => {
    if (selectedRowKeys.value.length === 0) {
        Message.warning("请先勾选设备");
        return;
    }
    assignMode.value = "devices";
    assignPendingIds.value = [...selectedRowKeys.value];
    assignTargetDeptId.value = undefined;
    assignVisible.value = true;
};

const openRowAssign = (device: DeviceVO) => {
    assignMode.value = "devices";
    assignPendingIds.value = [device.id];
    assignTargetDeptId.value = undefined;
    assignVisible.value = true;
};

const submitAssign = async () => {
    if (!assignTargetDeptId.value) {
        Message.warning("请选择目标部门");
        return;
    }
    assignSubmitting.value = true;
    try {
        if (assignMode.value === "dept") {
            const { data } = await assignDeptDevices(assignSourceDeptId.value!, assignTargetDeptId.value);
            Message.success(`整部门分配完成:${data?.succeeded ?? 0}/${data?.total ?? 0} 台`);
        } else {
            const { data } = await assignDevices(assignPendingIds.value, assignTargetDeptId.value);
            const succeeded = data?.results?.filter((r) => r.success).length ?? 0;
            const failed = (data?.results?.length ?? 0) - succeeded;
            Message.success(`分配完成:成功 ${succeeded} 台${failed > 0 ? `,失败 ${failed} 台` : ""}`);
        }
        assignVisible.value = false;
        selectedRowKeys.value = [];
        await loadDevices();
    } catch (error: unknown) {
        Message.error(error instanceof Error ? error.message : "分配失败");
    } finally {
        assignSubmitting.value = false;
    }
};

// ---- 共享 ----
const shareVisible = ref(false);
const shareDevices = ref<ShareDeviceBrief[]>([]);

const openRowShare = (device: DeviceVO) => {
    shareDevices.value = [{ id: device.id, deviceId: device.deviceId, name: device.name }];
    shareVisible.value = true;
};

const openBatchShare = () => {
    if (selectedRowKeys.value.length === 0) {
        Message.warning("请先勾选设备");
        return;
    }
    shareDevices.value = devices.value
        .filter((d) => selectedRowKeys.value.includes(d.id))
        .map((d) => ({ id: d.id, deviceId: d.deviceId, name: d.name }));
    shareVisible.value = true;
};

const handleShareChanged = () => {
    void loadDevices();
};

onMounted(() => {
    void loadDeptTree();
    void loadDevices();
});
</script>

<template>
    <div class="snow-fill">
        <div class="snow-fill-inner uvp-page-shell-flat container uvp-split-list-page">
            <s-fold-page :width="280">
                <template #sider>
                    <div class="left-box uvp-tree-panel">
                        <div class="uvp-tree-panel__head">
                            <span class="uvp-tree-panel__title">组织部门</span>
                        </div>
                        <a-input
                            class="uvp-tree-panel__search"
                            v-model="deptSearchKeyword"
                            placeholder="请输入部门名称"
                            allow-clear
                        >
                            <template #prefix>
                                <icon-search />
                            </template>
                        </a-input>
                        <div class="tree-box uvp-tree-panel__body">
                            <a-tree
                                v-if="deptTree.length"
                                :data="filteredDeptTree"
                                :field-names="{ key: 'id', title: 'name', children: 'children' }"
                                default-expand-all
                                show-line
                                @select="onSelectDept"
                            >
                                <template #icon="{ isLeaf, expanded }">
                                    <component
                                        :is="isLeaf ? Building2 : expanded ? FolderOpen : Folder"
                                        class="uvp-tree-node-icon"
                                        :size="15"
                                        :stroke-width="2"
                                    />
                                </template>
                                <template #switcher-icon>
                                    <ChevronDown class="arco-tree-node-switcher-icon" :size="13" :stroke-width="2.2" />
                                </template>
                                <template #title="node">
                                    <div class="dept-tree-node">
                                        <span class="dept-tree-node__name">{{ node.name }}</span>
                                        <a-link
                                            v-if="canAssign"
                                            class="dept-tree-node__assign"
                                            @click.stop="openDeptAssign(node)"
                                        >
                                            分配本部门设备
                                        </a-link>
                                    </div>
                                </template>
                            </a-tree>
                        </div>
                    </div>
                </template>

                <template #content>
                    <div class="right-box uvp-list-workspace">
                        <s-layout-search class="account-search-panel">
                            <template #fields>
                                <div class="segmented assignment-tabs">
                                    <button
                                        v-for="tab in assignmentTabs"
                                        :key="tab.value"
                                        type="button"
                                        :class="{ active: assignmentFilter === tab.value }"
                                        @click="switchAssignment(tab.value)"
                                    >
                                        {{ tab.label }}
                                    </button>
                                </div>
                                <div class="cmdk">
                                    <Search :size="14" />
                                    <input
                                        v-model="keyword"
                                        type="text"
                                        aria-label="搜索设备"
                                        placeholder="搜索设备 ID / 名称 ..."
                                        @keydown.enter.prevent="onSearch"
                                    />
                                    <button v-if="keyword" class="cmdk-clear" type="button" aria-label="清空搜索" @click="clearKeyword">
                                        <X :size="14" />
                                    </button>
                                </div>
                                <a-select
                                    v-model="statusFilter"
                                    allow-clear
                                    class="toolbar-select status-select"
                                    placeholder="状态"
                                    :style="{ width: '126px' }"
                                    @change="onSearch"
                                >
                                    <a-option value="online">在线</a-option>
                                    <a-option value="offline">离线</a-option>
                                </a-select>
                                <button class="btn-ghost" type="button" @click="onSearch">
                                    <RefreshCcw :size="14" :class="{ spin: rowsLoading }" />
                                    刷新
                                </button>
                            </template>
                        </s-layout-search>

                        <a-table
                            v-model:selected-keys="selectedRowKeys"
                            :data="devices"
                            :loading="rowsLoading"
                            :pagination="tablePagination"
                            row-key="id"
                            :scroll="{ x: 1200, y: '85%' }"
                            :row-selection="canAssign ? { type: 'checkbox', showCheckedAll: true } : undefined"
                            class="uvp-data-table device-data-table"
                            @page-change="onPageChange"
                            @page-size-change="onPageSizeChange"
                        >
                            <template #columns>
                                <a-table-column title="设备编号" :width="200">
                                    <template #cell="{ record }">
                                        <a-tooltip :content="record.deviceId" position="top">
                                            <span class="code-main text-ellipsis">{{ record.deviceId }}</span>
                                        </a-tooltip>
                                    </template>
                                </a-table-column>
                                <a-table-column title="设备名称" :width="180">
                                    <template #cell="{ record }">
                                        <a-tooltip :content="record.name || '-'" position="top">
                                            <span class="text-ellipsis">{{ record.name || "-" }}</span>
                                        </a-tooltip>
                                    </template>
                                </a-table-column>
                                <a-table-column title="状态" :width="100" align="center">
                                    <template #cell="{ record }">
                                        <a-tag :color="record.online ? 'green' : 'gray'" size="small">
                                            {{ record.online ? "在线" : "离线" }}
                                        </a-tag>
                                    </template>
                                </a-table-column>
                                <a-table-column title="归属部门" :width="160">
                                    <template #cell="{ record }">
                                        <span class="text-ellipsis">{{ deptNameText(record) }}</span>
                                    </template>
                                </a-table-column>
                                <a-table-column title="通道数" :width="100" align="center">
                                    <template #cell="{ record }">
                                        <span class="channel-text">{{ record.channelOnlineCount }}/{{ record.channelCount }}</span>
                                    </template>
                                </a-table-column>
                                <a-table-column title="操作" :width="170" align="center">
                                    <template #cell="{ record }">
                                        <a-link
                                            v-if="canAssign"
                                            class="uvp-table-action uvp-table-action--assign"
                                            @click="openRowAssign(record)"
                                        >
                                            调整归属
                                        </a-link>
                                        <a-link
                                            v-if="canShare"
                                            class="uvp-table-action uvp-table-action--permission"
                                            @click="openRowShare(record)"
                                        >
                                            共享管理
                                        </a-link>
                                    </template>
                                </a-table-column>
                            </template>
                        </a-table>

                        <!-- 勾选批量操作条 -->
                        <div v-if="selectedRowKeys.length > 0" class="batch-bar">
                            <span class="batch-bar__count">已选 {{ selectedRowKeys.length }} 台</span>
                            <button v-if="canAssign" class="btn-primary batch-bar__btn" type="button" @click="openBatchAssign">
                                <UserRoundCog :size="14" />
                                批量调整归属
                            </button>
                            <button v-if="canShare" class="btn-ghost batch-bar__btn" type="button" @click="openBatchShare">
                                <Share2 :size="14" />
                                共享管理
                            </button>
                            <a-link class="batch-bar__clear" @click="selectedRowKeys = []">清空选择</a-link>
                        </div>
                    </div>
                </template>
            </s-fold-page>
        </div>

        <!-- 分配弹窗:双栏流向(整部门 / 批量 / 单台共用) -->
        <a-modal
            v-model:visible="assignVisible"
            modal-class="uvp-system-dialog"
            :width="760"
            :title="assignMode === 'dept' ? '分配本部门设备' : '调整设备归属'"
            @cancel="assignVisible = false"
        >
            <div class="assign-flow">
                <!-- 源栏 -->
                <div class="assign-flow__col assign-flow__col--source">
                    <div class="assign-flow__col-head">
                        <span class="assign-flow__step">1</span>
                        <div class="assign-flow__heading">
                            <strong v-if="assignMode === 'dept'">源部门</strong>
                            <strong v-else>源设备</strong>
                            <small>{{ assignMode === "dept" ? "确认待流转范围" : `共 ${assignPendingDevices.length} 台待流转` }}</small>
                        </div>
                    </div>
                    <div v-if="assignMode === 'dept'" class="assign-flow__source-dept">
                        <span class="assign-flow__source-icon-wrap">
                            <Folder :size="22" class="assign-flow__source-icon" />
                        </span>
                        <span class="assign-flow__source-name">{{ assignSourceDeptName }}</span>
                        <span class="assign-flow__source-meta">该部门下的全部设备</span>
                    </div>
                    <ul
                        v-else
                        class="assign-flow__device-list"
                        :class="{ 'assign-flow__device-list--compact': assignPendingDevices.length <= 3 }"
                    >
                        <li v-for="device in assignPendingDevices" :key="device.id" class="assign-flow__device-item">
                            <span class="assign-flow__device-icon-wrap">
                                <Cctv :size="14" class="assign-flow__device-icon" />
                            </span>
                            <span class="text-ellipsis">{{ device.name }}</span>
                        </li>
                    </ul>
                    <div class="assign-flow__summary">{{ assignSummary }}</div>
                </div>

                <!-- 流向箭头 -->
                <div class="assign-flow__arrow">
                    <span class="assign-flow__arrow-icon">
                        <ArrowRight :size="18" :stroke-width="2.2" />
                    </span>
                    <small>流转</small>
                </div>

                <!-- 目标栏 -->
                <div class="assign-flow__col assign-flow__col--target">
                    <div class="assign-flow__col-head">
                        <span class="assign-flow__step">2</span>
                        <div class="assign-flow__heading">
                            <strong>目标部门</strong>
                            <small>从组织树中选择接收部门</small>
                        </div>
                    </div>
                    <div v-if="deptTree.length" class="assign-flow__tree">
                        <a-tree
                            :data="deptTree"
                            :field-names="{ key: 'id', title: 'name', children: 'children' }"
                            :selected-keys="assignTargetDeptId ? [assignTargetDeptId] : []"
                            default-expand-all
                            @select="onSelectTargetDept"
                        >
                            <template #icon="{ isLeaf, expanded }">
                                <component
                                    :is="isLeaf ? Building2 : expanded ? FolderOpen : Folder"
                                    class="uvp-tree-node-icon"
                                    :size="15"
                                    :stroke-width="2"
                                />
                            </template>
                        </a-tree>
                    </div>
                    <div v-else class="assign-flow__empty">
                        <Building2 :size="24" />
                        <strong>暂无可选部门</strong>
                        <span>请稍后刷新部门数据</span>
                    </div>
                    <div class="assign-flow__target-status" :class="{ 'is-selected': assignTargetDeptName }">
                        <span class="assign-flow__status-dot"></span>
                        <span>{{ assignTargetDeptName ? `已选择：${assignTargetDeptName}` : "等待选择目标部门" }}</span>
                    </div>
                </div>
            </div>

            <div class="assign-hint">
                <CircleAlert :size="16" :stroke-width="2" />
                <span>流转后，设备及通道、录像、告警等数据将归入目标部门；原部门立即失去访问，进行中的会话会被撤销。</span>
            </div>

            <template #footer>
                <div class="assign-footer">
                    <button class="btn-ghost" type="button" @click="assignVisible = false">取消</button>
                    <button
                        class="btn-primary"
                        type="button"
                        :disabled="!assignTargetDeptId || assignSubmitting"
                        @click="submitAssign"
                    >
                        {{ confirmAssignText }}
                    </button>
                </div>
            </template>
        </a-modal>

        <!-- 共享管理面板 -->
        <SharePanel v-model:visible="shareVisible" :devices="shareDevices" @changed="handleShareChanged" />
    </div>
</template>

<style scoped lang="less">
.dept-tree-node {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    cursor: pointer;

    &__name {
        font-size: 13px;
    }
    &__assign {
        display: none;
        font-size: 12px;
    }
}

.dept-tree-node:hover .dept-tree-node__assign {
    display: inline;
}

.assignment-tabs {
    margin-right: 4px;
}

.right-box {
    min-height: 0;
    overflow: hidden;
}

.batch-bar {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 12px;
    padding: 10px 16px;
    border-radius: 8px;
    border: 1px solid var(--uvp-panel-border);
    background: var(--uvp-brand-soft);

    &__count {
        font-weight: 600;
        color: var(--uvp-text-secondary);
        font-size: 13px;
    }
    &__btn {
        display: inline-flex;
        align-items: center;
        gap: 6px;
    }
    &__clear {
        margin-left: auto;
    }
}

.assign-flow {
    display: flex;
    align-items: stretch;
    gap: 14px;

    &__col {
        flex: 1;
        min-width: 0;
        display: flex;
        flex-direction: column;
        border: 1px solid var(--uvp-panel-border);
        border-radius: 10px;
        padding: 16px;
        background: var(--uvp-dialog-bg);
    }
    &__col--target {
        border-color: color-mix(in srgb, var(--uvp-brand) 28%, var(--uvp-panel-border));
    }
    &__col-head {
        display: flex;
        align-items: center;
        gap: 10px;
        min-height: 36px;
        margin-bottom: 14px;
    }
    &__step {
        display: inline-grid;
        flex: 0 0 28px;
        width: 28px;
        height: 28px;
        color: var(--uvp-brand-strong);
        background: var(--uvp-brand-soft);
        border-radius: 8px;
        place-items: center;
        font-size: 12px;
        font-weight: 700;
    }
    &__heading {
        display: flex;
        min-width: 0;
        flex-direction: column;
        gap: 2px;

        strong {
            color: var(--uvp-text-primary);
            font-size: 14px;
            font-weight: 650;
            line-height: 20px;
        }

        small {
            color: var(--uvp-text-tertiary);
            font-size: 12px;
            line-height: 16px;
        }
    }
    &__source-dept {
        display: flex;
        flex: 1;
        min-height: 196px;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        padding: 20px;
        border: 1px solid var(--uvp-panel-border);
        border-radius: 8px;
        text-align: center;
    }
    &__source-icon-wrap,
    &__device-icon-wrap {
        display: inline-grid;
        color: var(--uvp-brand-strong);
        background: var(--uvp-brand-soft);
        place-items: center;
    }
    &__source-icon-wrap {
        width: 44px;
        height: 44px;
        margin-bottom: 10px;
        border-radius: 10px;
    }
    &__source-icon {
        flex-shrink: 0;
    }
    &__source-name {
        max-width: 100%;
        color: var(--uvp-text-primary);
        font-size: 14px;
        font-weight: 650;
        line-height: 22px;
    }
    &__source-meta {
        margin-top: 3px;
        color: var(--uvp-text-tertiary);
        font-size: 12px;
        line-height: 18px;
    }
    &__device-list {
        display: flex;
        flex: 1;
        min-height: 196px;
        max-height: 236px;
        flex-direction: column;
        margin: 0;
        padding: 6px;
        list-style: none;
        overflow-y: auto;
        border: 1px solid var(--uvp-panel-border);
        border-radius: 8px;

        &--compact {
            justify-content: center;
        }

        &::-webkit-scrollbar {
            width: 6px;
        }
        &::-webkit-scrollbar-thumb {
            background: var(--uvp-panel-border);
            border-radius: 3px;
        }
    }
    &__device-item {
        display: flex;
        align-items: center;
        gap: 8px;
        min-height: 38px;
        padding: 7px 9px;
        border-radius: 7px;
        font-size: 13px;
        color: var(--uvp-text-primary);
        transition: background 0.15s ease;

        &:hover {
            background: var(--uvp-brand-soft);
        }
    }
    &__device-icon-wrap {
        flex: 0 0 26px;
        width: 26px;
        height: 26px;
        border-radius: 7px;
    }
    &__device-icon {
        flex-shrink: 0;
    }
    &__arrow {
        display: flex;
        flex: 0 0 36px;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        gap: 5px;
        color: var(--uvp-text-tertiary);

        small {
            font-size: 11px;
            line-height: 16px;
        }
    }
    &__arrow-icon {
        display: inline-grid;
        width: 32px;
        height: 32px;
        color: var(--uvp-brand-strong);
        background: var(--uvp-brand-soft);
        border-radius: 8px;
        place-items: center;
    }
    &__tree {
        flex: 1;
        min-height: 196px;
        max-height: 236px;
        overflow-y: auto;
        border: 1px solid var(--uvp-panel-border);
        border-radius: 8px;
        padding: 8px;
        background: var(--uvp-dialog-bg);

        &::-webkit-scrollbar {
            width: 6px;
        }
        &::-webkit-scrollbar-thumb {
            background: var(--uvp-panel-border);
            border-radius: 3px;
        }
    }
    &__empty {
        display: flex;
        flex: 1;
        min-height: 196px;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        gap: 6px;
        color: var(--uvp-text-tertiary);
        border: 1px solid var(--uvp-panel-border);
        border-radius: 8px;

        strong {
            color: var(--uvp-text-secondary);
            font-size: 13px;
        }

        span {
            font-size: 12px;
        }
    }
    &__summary,
    &__target-status {
        min-height: 34px;
        margin-top: 10px;
        padding: 8px 10px;
        color: var(--uvp-text-tertiary);
        border-radius: 8px;
        font-size: 12px;
        line-height: 18px;
    }
    &__summary {
        background: var(--uvp-brand-soft);
        color: var(--uvp-brand-strong);
    }
    &__target-status {
        display: flex;
        align-items: center;
        gap: 7px;
        border: 1px solid var(--uvp-panel-border);

        &.is-selected {
            color: var(--uvp-brand-strong);
            border-color: color-mix(in srgb, var(--uvp-brand) 28%, var(--uvp-panel-border));
            background: var(--uvp-brand-soft);
        }
    }
    &__status-dot {
        flex: 0 0 6px;
        width: 6px;
        height: 6px;
        background: var(--uvp-panel-border);
        border-radius: 50%;
    }
    &__target-status.is-selected &__status-dot {
        background: var(--uvp-brand);
    }
}

.assign-hint {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    margin-top: 14px;
    padding: 10px 12px;
    border: 1px solid color-mix(in srgb, var(--uvp-brand) 18%, var(--uvp-panel-border));
    border-radius: 8px;
    background: var(--uvp-brand-soft);
    color: var(--uvp-brand-strong);
    font-size: 12px;
    line-height: 19px;

    svg {
        flex: 0 0 auto;
        margin-top: 1px;
    }
}

.assign-footer {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
}

.code-main {
    font-family: var(--font-mono, monospace);
}

.channel-text {
    color: var(--uvp-text-secondary);
    font-size: 13px;
}
</style>
