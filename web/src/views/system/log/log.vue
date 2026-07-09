<template>
  <div class="snow-page">
    <div class="snow-inner uvp-page-shell-flat">
      <s-layout-search>
        <template #fields>
          <a-input v-model="form.username" placeholder="请输入用户名" style="width: 176px" allow-clear @press-enter="search" />
          <a-input v-model="form.module" placeholder="请输入操作模块" style="width: 176px" allow-clear @press-enter="search" />
          <a-select v-model="form.operation" placeholder="操作类型" style="width: 126px" allow-clear>
            <a-option value="create">新增</a-option>
            <a-option value="update">更新</a-option>
            <a-option value="delete">删除</a-option>
            <a-option value="query">查询</a-option>
            <a-option value="login">登录</a-option>
            <a-option value="logout">登出</a-option>
            <a-option value="export">导出</a-option>
            <a-option value="import">导入</a-option>
          </a-select>
          <a-select v-model="form.status" placeholder="状态" style="width: 126px" allow-clear>
            <a-option value="success">成功</a-option>
            <a-option value="error">失败</a-option>
          </a-select>
          <a-range-picker v-model="dateRange" style="width: 240px" allow-clear />
        </template>
        <template #actions>
          <a-button type="primary" @click="search">
            <template #icon><icon-search /></template>
            <span>查询</span>
          </a-button>
          <a-button @click="reset">
            <template #icon><icon-refresh /></template>
            <span>重置</span>
          </a-button>
        </template>
        <template #extra>
          <a-button @click="onExport" v-hasPerm="['system:log:export']">
            <template #icon><icon-export /></template>
            <span>导出</span>
          </a-button>
          <a-popconfirm type="warning" content="确定删除选中的日志吗?" @ok="onDeleteSelected">
            <a-button status="danger" v-hasPerm="['system:log:delete']">
              <template #icon><icon-delete /></template>
              <span>删除</span>
            </a-button>
          </a-popconfirm>
        </template>
      </s-layout-search>

      <a-table
        class="uvp-data-table"
        row-key="id"
        :data="logList"
        :bordered="false"
        :loading="loading"
        :pagination="pagination"
        :scroll="tableScroll"
        v-model:selectedKeys="selectedKeys"
        :row-selection="{ type: 'checkbox', showCheckedAll: true }"
        @page-change="handlePageChange"
        @page-size-change="handlePageSizeChange"
      >
        <template #columns>
          <a-table-column title="用户名" data-index="username" :width="120"></a-table-column>
          <a-table-column title="操作模块" data-index="module" :width="150"></a-table-column>
          <a-table-column title="操作类型" :width="100">
            <template #cell="{ record }">
              <a-tag v-if="record.operation === 'create'" color="green">新增</a-tag>
              <a-tag v-else-if="record.operation === 'update'" color="blue">更新</a-tag>
              <a-tag v-else-if="record.operation === 'delete'" color="red">删除</a-tag>
              <a-tag v-else-if="record.operation === 'query'" color="arcoblue">查询</a-tag>
              <a-tag v-else-if="record.operation === 'login'" color="purple">登录</a-tag>
              <a-tag v-else-if="record.operation === 'logout'" color="orange">登出</a-tag>
              <a-tag v-else-if="record.operation === 'export'" color="cyan">导出</a-tag>
              <a-tag v-else-if="record.operation === 'import'" color="pink">导入</a-tag>
              <span v-else>{{ record.operation }}</span>
            </template>
          </a-table-column>
          <a-table-column title="请求方法" data-index="method" :width="100" align="center"></a-table-column>
          <a-table-column title="请求路径" data-index="path" :width="200" :ellipsis="true" :tooltip="true"></a-table-column>
          <a-table-column title="IP地址" data-index="ip" :width="130"></a-table-column>
          <a-table-column title="状态码" :width="100" align="center">
            <template #cell="{ record }">
              <a-tag v-if="record.statusCode < 400" color="green">{{ record.statusCode }}</a-tag>
              <a-tag v-else color="red">{{ record.statusCode }}</a-tag>
            </template>
          </a-table-column>
          <a-table-column title="耗时(ms)" :width="100" align="center">
            <template #cell="{ record }">
              <span :style="{ color: record.duration > 1000 ? 'red' : record.duration > 500 ? 'orange' : 'green' }">
                {{ record.duration }}
              </span>
            </template>
          </a-table-column>
          <a-table-column title="操作时间" data-index="createdAt" :width="180">
            <template #cell="{ record }">{{ formatTime(record.createdAt) }}</template>
          </a-table-column>
          <a-table-column title="操作" :width="96" align="center" :fixed="isMobile ? '' : 'right'">
            <template #cell="{ record }">
              <div class="uvp-table-actions">
                <a-link class="uvp-table-action uvp-table-action--detail" @click="viewDetail(record)">详情</a-link>
              </div>
            </template>
          </a-table-column>
        </template>
      </a-table>
    </div>

    <!-- 详情弹窗 -->
    <a-modal
      modal-class="uvp-system-dialog"
      v-model:visible="detailVisible"
      :width="layoutMode.width"
      :footer="false"
      @close="detailVisible = false"
    >
      <template #title>操作日志详情</template>
      <a-descriptions class="uvp-system-description uvp-system-description--compact" :column="1" bordered size="medium">
        <a-descriptions-item label="ID">{{ currentLog.id }}</a-descriptions-item>
        <a-descriptions-item label="用户名">{{ currentLog.username }}</a-descriptions-item>
        <a-descriptions-item label="操作模块">{{ currentLog.module }}</a-descriptions-item>
        <a-descriptions-item label="操作类型">
          <a-tag v-if="currentLog.operation === 'create'" color="green">新增</a-tag>
          <a-tag v-else-if="currentLog.operation === 'update'" color="blue">更新</a-tag>
          <a-tag v-else-if="currentLog.operation === 'delete'" color="red">删除</a-tag>
          <a-tag v-else-if="currentLog.operation === 'query'" color="arcoblue">查询</a-tag>
          <a-tag v-else-if="currentLog.operation === 'login'" color="purple">登录</a-tag>
          <a-tag v-else-if="currentLog.operation === 'logout'" color="orange">登出</a-tag>
          <a-tag v-else-if="currentLog.operation === 'export'" color="cyan">导出</a-tag>
          <a-tag v-else-if="currentLog.operation === 'import'" color="pink">导入</a-tag>
          <span v-else>{{ currentLog.operation }}</span>
        </a-descriptions-item>
        <a-descriptions-item label="请求方法">{{ currentLog.method }}</a-descriptions-item>
        <a-descriptions-item label="请求路径">{{ currentLog.path }}</a-descriptions-item>
        <a-descriptions-item label="IP地址">{{ currentLog.ip }}</a-descriptions-item>
        <a-descriptions-item label="用户代理">{{ currentLog.userAgent }}</a-descriptions-item>
        <a-descriptions-item label="状态码">
          <a-tag v-if="currentLog.statusCode < 400" color="green">{{ currentLog.statusCode }}</a-tag>
          <a-tag v-else color="red">{{ currentLog.statusCode }}</a-tag>
        </a-descriptions-item>
        <a-descriptions-item label="耗时">{{ currentLog.duration }} ms</a-descriptions-item>
        <a-descriptions-item label="部门">{{ currentLog.deptName }}</a-descriptions-item>
        <a-descriptions-item label="操作时间">{{ formatTime(currentLog.createdAt) }}</a-descriptions-item>
        <a-descriptions-item label="请求参数">
          <pre class="log-detail-block">{{ currentLog.requestData }}</pre>
        </a-descriptions-item>
        <a-descriptions-item label="响应数据">
          <pre class="log-detail-block">{{ currentLog.responseData }}</pre>
        </a-descriptions-item>
        <a-descriptions-item label="错误信息" v-if="currentLog.errorMsg">
          <pre class="log-detail-block log-detail-block--error">{{ currentLog.errorMsg }}</pre>
        </a-descriptions-item>
      </a-descriptions>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from "vue";
