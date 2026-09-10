<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { CircleStop, Download, Eraser, FileText, RotateCcw, Search, Trash2 } from "lucide-vue-next";
import {
    batchDeleteWorkOrders,
    deleteWorkOrder,
    getWorkOrder,
    listWorkOrders,
    stopWorkOrder,
    workOrderDownloadUrl,
    workOrderFileUrl,
    type WorkOrderDetail,
    type WorkOrderSnapshot
} from "@/api/gb28181-work-recording";
import { useUserStoreHook } from "@/store/modules/user";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import WorkOrderChannelsDialog from "./WorkOrderChannelsDialog.vue";
import {
    formatWorkOrderDuration,
    formatWorkOrderSize,
    formatWorkOrderTime,
    isWorkOrderActive,
    workOrderCameraNames,
    workOrderFormSections,
    workOrderSliceCount,
    workOrderStateColor,
    workOrderStateLabel,
    workOrderTotalBytes
} from "./orderState";

const userStore = useUserStoreHook();
// 操作列的右固定与其它列表页一致：窄屏取消固定，避免用户横向滚动时被遮住。
const { isMobile } = useDevicesSize();
const permissions = computed(() => userStore.account.permissions ?? []);
const hasPermission = (permission: string) => permissions.value.includes("*:*:*") || permissions.value.includes(permission);
const canView = computed(() => hasPermission("gb28181:work-order:view"));
const canStop = computed(() => hasPermission("gb28181:work-order:stop"));
const canDelete = computed(() => hasPermission("gb28181:work-order:delete"));

const stateOptions = [
    { value: "recording", label: "录像中" },
    { value: "starting", label: "开始中" },
    { value: "stopping", label: "结束中" },
    { value: "stopped", label: "已结束" },
    { value: "failed", label: "失败" },
    { value: "unknown", label: "待核实" }
];

const query = reactive<{ keyword: string; state?: string; range: string[] }>({ keyword: "", state: undefined, range: [] });
const applied = reactive<{ keyword: string; state?: string; startTime?: string; endTime?: string }>({ keyword: "" });

const rows = ref<WorkOrderSnapshot[]>([]);
const loading = ref(false);
const errorMessage = ref("");
const page = reactive({ current: 1, pageSize: 10, total: 0 });
const pagination = computed(() => ({ current: page.current, pageSize: page.pageSize, total: page.total, showTotal: true, showPageSize: true }));

const detailVisible = ref(false);
const detailLoading = ref(false);
const detailError = ref("");
const detail = ref<WorkOrderDetail | null>(null);
const stopping = ref("");
const deleting = ref(false);
const selectedIds = ref<string[]>([]);

/** 通道明细弹窗：列表的「通道数」与详情弹窗都从这里打开。 */
const channelsVisible = ref(false);
const channelsTarget = ref<WorkOrderSnapshot | null>(null);

/** 进行中的作业单不能删，勾选与按钮都要据此提示，避免点下去才发现白点。 */
const hasActiveSelection = computed(() => rows.value.some(row => selectedIds.value.includes(row.id) && isWorkOrderActive(row.state)));

const detailSections = computed(() => {
    const form = detail.value?.form?.form;
    return workOrderFormSections.map(section => ({
        title: section.title,
        rows: section.fields.map(([key, label]) => ({
            label,
            value: key === "workPersonnel" ? (form?.workPersonnel || []).join("、") || "—" : String(form?.[key] ?? "") || "—"
        }))
    }));
});

async function load(nextPage = page.current) {
    if (!canView.value) return;
    loading.value = true;
    errorMessage.value = "";
    try {
        const response = await listWorkOrders({
            page: nextPage,
            pageSize: page.pageSize,
            state: applied.state,
            keyword: applied.keyword || undefined,
            startTime: applied.startTime,
            endTime: applied.endTime
        });
        if (response.code !== 0) throw new Error(response.message || "作业单查询失败");
        rows.value = response.data.items || [];
        // 分页/筛选之后旧勾选可能已不在当前数据里，保留会让批量删除删到看不见的行。
        selectedIds.value = selectedIds.value.filter(id => rows.value.some(row => row.id === id));
        page.current = response.data.page ?? nextPage;
        page.pageSize = response.data.pageSize ?? page.pageSize;
        page.total = response.data.total ?? 0;
    } catch (reason: any) {
        errorMessage.value = reason?.response?.data?.message || reason?.message || "作业单查询失败";
    } finally {
        loading.value = false;
    }
}

