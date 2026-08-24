<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { useDebounceFn } from "@vueuse/core";
import { useRouter } from "vue-router";
import {
    Building2,
    ChevronDown,
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
import {
    applyPermissionWorkbenchAssignments,
    applyPermissionWorkbenchGrants,
    getPermissionWorkbenchSummary,
    listAssignmentDevices,
    queryPermissionWorkbenchGrants,
    resolvePermissionWorkbenchDevices,
    type AssignmentFilter,
    type AssignmentResult,
    type GrantApplyResult,
    type GrantTarget
} from "./api";
import ShareDrawer, { type ShareDeviceBrief } from "./components/ShareDrawer.vue";
import AssignmentDrawer, { type AssignmentDeviceBrief } from "./components/AssignmentDrawer.vue";
import OperationResultDrawer from "./components/OperationResultDrawer.vue";
import { useCrossPageSelection } from "./useCrossPageSelection";

// ---- 权限 ----
const permissions = computed(() => useUserStoreHook().account?.permissions ?? []);
const router = useRouter();
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
const mainView = ref<"assignment" | "sharing">("assignment");
const summary = ref({ allCount: 0, assignedCount: 0, unassignedCount: 0, departments: [] as Array<{ deptId: number; directCount: number; subtreeCount: number }> });

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
    selection.clear();
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
const selection = useCrossPageSelection();
const selectedRowKeys = ref<number[]>([]);
const selectedCount = computed(() => selection.selectedCount.value);

const syncCurrentPageSelection = () => {
    selectedRowKeys.value = selection.currentPageKeys.value;
};

const normalizeSelectedIds = (ids: Array<string | number>) =>
    ids
        .map((id) => devices.value.find((device) => String(device.id) === String(id))?.id)
        .filter((id): id is number => id !== undefined);

const onTableSelect = (rowKeys: Array<string | number>) => {
    const ids = normalizeSelectedIds(rowKeys);
    selection.applyPageSelection(devices.value, ids);
    selectedRowKeys.value = ids;
};

const onTableSelectAll = (checked: boolean) => {
    const pageIds = devices.value.map((device) => device.id);
    const nextIds = checked
        ? [...new Set([...selection.selectedIds.value, ...pageIds])]
        : selection.selectedIds.value.filter((id) => !pageIds.includes(id));
    selection.applyPageSelection(devices.value, nextIds);
    syncCurrentPageSelection();
};

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
            ownerDeptId: selectedDeptId.value
        });
        devices.value = data?.list ?? [];
        selection.setVisiblePage(devices.value);
        syncCurrentPageSelection();
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

const debouncedSearch = useDebounceFn(onSearch, 300);
watch(keyword, () => debouncedSearch());

