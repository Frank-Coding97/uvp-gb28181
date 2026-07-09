<template>
  <div class="snow-page">
    <div class="snow-inner uvp-page-shell-flat">
      <s-layout-search>
        <template #fields>
          <a-input v-model="form.name" placeholder="请输入表名" style="width: 176px" allow-clear @press-enter="search" />
          <a-input
            v-model="form.moduleName"
            placeholder="请输入模块名称"
            style="width: 176px"
            allow-clear
            @press-enter="search"
          />
        </template>
        <template #actions>
          <a-button type="primary" @click="search">
            <template #icon><IconSearch /></template>
            <span>查询</span>
          </a-button>
          <a-button @click="reset">
            <template #icon><IconRefresh /></template>
            <span>重置</span>
          </a-button>
        </template>
        <template #extra>
          <a-button type="primary" @click="onImport" v-hasPerm="['system:codegen:batchInsert']">
            <template #icon><IconImport /></template>
            <span>导入表</span>
          </a-button>
        </template>
      </s-layout-search>

      <a-table
        class="uvp-data-table"
        row-key="id"
        :data="sysGenList"
        :bordered="false"
        :loading="loading"
        :scroll="tableScroll"
        :pagination="pagination"
      >
        <template #columns>
          <a-table-column title="表名" data-index="name" :width="150"></a-table-column>
          <a-table-column title="目录/模块" data-index="moduleName" :width="150"></a-table-column>
          <a-table-column title="描述" data-index="describe" :ellipsis="true" :width="200" :tooltip="true"></a-table-column>
          <a-table-column title="创建时间" data-index="createdAt" :width="180" align="center">
            <template #cell="{ record }">
              {{ record.createdAt ? formatTime(record.createdAt) : "-" }}
            </template>
          </a-table-column>
          <a-table-column title="操作" :width="220" align="center" :fixed="isMobile ? '' : 'right'">
            <template #cell="{ record }">
              <div class="uvp-table-actions">
                <a-link
                  class="uvp-table-action uvp-table-action--edit"
                  @click="onEdit(record)"
                  v-hasPerm="['system:codegen:update']"
                >
                  <IconEdit />
                  <span>配置</span>
                </a-link>
                <a-link
                  class="uvp-table-action uvp-table-action--preview"
                  @click="onPreview(record)"
                  v-hasPerm="['system:codegen:preview']"
                >
                  <IconEye />
                  <span>预览</span>
                </a-link>
                <a-popconfirm type="warning" content="确定生成代码吗?" @ok="onGenCode(record)">
                  <a-link class="uvp-table-action uvp-table-action--execute" v-hasPerm="['system:codegen:generate']">
                    <IconPlayArrow />
                    <span>生成</span>
                  </a-link>
                </a-popconfirm>
                <a-popconfirm type="warning" content="确定同步字段信息吗?" @ok="onRefreshFields(record)">
                  <a-link class="uvp-table-action uvp-table-action--sync" v-hasPerm="['system:codegen:refreshFields']">
                    <IconRefresh />
                    <span>同步</span>
                  </a-link>
                </a-popconfirm>
                <a-popconfirm type="warning" content="确定删除该生成配置吗?" @ok="onDelete(record)">
                  <a-link class="uvp-table-action uvp-table-action--delete" v-hasPerm="['system:codegen:delete']">
                    <IconDelete />
                    <span>删除</span>
                  </a-link>
                </a-popconfirm>
              </div>
            </template>
          </a-table-column>
        </template>
      </a-table>
    </div>

    <!-- 导入表对话框 -->
    <a-modal
      modal-class="uvp-system-dialog"
      v-model:visible="importVisible"
      @ok="handleImport"
      @cancel="afterImportClose"
      :ok-loading="importLoading"
      :width="layoutMode.width"
    >
      <template #title> 导入表 </template>
      <div>
        <a-space direction="vertical" fill style="width: 100%">
          <a-alert type="info"> 选择需要导入的表，系统将自动生成代码生成配置 </a-alert>
          <a-table
            class="uvp-data-table"
            row-key="tableName"
            :data="tableList"
            :bordered="false"
            :loading="tableLoading"
            :pagination="false"
            :scroll="importTableScroll"
            v-model:selectedKeys="selectedTables"
            :row-selection="{
              type: 'checkbox',
              showCheckedAll: true,
              onlyCurrent: false
            }"
          >
            <template #columns>
              <a-table-column title="表名" data-index="tableName" :width="200"></a-table-column>
              <a-table-column title="描述" data-index="tableComment.String" :ellipsis="true" :tooltip="true"></a-table-column>
            </template>
          </a-table>
        </a-space>
      </div>
    </a-modal>

    <!-- 修改配置抽屉 -->
    <CodegenConfigDrawer v-model:visible="editVisible" :record-id="currentEditId" @success="handleEditSuccess" />

    <!-- 代码预览模态框 -->
    <CodegenPreviewModal
      v-model:visible="previewVisible"
      :record-id="currentPreviewId"
      :record="currentPreviewRecord"
      @success="handlePreviewSuccess"
    />
  </div>
</template>

<script setup lang="ts">
import {
  getSysGenListAPI,
  batchInsertSysGenAPI,
  deleteSysGenAPI,
  refreshFields,
  type SysGenItem,
  type SysGenListParams
} from "@/api/sysgen";