function search() {
    applied.keyword = query.keyword.trim();
    applied.state = query.state;
    applied.startTime = query.range?.[0] || undefined;
    applied.endTime = query.range?.[1] || undefined;
    page.current = 1;
    void load(1);
}

function reset() {
    query.keyword = "";
    query.state = undefined;
    query.range = [];
    search();
}

function handlePageChange(current: number) {
    void load(current);
}

function handlePageSizeChange(size: number) {
    page.pageSize = size;
    void load(1);
}

async function openDetail(order: WorkOrderSnapshot) {
    detailVisible.value = true;
    detailLoading.value = true;
    detailError.value = "";
    detail.value = null;
    try {
        const response = await getWorkOrder(order.id);
        if (response.code !== 0) throw new Error(response.message || "作业单详情读取失败");
        detail.value = response.data;
    } catch (reason: any) {
        detailError.value = reason?.response?.data?.message || reason?.message || "作业单详情读取失败";
    } finally {
        detailLoading.value = false;
    }
}

async function stop(order: WorkOrderSnapshot) {
    if (!canStop.value || stopping.value) return;
    stopping.value = order.id;
    try {
        const response = await stopWorkOrder(order.id);
        if (response.code !== 0) throw new Error(response.message || "结束录像失败");
        Message.success("已结束录像");
        await load();
        if (detailVisible.value && detail.value?.snapshot.id === order.id) detail.value = { ...detail.value, snapshot: response.data };
    } catch (reason: any) {
        Message.error(reason?.response?.data?.message || reason?.message || "结束录像失败，请刷新核实");
        await load();
    } finally {
        stopping.value = "";
    }
}

function download(order: WorkOrderSnapshot) {
    window.open(workOrderDownloadUrl(order.id), "_blank", "noopener");
}

function openChannels(order: WorkOrderSnapshot) {
    channelsTarget.value = order;
    channelsVisible.value = true;
}

/** 删除结果文案统一在一处：跳过的条目必须说清楚为什么没删掉。 */
function describeDelete(result: { deleted: number; skipped?: string[]; missing?: string[] }) {
    const parts = [`已删除 ${result.deleted} 张作业单`];
    if (result.skipped?.length) parts.push(`跳过 ${result.skipped.length} 张（正在录制，请先结束录像）`);
    if (result.missing?.length) parts.push(`${result.missing.length} 张已不存在`);
    return parts.join("，");
}

async function removeOne(order: WorkOrderSnapshot) {
    if (!canDelete.value || deleting.value) return;
    Modal.confirm({
        title: "删除作业单",
        content: `确认删除作业单 ${order.id.slice(0, 8)}（${order.projectName || "未填写项目名称"}）？删除后无法在列表中找回。`,
        okText: "删除",
        cancelText: "取消",
        okButtonProps: { status: "danger" },
        onOk: async () => {
            deleting.value = true;
            try {
                const response = await deleteWorkOrder(order.id);
                if (response.code !== 0) throw new Error(response.message || "删除失败");
                Message.success(describeDelete(response.data));
                await load();
            } catch (reason: any) {
                Message.error(reason?.response?.data?.message || reason?.message || "删除失败，请刷新核实");
                await load();
            } finally {
                deleting.value = false;
            }
        }
    });
}

