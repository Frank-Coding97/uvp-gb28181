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
        <div class="uvp-table-actions firmware-repo-actions">
          <a-link v-if="record.status === 'draft'" class="uvp-table-action uvp-table-action--edit" @click="handlePublish(record)">
            <template #icon><Check :size="13" /></template><span>发布</span>
          </a-link>
          <a-link
            v-if="record.status === 'published'"
            class="uvp-table-action uvp-table-action--edit"
            @click="handleArchive(record)"
          >
            <template #icon><Archive :size="13" /></template><span>归档</span>
          </a-link>
          <a-link class="uvp-table-action" :disabled="downloading" @click="handleDownload(record)">
            <template #icon><Download :size="13" /></template><span>下载</span>
          </a-link>
          <a-link class="uvp-table-action uvp-table-action--delete" :disabled="deleting" @click="handleDelete(record)">
            <template #icon><Trash2 :size="13" /></template><span>删除</span>
          </a-link>
        </div>
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
    // ⛔ 必须 align="center"：`.uvp-table-actions` 自身是 `inline-flex + justify-content:center`，
    //   但它只在**自己的盒子内**居中；单元格默认左对齐，盒子紧贴左边 ⇒ 按钮整体偏左、
    //   右侧留一截空白，两侧不对称（老板 10-06 指出）。
    //   全仓操作列主流写法就是 `align="center"`（9 处），设备列表亦然。
    width: 240,
    align: "center",
    fixed: "right"
  }
];

const filters = reactive({
  manufacturer: "",
  status: ""
});

const tableData = ref<FirmwareRepository[]>([]);
const loading = ref(false);
/** 删除进行中：禁用在途的删除按钮，防重复提交 */
const deleting = ref(false);
/** 下载进行中 */
const downloading = ref(false);
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

/**
 * 归档确认弹窗。与删除弹窗同形态（Modal.warning + 危险色确认按钮）。
 * ⛔ 全仓惯例（设备管理/告警/云录像共 9 处）一律 `status: "danger"`，没有用 warning 的 ——
 *   归档同样会改变固件可用性，按破坏性操作处理，不用黄色按钮另起一套观感。
 * ⛔ 不传 modalClass：Modal.warning/confirm 是 Arco 的 **simple 弹窗**（结构与
 *   `<a-modal modal-class>` 挂 `.arco-modal` 面板不同），`uvp-system-dialog` 那套样式挂不上去，
 *   设备列表也没挂 —— 统一就是统一到"不挂"。
 */
function handleArchive(record: FirmwareRepository) {
  Modal.warning({
    title: "归档固件",
    content: `即将归档固件「${record.version}」。归档后设备升级时将无法选择该固件,文件仍会保留。`,
    hideCancel: false,
    okText: "归档",
    cancelText: "取消",
    okButtonProps: { status: "danger" },
    onOk: async () => {
      try {
        await updateFirmwareStatus(record.id, "archived");
        Message.success("固件已归档");
        record.status = "archived";
      } catch (error: any) {
        Message.error(error?.message || "归档失败");
      }
    }
  });
}

/**
 * 下载固件。
 * ⛔ 下载 token 是**一次性**的（后端 GetDel 消费），所以必须「拿到链接立刻跳转」，
 *   中间任何一次重新请求都会把 token 用掉。window.open 传 "_blank" 即可，
 *   不要先 await 别的请求再跳。
 * ⛔ 链接由后端生成、路径与路由强绑定（firmware.DownloadRoutePath），
 *   前端**不要**自己拼路径，否则两边写歪就是 404（历史上就这么坏过）。
 */
async function handleDownload(record: FirmwareRepository) {
  downloading.value = true;
  try {
    const { data } = await generateDownloadLink(record.id);
    if (!data?.downloadUrl) throw new Error("后端未返回下载链接");
    // 同源直接开新窗口；被拦截（返回 null）时回退到隐藏 <a download> 触发
    const opened = window.open(data.downloadUrl, "_blank");
    if (!opened) {
      const anchor = document.createElement("a");
      anchor.href = data.downloadUrl;
      anchor.rel = "noopener";
      anchor.target = "_blank";
      // ⛔ download 属性让浏览器直接下载而不导航。同一条 URL 没有它就是一次页面导航，
      //   跨域时会被浏览器当新页面打开（体验是"点了没反应"），测试里还会触发真实请求。
      anchor.download = record.fileName || "";
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
    }
    Message.success("已开始下载");
  } catch (error: any) {
    Message.error(error?.message || "生成下载链接失败");
  } finally {
    downloading.value = false;
  }
}

/**
 * 删除确认弹窗。
 * ⛔ 与设备列表**逐字对齐** `device-mgmt/index.vue` handleDeleteDevice（1958 行）的配置：
 *   Modal.warning（**不是** Modal.confirm）+ hideCancel:false + okText/cancelText
 *   + okButtonProps:{status:"danger"}，且**不传 modalClass**。
 *
 * ⛔⛔ 为什么必须是 warning 而不是 confirm：两者都是 Arco 的 simple 弹窗，但
 *   warning 会渲染一个**橙色感叹号图标**（见 arco modal.js：messageType==="warning"
 *   才插 exclamation-circle-fill），confirm 没有图标。老板看到的就是这个差别。
 * ⛔ 也不传 modalClass="uvp-system-dialog"：那是给 `<a-modal modal-class>` 用的
 *   （挂在 .arco-modal 面板上），simple 弹窗结构不同，挂了不生效、反而和设备列表不一致。
 */
function handleDelete(record: FirmwareRepository) {
  Modal.warning({
    title: "删除固件",
    content: `即将删除固件「${record.version}」（${record.fileName}）及其文件。此操作不可恢复,是否继续?`,
    hideCancel: false,
    okText: "删除",
    cancelText: "取消",
    okButtonProps: { status: "danger" },
    onOk: async () => {
      deleting.value = true;
      try {
        await deleteFirmware(record.id);
        Message.success("固件已删除");
        afterMutateSuccess();
      } catch (error: any) {
        // ⛔ 后端对"已被设备升级记录引用"会返回明确原因，直接透传给用户，
        //   不要二次包装成"删除失败"把原因吃掉。
        Message.error(error?.message || "删除失败");
      } finally {
        deleting.value = false;
      }
    }
  });
}

/**
 * 删除/发布/归档后刷新列表。
 * ⛔ 删掉当前页最后一条时，页码会停在越界页上，列表变成空白 —— 看起来像"删了但还在"。
 *   这里回退一页再取。
 */
function afterMutateSuccess() {
  if (tableData.value.length <= 1 && pagination.current > 1) {
    pagination.current -= 1;
  }
  return fetchData();
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
  color: var(--uvp-text-tertiary);
}

/* 操作列：只补「居中 + 不换行」。
   ⛔ 不写 `flex-wrap: nowrap` —— 全局 `.uvp-table-actions` 是 `inline-flex`，
     强行 nowrap 会让窄屏下按钮溢出单元格（fixed 列不会自适应），
     宁可让它换行也不溢出。真正的居中由列上的 `align="center"` 负责，
     这里只保证内部按整组居中且不被挤压。 */
.firmware-repo-actions {
  justify-content: center;
  white-space: nowrap;
}
</style>
