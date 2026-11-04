<template>
  <div class="snow-page sysjobresults-page">
    <div class="snow-inner uvp-page-shell-flat">
      <s-layout-search>
        <template #fields>
          <a-input
            v-model="searchForm.jobId"
            placeholder="请输入任务ID"
            style="width: 176px"
            allow-clear
            @press-enter="handleSearch"
          />
          <a-select v-model="searchForm.status" placeholder="请选择执行状态" style="width: 136px" allow-clear>
            <a-option value="SUCCESS">SUCCESS</a-option>
            <a-option value="FAILED">FAILED</a-option>
            <a-option value="PANIC">PANIC</a-option>
          </a-select>
          <a-range-picker
            v-model="searchForm.startTimeRange"
            show-time
            format="YYYY-MM-DD HH:mm:ss"
            style="width: 360px"
            :placeholder="['开始时间', '结束时间']"
          />
        </template>
        <template #actions>
          <a-button type="primary" @click="handleSearch">
            <template #icon><icon-search /></template>
            <span>查询</span>
          </a-button>
          <a-button @click="handleReset">
            <template #icon><icon-refresh /></template>
            <span>重置</span>
          </a-button>
          <!--
            一键清理：工具栏上的**危险**操作 ⇒ 按按钮真源（styles/uvp-buttons.scss 语义表）
            用 status="danger" 软红；⛔ 不能加 type="primary" —— 一个操作区最多 1 个 primary，
            查询已经占了，且 primary 会把它变成实心蓝/橙（旧告警页是建真源之前的写法）。
          -->
          <a-button
            data-testid="sysjobresults-clear-all"
            status="danger"
            :loading="clearing"
            :disabled="clearing"
            v-hasPerm="['system:sysjobresults:clear']"
            @click="requestClearAll"
          >
            <template #icon><Eraser :size="15" /></template>
            <span>一键清理</span>
          </a-button>
        </template>
      </s-layout-search>

      <a-table
        class="uvp-data-table"
        :data="dataList"
        :loading="loading"
        :pagination="paginationConfig"
        :bordered="false"
        :scroll="{ x: '120%' }"
        @page-change="handlePageChange"
        @page-size-change="handlePageSizeChange"
      >
        <template #columns>
          <a-table-column title="任务ID" data-index="jobId" :width="110" ellipsis tooltip />
          <a-table-column title="执行状态" data-index="status" :width="150" ellipsis tooltip />
          <a-table-column title="错误信息" data-index="error" :width="150" ellipsis tooltip />
          <a-table-column title="执行摘要" data-index="summary" :width="260" ellipsis tooltip>
            <template #cell="{ record }">
              {{ record.summary?.trim() || "—" }}
            </template>
          </a-table-column>
          <a-table-column title="开始时间" data-index="startTime" :width="150" ellipsis tooltip>
            <template #cell="{ record }">
              {{ record["startTime"] ? formatTime(record["startTime"]) : "" }}
            </template>
          </a-table-column>
          <a-table-column title="结束时间" data-index="endTime" :width="150" ellipsis tooltip>
            <template #cell="{ record }">
              {{ record["endTime"] ? formatTime(record["endTime"]) : "" }}
            </template>
          </a-table-column>
          <a-table-column title="执行时长(秒)" data-index="duration" :width="150" ellipsis tooltip>
            <template #cell="{ record }">
              {{ record["duration"] ? (record["duration"] / 1000000000).toFixed(4) : "" }}
            </template>
          </a-table-column>
          <a-table-column title="重试次数" data-index="retryCount" :width="150" ellipsis tooltip />
          <a-table-column title="操作" :width="96" align="center" :fixed="isMobile ? '' : 'right'">
            <template #cell="{ record }">
              <div class="uvp-table-actions">
                <a-link
                  class="uvp-table-action uvp-table-action--delete"
                  v-hasPerm="['system:sysjobresults:delete']"
                  @click="requestDelete(record)"
                >
                  <template #icon><icon-delete /></template>
                  <span>删除</span>
                </a-link>
              </div>
            </template>
          </a-table-column>
        </template>
      </a-table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, computed, ref, onMounted, watch } from "vue";
import { useRoute } from "vue-router";
import { Modal, Message } from "@arco-design/web-vue";
import { Eraser } from "lucide-vue-next";
import { useSysJobResultsPluginHook } from "@/hooks/useSysJobResults";
import { formatTime } from "@/globals";
import { useDevicesSize } from "@/hooks/useDevicesSize";

const route = useRoute();
const { isMobile } = useDevicesSize();
const { dataList, loading, total, currentPage, pageSize, fetchDataList, deleteData, clearData, resetSearchParams } =
  useSysJobResultsPluginHook();

const clearing = ref(false);

// 搜索表单
const searchForm = reactive({
  jobId: "",
  status: "",
  startTimeRange: []
});

// 分页配置
const paginationConfig = computed(() => ({
  total: total.value,
  current: currentPage.value,
  pageSize: pageSize.value,
  showTotal: true,
  showJumper: true,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50, 100]
}));