async function removeSelected() {
    if (!canDelete.value || deleting.value || !selectedIds.value.length) return;
    const ids = [...selectedIds.value];
    Modal.confirm({
        title: "批量删除作业单",
        content: hasActiveSelection.value
            ? `已勾选 ${ids.length} 张作业单，其中包含正在录制的条目，这些会被跳过。确认继续？`
            : `确认删除已勾选的 ${ids.length} 张作业单？删除后无法在列表中找回。`,
        okText: "删除",
        cancelText: "取消",
        okButtonProps: { status: "danger" },
        onOk: async () => {
            deleting.value = true;
            try {
                const response = await batchDeleteWorkOrders(ids);
                if (response.code !== 0) throw new Error(response.message || "批量删除失败");
                Message.success(describeDelete(response.data));
                selectedIds.value = [];
                await load();
            } catch (reason: any) {
                Message.error(reason?.response?.data?.message || reason?.message || "批量删除失败，请刷新核实");
                await load();
            } finally {
                deleting.value = false;
            }
        }
    });
}

onMounted(() => {
    void load(1);
});
</script>

<template>
    <div class="snow-fill work-orders-page">
        <div class="snow-fill-inner uvp-page-shell-flat work-orders-shell">
            <a-alert v-if="!canView" class="work-orders-state" type="warning">无权查看作业单，请联系管理员分配作业单查看权限。</a-alert>

            <template v-else>
                <s-layout-search>
                    <template #fields>
                        <a-input v-model="query.keyword" data-testid="keyword" placeholder="作业单号或项目名称" allow-clear style="width: 260px" @press-enter="search" />
                        <a-select v-model="query.state" data-testid="state" placeholder="作业单状态" allow-clear style="width: 140px">
                            <a-option v-for="option in stateOptions" :key="option.value" :value="option.value">{{ option.label }}</a-option>
                        </a-select>
                        <a-range-picker v-model="query.range" data-testid="range" show-time value-format="YYYY-MM-DDTHH:mm:ssZ" allow-clear style="width: 340px" />
                    </template>
                    <template #actions>
                        <a-button v-if="canDelete" status="danger" data-testid="batch-delete" :disabled="!selectedIds.length || deleting" @click="removeSelected">
                            <template #icon><Trash2 :size="15" /></template>
                            批量删除{{ selectedIds.length ? `（${selectedIds.length}）` : "" }}
                        </a-button>
                        <a-button type="primary" data-testid="search" @click="search">
                            <template #icon><Search :size="15" /></template>
                            查询
                        </a-button>
                        <a-button data-testid="reset" @click="reset">
                            <template #icon><RotateCcw :size="15" /></template>
                            重置
                        </a-button>
                        <a-button data-testid="refresh" :loading="loading" @click="load()">
                            <template #icon><Eraser :size="15" /></template>
                            刷新
                        </a-button>
                    </template>
                </s-layout-search>

                <a-alert v-if="errorMessage && !loading" class="work-orders-state" type="error" closable @close="errorMessage = ''">
                    {{ errorMessage }}
                </a-alert>

                <div v-else class="work-orders-table-wrap">
                    <a-table
                        class="uvp-data-table"
                        data-testid="work-order-table"
                        row-key="id"
                        v-model:selected-keys="selectedIds"
                        :data="rows"
                        :bordered="false"
                        :loading="loading"
                        :pagination="pagination"
                        :row-selection="canDelete ? { type: 'checkbox', showCheckedAll: true } : undefined"
                        @page-change="handlePageChange"
                        @page-size-change="handlePageSizeChange"
                    >
                        <template #columns>
                            <a-table-column title="作业单号" :width="150">
                                <template #cell="{ record }"><span class="work-order-serial">{{ record.id.slice(0, 8) }}</span></template>
                            </a-table-column>
                            <a-table-column title="项目名称" :width="200" :ellipsis="true" :tooltip="true">
                                <template #cell="{ record }">{{ record.projectName || "—" }}</template>
                            </a-table-column>
                            <a-table-column title="录制通道" :width="240" :ellipsis="true" :tooltip="true">
                                <template #cell="{ record }">{{ workOrderCameraNames(record) || "—" }}</template>
                            </a-table-column>
                            <a-table-column title="通道数" :width="86" align="center">
                                <template #cell="{ record }">
                                    <a-link
                                        class="uvp-table-action uvp-table-action--detail"
                                        data-testid="channel-count"
                                        :disabled="!record.cameras?.length"
                                        :title="record.cameras?.length ? '查看录制通道明细' : '没有关联的录制通道'"
                                        @click="openChannels(record)"
                                    >
                                        {{ record.cameras?.length || 0 }}
                                    </a-link>
                                </template>
                            </a-table-column>
                            <a-table-column title="开始时间" :width="176">
                                <template #cell="{ record }">{{ formatWorkOrderTime(record.cameras?.[0]?.startedAt) }}</template>
                            </a-table-column>
                            <a-table-column title="结束时间" :width="176">
                                <template #cell="{ record }">{{ formatWorkOrderTime(record.cameras?.[0]?.stoppedAt) }}</template>
                            </a-table-column>
                            <a-table-column title="分片" :width="80">
                                <template #cell="{ record }">{{ workOrderSliceCount(record) }}</template>
                            </a-table-column>
                            <a-table-column title="大小" :width="110">
                                <template #cell="{ record }">{{ formatWorkOrderSize(workOrderTotalBytes(record)) }}</template>
                            </a-table-column>
                            <a-table-column title="状态" :width="100">
                                <template #cell="{ record }"><a-tag :color="workOrderStateColor(record.state)">{{ workOrderStateLabel(record.state) }}</a-tag></template>
                            </a-table-column>
                            <a-table-column title="操作" :width="250" align="center" :fixed="isMobile ? '' : 'right'">
                                <template #cell="{ record }">
                                    <div class="uvp-table-actions">
                                        <a-link class="uvp-table-action uvp-table-action--detail" data-testid="detail" @click="openDetail(record)">
                                            <template #icon><FileText :size="13" /></template>
                                            <span>详情</span>
                                        </a-link>
                                        <a-link
                                            class="uvp-table-action uvp-table-action--download"
                                            data-testid="download"
                                            :disabled="!workOrderSliceCount(record)"
                                            @click="download(record)"
                                        >
                                            <template #icon><Download :size="13" /></template>
                                            <span>下载</span>
                                        </a-link>
                                        <a-link
                                            v-if="canStop && isWorkOrderActive(record.state)"
                                            class="uvp-table-action uvp-table-action--stop"
                                            data-testid="stop"
                                            :loading="stopping === record.id"
                                            :disabled="Boolean(stopping)"
                                            @click="stop(record)"
                                        >
                                            <template #icon><CircleStop :size="13" /></template>
                                            <span>结束录像</span>
                                        </a-link>
                                        <a-link
                                            v-else-if="canDelete"
                                            class="uvp-table-action uvp-table-action--delete"
                                            data-testid="delete"
                                            :title="isWorkOrderActive(record.state) ? '正在录制的作业单不能删除，请先结束录像' : '删除作业单'"
                                            :disabled="isWorkOrderActive(record.state) || deleting"
                                            @click="removeOne(record)"
                                        >
                                            <template #icon><Trash2 :size="13" /></template>
                                            <span>删除</span>
                                        </a-link>
                                    </div>
                                </template>
                            </a-table-column>
                        </template>
                        <template #empty>
                            <a-empty description="暂无作业单" />
                        </template>
                    </a-table>
                </div>
            </template>
        </div>

        <a-modal v-model:visible="detailVisible" modal-class="uvp-system-dialog" title="作业单详情" :width="960" :footer="false">
            <a-spin :loading="detailLoading" style="width: 100%">
                <p v-if="detailError" role="alert" class="work-order-detail-error">{{ detailError }}</p>
                <template v-if="detail">
                    <div class="work-order-detail-head">
                        <div>
                            <strong>{{ detail.snapshot.projectName || "未填写项目名称" }}</strong>
                            <small>{{ detail.snapshot.id }}</small>
                        </div>
                        <a-tag :color="workOrderStateColor(detail.snapshot.state)">{{ workOrderStateLabel(detail.snapshot.state) }}</a-tag>
                        <a-button data-testid="detail-channels" :disabled="!detail.snapshot.cameras?.length" @click="openChannels(detail.snapshot)">通道明细</a-button>
                        <a-button :disabled="!workOrderSliceCount(detail.snapshot)" @click="download(detail.snapshot)">下载整单 ZIP</a-button>
                        <a-button
                            v-if="canStop && isWorkOrderActive(detail.snapshot.state)"
                            status="danger"
                            :loading="stopping === detail.snapshot.id"
                            :disabled="Boolean(stopping)"
                            @click="stop(detail.snapshot)"
                        >
                            结束录像
                        </a-button>
                    </div>

                    <section v-for="section in detailSections" :key="section.title" class="work-order-fields">
                        <h4>{{ section.title }}</h4>
                        <dl>
                            <div v-for="row in section.rows" :key="row.label"><dt>{{ row.label }}</dt><dd>{{ row.value }}</dd></div>
                        </dl>
                    </section>

                    <section v-for="camera in detail.snapshot.cameras || []" :key="camera.channelId" class="work-order-camera">
                        <header>
                            <strong>{{ camera.channelName || `通道 ${camera.channelId}` }}</strong>
                            <span>
                                <a-tag :color="workOrderStateColor(camera.state)">{{ workOrderStateLabel(camera.state) }}</a-tag>
                                {{ camera.files?.length || 0 }} 个分片 · {{ formatWorkOrderSize((camera.files || []).reduce((sum, file) => sum + (file.fileSize || 0), 0)) }}
                            </span>
                        </header>
                        <div v-for="file in camera.files || []" :key="file.id" class="work-order-file">
                            <span class="work-order-file-name">{{ file.fileName }}</span>
                            <small>{{ formatWorkOrderTime(file.startTime) }}</small>
                            <small>{{ formatWorkOrderDuration(file.timeLen) }}</small>
                            <small>{{ formatWorkOrderSize(file.fileSize) }}</small>
                            <a :href="workOrderFileUrl(detail.snapshot.id, file.id)" target="_blank" rel="noopener">在线播放</a>
                        </div>
                        <p v-if="!(camera.files || []).length" class="work-order-empty-file">暂无已归档分片</p>
                    </section>
                </template>
            </a-spin>
        </a-modal>

        <WorkOrderChannelsDialog
            :visible="channelsVisible"
            :order-id="channelsTarget?.id"
            :project-name="channelsTarget?.projectName"
            :cameras="channelsTarget?.cameras || []"
            @close="channelsVisible = false"
        />
    </div>