import { getOperationLogsAPI, deleteOperationLogsAPI, exportOperationLogsAPI, type OperationLogItem } from "@/api/log";
import useGlobalProperties from "@/hooks/useGlobalProperties";
import { formatTime } from "@/globals";
import { useDevicesSize } from "@/hooks/useDevicesSize";
const { isMobile } = useDevicesSize();
const layoutMode = computed(() => {
  let info = {
    mobile: {
      width: "95%",
      layout: "vertical"
    },
    desktop: {
      width: "40%",
      layout: "horizontal"
    }
  };
  return isMobile.value ? info.mobile : info.desktop;
});

const proxy = useGlobalProperties();

// 表单数据
const form = ref({
  username: "",
  module: "",
  operation: "",
  status: "",
  ip: ""
});

// 日期范围
const dateRange = ref([]);

// 表格相关
const logList = ref<OperationLogItem[]>([]);
const loading = ref(false);
const selectedKeys = ref<number[]>([]);
const tableScroll = computed(() => ({
  x: "100%",
  minWidth: 1200,
  ...(logList.value.length > 0 ? { y: "100%" } : {})
}));
const pagination = reactive({
  current: 1,
  pageSize: 10,
  total: 0,
  showTotal: true,
  showJumper: true,
  showPageSize: true
});

