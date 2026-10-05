<template>
  <div class="firmware-repo-container">
    <s-layout-search>
      <template #fields>
        <a-input
          v-model="filters.manufacturer"
          placeholder="厂商"
          allow-clear
          style="width: 200px"
          @clear="handleSearch"
          @press-enter="handleSearch"
        />
        <a-select v-model="filters.status" placeholder="状态" allow-clear style="width: 150px" @change="handleSearch">
          <a-option value="draft">草稿</a-option>
          <a-option value="published">已发布</a-option>
          <a-option value="archived">已归档</a-option>
        </a-select>
      </template>
      <template #actions>
        <a-button type="primary" @click="handleSearch">
          <template #icon>
            <Search :size="15" />
          </template>
          查询
        </a-button>
        <a-button @click="handleReset">
          <template #icon>
            <RotateCcw :size="15" />
          </template>
          重置
        </a-button>
      </template>
      <template #extra>
        <a-button type="primary" @click="showUploadDialog = true">
          <template #icon>
            <Plus :size="15" />
          </template>
          上传固件
        </a-button>
      </template>
    </s-layout-search>

    <a-table
      class="uvp-data-table"
      :columns="columns"
      :data="tableData"
      :loading="loading"
      :pagination="false"
      :scroll="{ x: 'max-content' }"
      @page-change="handlePageChange"
      @page-size-change="handlePageSizeChange"
    >
      <template #fileName="{ record }">
        <div class="file-info">
          <div class="file-name">{{ record.fileName }}</div>
          <div class="file-size">{{ formatFileSize(record.fileSize) }}</div>
        </div>
      </template>

      <template #status="{ record }">
        <a-tag :color="getStatusColor(record.status)">
          {{ getStatusText(record.status) }}
        </a-tag>
      </template>

      <template #createdAt="{ record }">
        {{ formatDateTime(record.createdAt) }}
      </template>

      <template #actions="{ record }">
        <a-space :size="4">
          <a-button v-if="record.status === 'draft'" type="text" size="small" status="success" @click="handlePublish(record)">
            <template #icon>
              <Check :size="14" />
            </template>
            发布
          </a-button>
          <a-button v-if="record.status === 'published'" type="text" size="small" status="warning" @click="handleArchive(record)">
            <template #icon>
              <Archive :size="14" />
            </template>
            归档
          </a-button>
          <a-button type="text" size="small" @click="handleDownload(record)">
            <template #icon>
              <Download :size="14" />
            </template>
            下载
          </a-button>
          <a-button type="text" size="small" status="danger" @click="handleDelete(record)">
            <template #icon>
              <Trash2 :size="14" />
            </template>
            删除
          </a-button>
        </a-space>
      </template>
    </a-table>

    <footer class="uvp-pagination-bar">
      <a-pagination
        :current="pagination.current"
        :page-size="pagination.pageSize"
        :total="pagination.total"
        :page-size-options="[10, 20, 50, 100]"
        show-total
        show-page-size
        show-jumper
        @change="handlePageChange"
        @page-size-change="handlePageSizeChange"
      />
    </footer>

    <FirmwareUploadDialog v-model:visible="showUploadDialog" @success="handleUploadSuccess" />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Search, RotateCcw, Plus, Download, Trash2, Check, Archive } from "lucide-vue-next";
import { listFirmware, deleteFirmware, generateDownloadLink, updateFirmwareStatus } from "./api";
import FirmwareUploadDialog from "./FirmwareUploadDialog.vue";
import type { FirmwareRepository } from "./api";
import type { TableColumnData } from "@arco-design/web-vue";

const columns: TableColumnData[] = [
  {
    title: "固件版本",
    dataIndex: "version",
    width: 150
  },
  {
    title: "厂商",
    dataIndex: "manufacturer",
    width: 150
  },
  {
    title: "适用型号",
    dataIndex: "modelPattern",
    width: 200,
    render: ({ record }: { record: FirmwareRepository }) => record.modelPattern || "-"
  },
  {
    title: "文件信息",
    slotName: "fileName",
    width: 250
  },
  {
    title: "状态",
    slotName: "status",
    width: 100
  },
  {
    title: "上传时间",
    slotName: "createdAt",
    width: 180
  },
  {
    title: "备注",
    dataIndex: "remark",
    ellipsis: true,
    tooltip: true,
    render: ({ record }: { record: FirmwareRepository }) => record.remark || "-"
  },
  {
    title: "操作",
    slotName: "actions",
    width: 160,
    fixed: "right"
  }
];