</template>

<style scoped>
.work-orders-state { margin-bottom: 12px; }
.work-orders-table-wrap { min-width: 0; }
.work-order-serial { font-family: var(--font-mono, monospace); }
/* 「结束录像」沿用「云端录像」页停止动作的橙色，全站录像类操作保持同一语义色。 */
:deep(.uvp-data-table .uvp-table-action--stop) { color: #d97706; }
:deep(.uvp-data-table .uvp-table-action--stop:hover) { color: #b45309; background: rgb(217 119 6 / 8%); }
.work-order-detail-error { color: rgb(var(--danger-6)); }
.work-order-detail-head { display: flex; align-items: center; gap: 14px; padding-bottom: 14px; border-bottom: 1px solid var(--color-border-2); }
.work-order-detail-head > div { flex: 1; min-width: 0; }
.work-order-detail-head strong { display: block; font-size: 15px; font-weight: 500; }
.work-order-detail-head small { color: var(--color-text-3); }
.work-order-fields h4 { margin: 18px 0 10px; font-size: 13px; font-weight: 500; color: var(--color-text-2); }
.work-order-fields dl { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px 18px; margin: 0; }
.work-order-fields dt { color: var(--color-text-3); font-size: 12px; }
.work-order-fields dd { margin: 2px 0 0; font-size: 13px; word-break: break-all; }
.work-order-camera { margin-top: 18px; border-top: 1px solid var(--color-border-2); padding-top: 12px; }
.work-order-camera header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
.work-order-camera header span { color: var(--color-text-3); font-size: 12px; }
.work-order-file { display: flex; align-items: center; gap: 14px; padding: 7px 0; font-size: 13px; }
.work-order-file-name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.work-order-file small { color: var(--color-text-3); }
.work-order-file a { color: rgb(var(--primary-6)); }
.work-order-empty-file { color: var(--color-text-3); font-size: 12px; }
@media (max-width: 720px) { .work-order-fields dl { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