// 详情弹窗
const detailVisible = ref(false);
const currentLog = ref<OperationLogItem>({} as OperationLogItem);

// 查询日志列表
const getLogList = async () => {
  try {
    loading.value = true;
    const params: any = {
      pageNum: pagination.current,
      pageSize: pagination.pageSize,
      order: "id desc",
      username: form.value.username,
      module: form.value.module,
      operation: form.value.operation,
      status: form.value.status,
      ip: form.value.ip
    };

    if (dateRange.value && dateRange.value.length === 2) {
      params.startTime = dateRange.value[0];
      params.endTime = dateRange.value[1];
    }

    const res = await getOperationLogsAPI(params);
    logList.value = res.data.list as OperationLogItem[];
    pagination.total = res.data.total;
  } catch (error) {
    console.error(error);
  } finally {
    loading.value = false;
  }
};

// 查询
const search = () => {
  pagination.current = 1;
  getLogList();
};

// 重置
const reset = () => {
  form.value = {
    username: "",
    module: "",
    operation: "",
    status: "",
    ip: ""
  };
  dateRange.value = [];
  pagination.current = 1;
  getLogList();
};

// 分页变化
const handlePageChange = (current: number) => {
  pagination.current = current;
  getLogList();
};

const handlePageSizeChange = (pageSize: number) => {
  pagination.pageSize = pageSize;
  pagination.current = 1;
  getLogList();
};

// 查看详情
const viewDetail = (record: OperationLogItem) => {
  currentLog.value = { ...record };
  detailVisible.value = true;
};

// 删除选中日志
const onDeleteSelected = async () => {
  if (selectedKeys.value.length === 0) {
    proxy.$message.warning("请先选择要删除的日志");
    return;
  }

  try {
    await deleteOperationLogsAPI({ ids: selectedKeys.value });
    proxy.$message.success("删除成功");
    selectedKeys.value = [];
    getLogList();
  } catch (error) {
    console.error(error);
  }
};

// 导出日志
const onExport = async () => {
  try {
    const params: any = {
      username: form.value.username,
      module: form.value.module,
      operation: form.value.operation,
      status: form.value.status,
      ip: form.value.ip
    };

    if (dateRange.value && dateRange.value.length === 2) {
      params.startTime = dateRange.value[0];
      params.endTime = dateRange.value[1];
    }

    try {
      const response: any = await exportOperationLogsAPI(params);
      // 创建下载链接
      const blob = new Blob([response], { type: "text/csv" });
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = `operation_logs_${new Date().toISOString().slice(0, 19).replace(/:/g, "-")}.csv`;
      link.click();
      window.URL.revokeObjectURL(url);

      proxy.$message.success("导出成功");
    } catch (error) {
      console.error(error);
    }
  } catch (error) {
    console.error(error);
    proxy.$message.error("导出失败");
  }
};

// 初始化
onMounted(() => {
  getLogList();
});
</script>

<style lang="scss" scoped>
.log-detail-block {
  padding: 10px 12px;
  margin: 0;
  overflow: auto;
  color: var(--uvp-text-secondary);
  white-space: pre-wrap;
  word-break: break-word;
  background: #f8fbff;
  border: 1px solid #e6edf7;
  border-radius: 10px;
}

.log-detail-block--error {
  color: var(--uvp-danger);
  background: #fff7f7;
  border-color: #f4d0d0;
}
</style>

<style lang="scss" scoped>
.text-right-gap {
  margin-right: 5px;
}
</style>