const loadSummary = async () => {
    try {
        const { data } = await getPermissionWorkbenchSummary();
        if (data) summary.value = data;
    } catch (error: unknown) {
        Message.error(error instanceof Error ? error.message : "加载权限汇总失败");
    }
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

// ---- 分配抽屉(整部门 / 批量 / 单台 三来源共用) ----
const assignVisible = ref(false);
const assignMode = ref<"department" | "devices">("devices");
const assignSourceDeptName = ref("");
const assignSourceDeptId = ref<number | undefined>(undefined);
const assignPendingIds = ref<number[]>([]);
const assignmentDrawerDevices = computed<AssignmentDeviceBrief[]>(() =>
    assignPendingIds.value.map((id) => {
        const device = selection.selectedDevices.value.find((item) => item.id === id) ?? devices.value.find((item) => item.id === id);
        return {
            id,
            deviceId: device?.deviceId ?? `设备-${id}`,
            name: device?.name ?? `设备 #${id}`,
            ownerDeptId: devices.value.find((item) => item.id === id)?.ownerDeptId ?? 0,
            ownerDeptName: deptNameText(devices.value.find((item) => item.id === id) ?? ({ ownerDeptId: 0 } as DeviceVO))
        };
    })
);

const assignSourceSummary = computed(() => summary.value.departments.find((item) => item.deptId === assignSourceDeptId.value));

const openDeptAssign = (node: DivisionItem) => {
    assignMode.value = "department";
    assignSourceDeptName.value = node.name;
    assignSourceDeptId.value = node.id;
    assignVisible.value = true;
};

const openBatchAssign = () => {
    if (selection.selectedCount.value === 0) {
        Message.warning("请先勾选设备");
        return;
    }
    assignMode.value = "devices";
    assignPendingIds.value = [...selection.selectedIds.value];
    assignVisible.value = true;
};

const openRowAssign = (device: DeviceVO) => {
    assignMode.value = "devices";
    assignPendingIds.value = [device.id];
    assignVisible.value = true;
};

// ---- 共享 ----
const shareVisible = ref(false);
const shareDevices = ref<ShareDeviceBrief[]>([]);

const openRowShare = (device: DeviceVO) => {
    shareDevices.value = [{ id: device.id, deviceId: device.deviceId, name: device.name }];
    shareVisible.value = true;
};

const openBatchShare = () => {
    if (selection.selectedCount.value === 0) {
        Message.warning("请先勾选设备");
        return;
    }
    shareDevices.value = selection.selectedDevices.value
        .map((d) => ({ id: d.id, deviceId: d.deviceId, name: d.name }));
    shareVisible.value = true;
};

type OperationResult = AssignmentResult | GrantApplyResult;
const resultVisible = ref(false);
const resultKind = ref<"assignment" | "grant">("assignment");
const operationResult = ref<OperationResult | null>(null);
const retryOperation = ref<(() => Promise<void>) | null>(null);

const applyOperationSelectionResult = (result: OperationResult) => {
    selection.applyOperationResult({
        changed: result.results.filter((item) => item.status === "changed").map((item) => item.deviceId),
        skipped: result.results.filter((item) => item.status === "skipped").map((item) => item.deviceId),
        failed: result.results.filter((item) => item.status === "failed").map((item) => item.deviceId)
    });
};

const handleShareSubmitted = (result: GrantApplyResult, context: { mode: "add" | "remove"; targets: GrantTarget[] }) => {
    operationResult.value = result;
    resultKind.value = "grant";
    resultVisible.value = true;
    applyOperationSelectionResult(result);
    syncCurrentPageSelection();
    retryOperation.value = async () => {
        const failedIds = result.results.filter((item) => item.status === "failed").map((item) => item.deviceId);
        if (!failedIds.length) return;
        const latest = await queryPermissionWorkbenchGrants(failedIds);
        const revisions = new Map((latest.data?.devices ?? []).map((device) => [device.deviceId, device.revision]));
        const retried = await applyPermissionWorkbenchGrants({
            items: failedIds.map((deviceId) => ({ deviceId, expectedRevision: revisions.get(deviceId) ?? "" })),
            mode: context.mode,
            targets: context.targets
        });
        if (retried.data) {
            operationResult.value = retried.data;
            applyOperationSelectionResult(retried.data);
        }
    };
    void loadSummary();
    void loadDevices();
};

const handleAssignmentSubmitted = (result: AssignmentResult, targetDeptId: number) => {
    operationResult.value = result;
    resultKind.value = "assignment";
    resultVisible.value = true;
    retryOperation.value = async () => {
        const failedIds = result.results.filter((item) => item.status === "failed").map((item) => item.deviceId);
        if (!failedIds.length) return;
        const latest = await resolvePermissionWorkbenchDevices(failedIds);
        const items = (latest.data?.devices ?? []).map((device) => ({ deviceId: device.id, expectedOwnerDeptId: device.ownerDeptId }));
        const retried = await applyPermissionWorkbenchAssignments({ items, targetDeptId });
        if (retried.data) {
            operationResult.value = retried.data;
            applyOperationSelectionResult(retried.data);
        }
    };
    applyOperationSelectionResult(result);
    syncCurrentPageSelection();
    void loadSummary();
    void loadDevices();
};

const retryFailedOperation = async () => {
    if (!retryOperation.value) return;
    try {
        await retryOperation.value();
        Message.success("失败项已重新提交");
        void loadSummary();
        void loadDevices();
    } catch (error: unknown) {
        Message.error(error instanceof Error ? error.message : "重试失败项失败");
    }
};

const openOperationLogs = () => {
    void router.push({ path: "/system/log", query: { module: "GB28181设备管理", path: "permission-workbench" } });
};

onMounted(() => {
    void loadDeptTree();
    void loadSummary();
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
                         <div class="workbench-head">
                             <div>
                                 <h2 class="workbench-head__title">设备权限工作台</h2>
                                 <p class="workbench-head__sub">统一管理设备归属与共享授权</p>
                             </div>
                             <div class="segmented workbench-views" role="tablist" aria-label="权限视图">
                                 <button type="button" :class="{ active: mainView === 'assignment' }" @click="mainView = 'assignment'">设备归属</button>
                                 <button type="button" :class="{ active: mainView === 'sharing' }" @click="mainView = 'sharing'">共享授权</button>
                             </div>
                         </div>
                         <div class="summary-strip" aria-label="设备权限汇总">
                             <div class="summary-strip__item"><span>全部设备</span><strong>{{ summary.allCount }}</strong></div>
                             <div class="summary-strip__item"><span>已分配</span><strong>{{ summary.assignedCount }}</strong></div>
                             <div class="summary-strip__item"><span>未分配</span><strong>{{ summary.unassignedCount }}</strong></div>
                             <div class="summary-strip__item"><span>部门数</span><strong>{{ summary.departments.length }}</strong></div>
                         </div>
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
                             v-show="mainView === 'assignment'"
                            v-model:selected-keys="selectedRowKeys"
                            :data="devices"
                            :loading="rowsLoading"
                            :pagination="tablePagination"
                            row-key="id"
                            :scroll="{ x: 1200, y: '85%' }"
                            :row-selection="canAssign || canShare ? { type: 'checkbox', showCheckedAll: true } : undefined"
                            class="uvp-data-table device-data-table"
                            @select="onTableSelect"
                            @select-all="onTableSelectAll"
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
                                 <a-table-column title="操作" :width="170" align="center" cell-class="operation-column">
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

                         <div v-if="mainView === 'sharing'" class="sharing-view-empty">
                             <Share2 :size="24" />
                             <strong>共享授权视图</strong>
                             <span>请在设备行操作中打开共享管理，批量共享将在此视图统一收口。</span>
                         </div>

                        <!-- 勾选批量操作条 -->
                        <div v-if="selectedCount > 0" class="batch-bar">
                            <span class="batch-bar__count">已选 {{ selectedCount }} 台</span>
                            <button v-if="canAssign" class="btn-primary batch-bar__btn" type="button" @click="openBatchAssign">
                                <UserRoundCog :size="14" />
                                批量调整归属
                            </button>
                            <button v-if="canShare" class="btn-ghost batch-bar__btn" type="button" @click="openBatchShare">
                                <Share2 :size="14" />
                                共享管理
                            </button>
                             <a-link class="batch-bar__clear" @click="selection.clear(); selectedRowKeys = []">清空选择</a-link>
                        </div>
                    </div>
                </template>
            </s-fold-page>
        </div>

        <AssignmentDrawer
            v-model:visible="assignVisible"
            :mode="assignMode"
            :devices="assignmentDrawerDevices"
            :source-dept-id="assignSourceDeptId"
            :source-dept-name="assignSourceDeptName"
            :source-direct-count="assignMode === 'department' ? assignSourceSummary?.directCount : undefined"
            :source-subtree-count="assignMode === 'department' ? assignSourceSummary?.subtreeCount : undefined"
            :departments="deptTree"
            @submitted="handleAssignmentSubmitted"
        />

        <!-- 共享管理抽屉 -->
        <ShareDrawer v-model:visible="shareVisible" :devices="shareDevices" @submitted="handleShareSubmitted" />
        <OperationResultDrawer
            v-model:visible="resultVisible"
            :kind="resultKind"
            :result="operationResult"
            @retry="retryFailedOperation"
            @view-log="openOperationLogs"
        />
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

 .workbench-head {
     display: flex;
     align-items: flex-start;
     justify-content: space-between;
     gap: 16px;
     margin-bottom: 14px;

     &__title {
         margin: 0;
         color: var(--uvp-text-primary);
         font-size: 20px;
         line-height: 28px;
     }

     &__sub {
         margin: 3px 0 0;
         color: var(--uvp-text-tertiary);
         font-size: 12px;
     }
 }

 .workbench-views {
     flex: 0 0 auto;
 }

 .summary-strip {
     display: grid;
     grid-template-columns: repeat(4, minmax(0, 1fr));
     gap: 10px;
     margin-bottom: 14px;

     &__item {
         display: flex;
         min-height: 58px;
         flex-direction: column;
         justify-content: center;
         gap: 2px;
         padding: 9px 14px;
         border: 1px solid var(--uvp-panel-border);
         border-radius: 8px;
         background: var(--uvp-dialog-bg);

         span {
             color: var(--uvp-text-tertiary);
             font-size: 12px;
         }

         strong {
             color: var(--uvp-text-primary);
             font-size: 20px;
             line-height: 24px;
         }
     }
 }

 .sharing-view-empty {
     display: flex;
     min-height: 280px;
     flex-direction: column;
     align-items: center;
     justify-content: center;
     gap: 8px;
     color: var(--uvp-text-tertiary);
     border: 1px dashed var(--uvp-panel-border);
     border-radius: 8px;

     strong {
         color: var(--uvp-text-secondary);
         font-size: 14px;
     }

     span {
         max-width: 420px;
         text-align: center;
         font-size: 12px;
         line-height: 18px;
     }
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
