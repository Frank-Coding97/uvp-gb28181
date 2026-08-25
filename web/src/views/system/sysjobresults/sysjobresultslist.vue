<template>
  <div class="snow-page sysjobresults-page">
    <div class="snow-inner uvp-page-shell-flat">
      <a-card :loading="loading" :bordered="false">
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
            <a-table-column title="操作" :width="96" :fixed="isMobile ? '' : 'right'">
              <template #cell="{ record }">
                <div class="uvp-table-actions">
                  <a-popconfirm content="确定要删除这条数据吗？" @ok="handleDelete(record.id)">
                    <a-link class="uvp-table-action uvp-table-action--delete" v-hasPerm="['system:sysjobresults:delete']">
                      删除
                    </a-link>
                  </a-popconfirm>
                </div>
              </template>
            </a-table-column>
          </template>
        </a-table>
      </a-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, computed, onMounted, watch } from "vue";
import { useRoute } from "vue-router";
import { useSysJobResultsPluginHook } from "@/hooks/useSysJobResults";
import { formatTime } from "@/globals";
import { useDevicesSize } from "@/hooks/useDevicesSize";

const route = useRoute();
const { isMobile } = useDevicesSize();
const { dataList, loading, total, currentPage, pageSize, fetchDataList, deleteData, resetSearchParams } =
  useSysJobResultsPluginHook();

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
  pageSizeOptions: [10, 20, 30, 50]
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

// 删除数据
const handleDelete = async (id: number) => {
  try {
    await deleteData(id);
    // 重新加载当前页数据
    await loadData();
    // 显示删除成功消息
    // 这里可以使用项目的消息提示机制
  } catch (error) {
    // 显示删除失败消息
    console.error("删除失败:", error);
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
  height: 44px;
  min-height: 44px;
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
  height: 44px;
  min-height: 44px;
  border-radius: 10px;
}

.sysjobresults-page :deep(.uvp-data-table .arco-pagination-item),
.sysjobresults-page :deep(.uvp-data-table .arco-pagination-options .arco-select-view-single),
.sysjobresults-page :deep(.uvp-data-table .arco-pagination-jumper-input) {
  box-sizing: border-box;
  min-width: 32px;
  height: 32px;
  min-height: 32px;
  border-radius: 8px;
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