import { generateCode, getTables, type TableInfo } from "@/api/syscodegen";
import { formatTime } from "@/globals";
import CodegenConfigDrawer from "./components/codegen-config-drawer.vue";
import CodegenPreviewModal from "./components/codegen-preview-modal.vue";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import { IconDelete, IconEdit, IconEye, IconImport, IconPlayArrow, IconRefresh, IconSearch } from "@arco-design/web-vue/es/icon";
const { isMobile } = useDevicesSize();
const layoutMode = computed(() => {
  let info = {
    mobile: {
      width: "95%",
      drawerWidth: "100%",
      layout: "vertical"
    },
    desktop: {
      width: "40%",
      drawerWidth: "75%",
      layout: "horizontal"
    }
  };
  return isMobile.value ? info.mobile : info.desktop;
});

// 查询表单
const form = ref<SysGenListParams>({
  name: "",
  moduleName: ""
});

// 列表数据
const sysGenList = ref<SysGenItem[]>([]);
const loading = ref(false);
const currentPage = ref(1);
const pageSize = ref(10);
const total = ref(0);
const pagination = ref({
  current: currentPage,
  pageSize: pageSize,
  total: total,
  showPageSize: true,
  showTotal: true,
  onChange: (page: number) => {
    currentPage.value = page;
    getSysGenList();
  },
  onPageSizeChange: (size: number) => {
    pageSize.value = size;
    currentPage.value = 1;
    getSysGenList();
  }
});
const tableScroll = computed(() => ({
  x: "100%",
  minWidth: 1200,
  ...(sysGenList.value.length > 0 ? { y: "100%" } : {})
}));

// 获取代码生成配置列表
const getSysGenList = async () => {
  loading.value = true;
  try {
    const params: SysGenListParams = {
      pageNum: currentPage.value,
      pageSize: pageSize.value,
      ...form.value
    };
    const res = await getSysGenListAPI(params);
    if (res.data) {
      sysGenList.value = res.data.list || [];
      total.value = res.data.total || 0;
    }
  } catch (error) {
    console.error("获取代码生成配置列表失败:", error);
    arcoMessage("error", "获取代码生成配置列表失败");
  } finally {
    loading.value = false;
  }
};

// 搜索
const search = () => {
  currentPage.value = 1;
  getSysGenList();
};

// 重置
const reset = () => {
  form.value = {
    name: "",
    moduleName: ""
  };
  currentPage.value = 1;
  getSysGenList();
};

// 导入表相关
const importVisible = ref(false);
const importLoading = ref(false);
const tableList = ref<TableInfo[]>([]);
const tableLoading = ref(false);
const selectedTables = ref<string[]>([]);
const importTableScroll = computed(() => ({
  x: "100%",
  ...(tableList.value.length > 0 ? { y: 400 } : {})
}));

// 获取表列表
const getTableList = async () => {
  tableLoading.value = true;
  try {
    const res = await getTables("");
    if (res.data) {
      tableList.value = res.data.tables || [];
    }
  } catch (error) {
    console.error("获取表列表失败:", error);
    arcoMessage("error", "获取表列表失败");
  } finally {
    tableLoading.value = false;
  }
};

// 打开导入对话框
const onImport = () => {
  importVisible.value = true;
  selectedTables.value = [];
  getTableList();
};

// 执行导入
const handleImport = async () => {
  if (selectedTables.value.length === 0) {
    arcoMessage("warning", "请选择要导入的表");
    return false;
  }
  importLoading.value = true;
  try {
    const res = await batchInsertSysGenAPI({
      database: "", // 使用当前连接的数据库
      tables: selectedTables.value
    });

    if (res.data) {
      const { successCount, failedCount } = res.data;
      if (successCount > 0) {
        arcoMessage("success", `成功导入 ${successCount} 个表`);
      }
      if (failedCount > 0) {
        arcoMessage("warning", `有 ${failedCount} 个表导入失败`);
      }
    }

    getSysGenList();
    return true;
  } catch (error) {
    console.error("导入表失败:", error);
    arcoMessage("error", "导入表失败");
    return false;
  } finally {
    importLoading.value = false;
  }
};

// 关闭导入对话框
const afterImportClose = () => {
  importVisible.value = false;
  selectedTables.value = [];
  tableList.value = [];
};

// 删除配置
const onDelete = async (record: SysGenItem) => {
  try {
    // 调用删除API
    await deleteSysGenAPI(record.id);
    arcoMessage("success", "删除成功");
    getSysGenList(); // 刷新列表
  } catch (error) {
    console.error("删除失败:", error);
  }
};

// 修改配置相关
const editVisible = ref(false);
const currentEditId = ref(0);

// 打开修改对话框
const onEdit = (record: SysGenItem) => {
  currentEditId.value = record.id;
  editVisible.value = true;
};

// 处理编辑成功
const handleEditSuccess = () => {
  getSysGenList(); // 刷新列表
};

// 生成代码
const onGenCode = async (record: SysGenItem) => {
  try {
    await generateCode(record.id);
    arcoMessage("success", "代码生成成功");
  } catch (error) {
    console.error("代码生成失败:", error);
  }
};

// 同步字段
const onRefreshFields = async (record: SysGenItem) => {
  try {
    await refreshFields(record.id);
    arcoMessage("success", "字段同步成功");
    getSysGenList(); // 刷新列表
  } catch (error) {
    console.error("字段同步失败:", error);
    arcoMessage("error", "字段同步失败");
  }
};

// 预览
const previewVisible = ref(false);
const currentPreviewId = ref(0);
const currentPreviewRecord = ref<SysGenItem | null>(null);

// 打开预览模态框
const onPreview = (record: SysGenItem) => {
  currentPreviewId.value = record.id;
  currentPreviewRecord.value = record;
  previewVisible.value = true;
};

// 处理预览成功
const handlePreviewSuccess = () => {
  getSysGenList(); // 刷新列表
};

onMounted(() => {
  // 初始化数据
  getSysGenList();
});
</script>

<style lang="scss" scoped></style>