// 获取数据列表
const loadData = async (pageNum: number = currentPage.value, pageSizeVal: number = pageSize.value) => {
  const params: any = {
    pageNum,
    pageSize: pageSizeVal
  };
  if (searchForm.jobId) {
    params.jobId = searchForm.jobId;
  }
  if (searchForm.status) {
    params.status = searchForm.status;
  }
  if (searchForm.startTimeRange && searchForm.startTimeRange.length === 2) {
    params.startTimeStart = searchForm.startTimeRange[0];
    params.startTimeEnd = searchForm.startTimeRange[1];
  }
  await fetchDataList(params);
};

// 处理分页变化
const handlePageChange = (page: number) => {
  loadData(page, pageSize.value);
};

// 处理页面大小变化
const handlePageSizeChange = (size: number) => {
  loadData(1, size); // 页码重置为1
};

// 搜索处理
const handleSearch = () => {
  loadData(1); // 搜索时重置到第一页
};

// 重置搜索
const handleReset = () => {
  searchForm.jobId = "";
  searchForm.status = "";
  searchForm.startTimeRange = [];
  resetSearchParams();
  loadData(1);
};

/**
 * 单条删除确认弹窗。
 * ⛔ 用 Modal.warning（不是 Modal.confirm）：本仓删除类操作统一带橙色警示图标 +
 *   danger 主按钮，与设备列表 / 固件仓库的删除确认「逐字对齐」；confirm 没有图标。
 * ⛔ 不再用 a-popconfirm 气泡：气泡样式贴单元格、误触即执行，日志页误删代价高。
 */
const requestDelete = (record: { id: number }) => {
  Modal.warning({
    title: "删除日志记录",
    content: "将物理删除这条定时任务日志，删除后不可恢复。",
    okText: "删除",
    cancelText: "取消",
    hideCancel: false,
    escToClose: true,
    okButtonProps: { status: "danger" },
    onOk: () => handleDelete(record.id)
  });
};

// 删除数据
const handleDelete = async (id: number) => {
  try {
    await deleteData(id);
    // 重新加载当前页数据（删完末页可能变空，按当前页重拉，列表自己会回退）
    await loadData();
    Message.success("删除成功");
  } catch (error: any) {
    Message.error(error?.message || "删除失败");
  }
};

/**
 * 一键清理（清空全部日志）。
 * ⛔ 与单条删除共用一套弹窗形态，但文案要说清「全部」——这个按钮不受页面筛选条件影响。
 * 成功后回到第一页重拉，避免停在已清空的末页。
 */
const requestClearAll = () => {
  if (clearing.value) return;
  Modal.warning({
    title: "一键清理定时任务日志",
    content: "将清空全部定时任务执行日志（不受当前筛选条件影响），删除后不可恢复。",
    okText: "清空",
    cancelText: "取消",
    hideCancel: false,
    escToClose: true,
    okButtonProps: { status: "danger" },
    onOk: () => performClearAll()
  });
};

const performClearAll = async () => {
  if (clearing.value) return;
  clearing.value = true;
  try {
    const deletedCount = await clearData();
    Message.success(`已清空 ${deletedCount} 条日志`);
    await loadData(1);
  } catch (error: any) {
    Message.error(error?.message || "清空失败");
  } finally {
    clearing.value = false;
  }
};

// 监听路由参数变化，自动执行查询
watch(
  () => route.query.jobId,
  newJobId => {
    if (newJobId) {
      searchForm.jobId = newJobId as string;
      loadData(1);
    }
  },
  { immediate: true }
);

onMounted(async () => {
  // 如果路由中没有 jobId 参数，则初始化加载数据
  if (!route.query.jobId) {
    await loadData();
  }
});
</script>

<style scoped lang="scss">
.sysjobresults-page :deep(.uvp-search-panel .arco-input-wrapper),
.sysjobresults-page :deep(.uvp-search-panel .arco-select-view-single),
.sysjobresults-page :deep(.uvp-search-panel .arco-picker) {
  box-sizing: border-box;
  background: var(--uvp-search-control-bg) !important;
  border: 1px solid var(--uvp-search-secondary-btn-border) !important;
  border-radius: 10px !important;
  box-shadow: var(--uvp-search-control-shadow) !important;
}

.sysjobresults-page :deep(.uvp-search-panel .arco-input-wrapper:focus-within),
.sysjobresults-page :deep(.uvp-search-panel .arco-select-view-single.arco-select-view-focus),
.sysjobresults-page :deep(.uvp-search-panel .arco-picker-focused) {
  border-color: var(--uvp-brand) !important;
  box-shadow: var(--uvp-search-control-focus-shadow) !important;
}

.sysjobresults-page :deep(.uvp-search-panel .arco-input::placeholder),
.sysjobresults-page :deep(.uvp-search-panel .arco-select-view-input::placeholder),
.sysjobresults-page :deep(.uvp-search-panel .arco-picker input::placeholder) {
  color: var(--uvp-text-tertiary) !important;
  opacity: 1;
}

.sysjobresults-page :deep(.uvp-search-panel .arco-btn) {
  box-sizing: border-box;
  border-radius: 10px;
}

:deep(.arco-table-cell) {
  .arco-space {
    gap: 2px;
  }
}

:deep(.arco-btn-text.arco-btn-size-small) {
  color: var(--color-text-2);

  &:hover {
    color: rgb(var(--primary-6));
    background: var(--color-primary-light-1);
  }

  &.arco-btn-status-danger {
    color: rgb(var(--danger-6));

    &:hover {
      background: var(--color-danger-light-1);
    }
  }
}
</style>