const filters = reactive({
  manufacturer: "",
  status: ""
});

const tableData = ref<FirmwareRepository[]>([]);
const loading = ref(false);
const showUploadDialog = ref(false);

const pagination = reactive({
  current: 1,
  pageSize: 10,
  total: 0
});

async function fetchData() {
  loading.value = true;
  try {
    const { data } = await listFirmware({
      page: pagination.current,
      pageSize: pagination.pageSize,
      manufacturer: filters.manufacturer || undefined,
      status: filters.status || undefined
    });
    tableData.value = data.list;
    pagination.total = data.total;
  } catch (error: any) {
    Message.error(error.message || "加载固件列表失败");
  } finally {
    loading.value = false;
  }
}

function handleSearch() {
  pagination.current = 1;
  fetchData();
}

function handleReset() {
  filters.manufacturer = "";
  filters.status = "";
  pagination.current = 1;
  fetchData();
}

function handlePageChange(page: number) {
  pagination.current = page;
  fetchData();
}

function handlePageSizeChange(pageSize: number) {
  pagination.pageSize = pageSize;
  pagination.current = 1;
  fetchData();
}

function handleUploadSuccess() {
  pagination.current = 1;
  fetchData();
}

async function handlePublish(record: FirmwareRepository) {
  try {
    await updateFirmwareStatus(record.id, "published");
    Message.success("固件已发布");
    record.status = "published";
  } catch (error: any) {
    Message.error(error.message || "发布失败");
  }
}

async function handleArchive(record: FirmwareRepository) {
  Modal.confirm({
    title: "确认归档",
    content: `确定要归档固件 "${record.version}" 吗？归档后设备升级时将无法选择该固件。`,
    modalClass: "uvp-system-dialog",
    onOk: async () => {
      try {
        await updateFirmwareStatus(record.id, "archived");
        Message.success("固件已归档");
        record.status = "archived";
      } catch (error: any) {
        Message.error(error.message || "归档失败");
      }
    }
  });
}

async function handleDownload(record: FirmwareRepository) {
  try {
    const { data } = await generateDownloadLink(record.id);
    window.open(data.downloadUrl, "_blank");
    Message.success("下载链接已生成");
  } catch (error: any) {
    Message.error(error.message || "生成下载链接失败");
  }
}

function handleDelete(record: FirmwareRepository) {
  Modal.confirm({
    title: "确认删除",
    content: `确定要删除固件 "${record.version}" 吗？此操作不可恢复。`,
    modalClass: "uvp-system-dialog",
    onOk: async () => {
      try {
        await deleteFirmware(record.id);
        Message.success("删除成功");
        fetchData();
      } catch (error: any) {
        Message.error(error.message || "删除失败");
      }
    }
  });
}

function formatFileSize(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`;
}

function formatDateTime(dateString: string): string {
  const date = new Date(dateString);
  return date.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}

function getStatusColor(status: string): string {
  const colorMap: Record<string, string> = {
    draft: "gray",
    published: "green",
    archived: "orange"
  };
  return colorMap[status] || "gray";
}

function getStatusText(status: string): string {
  const textMap: Record<string, string> = {
    draft: "草稿",
    published: "已发布",
    archived: "已归档"
  };
  return textMap[status] || status;
}

onMounted(() => {
  fetchData();
});
</script>

<style scoped>
.firmware-repo-container {
  display: flex;
  flex-direction: column;
  gap: 12px;
  height: 100%;
  padding: 20px;
}

.uvp-pagination-bar {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding-top: 2px;
}

.file-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.file-name {
  font-weight: 500;
}

.file-size {
  font-size: 12px;
  color: var(--color-text-3);
}
</style>
