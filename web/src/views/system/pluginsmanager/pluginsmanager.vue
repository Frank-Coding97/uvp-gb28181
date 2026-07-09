<template>
  <div class="snow-page">
    <div class="snow-inner">
      <s-layout-search>
        <template #fields>
          <a-input
            v-model="form.keyword"
            placeholder="请输入插件名称或作者"
            style="width: 240px"
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
          <a-button type="primary" @click="showImportModal" v-hasPerm="['system:pluginsmanager:import']">
            <template #icon><IconUpload /></template>
            <span>导入插件</span>
          </a-button>
        </template>
      </s-layout-search>

      <a-row class="uvp-system-card-grid plugin-grid" :gutter="[16, 16]">
        <a-col :xs="24" :sm="12" :md="8" :lg="6" v-for="plugin in filteredPlugins" :key="plugin.folderName">
          <a-card class="uvp-system-panel uvp-system-panel--dense plugin-card" hoverable @click="viewDetail(plugin)">
            <template #cover>
              <div class="plugin-cover">
                <span class="plugin-cover__icon">
                  <IconApps />
                </span>
                <span class="plugin-cover__version">v{{ plugin.version || "1.0.0" }}</span>
              </div>
            </template>
            <template #title>
              <div class="plugin-title">{{ plugin.name }}</div>
            </template>
            <a-descriptions
              class="uvp-system-description uvp-system-description--compact"
              :column="1"
              size="small"
              :bordered="false"
            >
              <a-descriptions-item label="版本">{{ plugin.version }}</a-descriptions-item>
              <a-descriptions-item label="作者">{{ plugin.author }}</a-descriptions-item>
              <a-descriptions-item label="描述">
                {{ truncateString(plugin.description, 18) }}
              </a-descriptions-item>
            </a-descriptions>
          </a-card>
        </a-col>
      </a-row>

      <a-empty v-if="filteredPlugins.length === 0 && !loading" :description="emptyDescription" />
    </div>

    <!-- 详情弹窗 -->
    <a-modal
      modal-class="uvp-system-dialog"
      v-model:visible="detailVisible"
      :width="layoutMode.width"
      :footer="false"
      @close="detailVisible = false"
    >
      <template #title>插件详情 - {{ currentPlugin.name }}</template>
      <div class="uvp-system-panel__stack">
        <div class="uvp-system-summary">
          <div class="uvp-system-summary__body">
            <a-descriptions class="uvp-system-description uvp-system-description--compact" :column="1" bordered size="medium">
              <a-descriptions-item label="插件名称">{{ currentPlugin.name }}</a-descriptions-item>
              <a-descriptions-item label="版本">{{ currentPlugin.version }}</a-descriptions-item>
              <a-descriptions-item label="描述">{{ currentPlugin.description }}</a-descriptions-item>
              <a-descriptions-item label="作者">{{ currentPlugin.author }}</a-descriptions-item>
              <a-descriptions-item label="邮箱">{{ currentPlugin.email }}</a-descriptions-item>
              <a-descriptions-item label="官网">
                <a-link v-if="currentPlugin.url" :href="currentPlugin.url" target="_blank">{{ currentPlugin.url }}</a-link>
                <span v-else>-</span>
              </a-descriptions-item>
              <a-descriptions-item label="文件夹名称">{{ currentPlugin.folderName }}</a-descriptions-item>
            </a-descriptions>
          </div>
        </div>
        <a-descriptions class="uvp-system-description uvp-system-description--compact" :column="1" bordered size="medium">
          <a-descriptions-item label="导出目录" v-if="currentPlugin.exportDirs && currentPlugin.exportDirs.length > 0">
            <a-space wrap>
              <a-tag v-for="dir in currentPlugin.exportDirs" :key="dir">{{ dir }}</a-tag>
            </a-space>
          </a-descriptions-item>
          <a-descriptions-item label="数据库表" v-if="currentPlugin.databaseTable && currentPlugin.databaseTable.length > 0">
            <a-space wrap>
              <a-tag v-for="table in currentPlugin.databaseTable" :key="table" color="blue">{{ table }}</a-tag>
            </a-space>
          </a-descriptions-item>
          <a-descriptions-item label="菜单项" v-if="currentPlugin.menus && currentPlugin.menus.length > 0">
            <a-table class="uvp-data-table" :data="currentPlugin.menus" :pagination="false" :bordered="false" size="small">
              <template #columns>
                <a-table-column title="路径" data-index="path"></a-table-column>
                <a-table-column title="类型" data-index="type">
                  <template #cell="{ record }">
                    <a-tag v-if="record.type === 1" color="green">目录</a-tag>
                    <a-tag v-if="record.type === 2" color="blue">菜单</a-tag>
                  </template>
                </a-table-column>
              </template>
            </a-table>
          </a-descriptions-item>
          <a-descriptions-item
            label="依赖"
            v-if="currentPlugin.dependencies && Object.keys(currentPlugin.dependencies).length > 0"
          >
            <div class="plugin-dependencies">
              <div v-for="(version, name) in currentPlugin.dependencies" :key="name" class="plugin-dependency">
                <span>{{ name }}: {{ version }}</span>
              </div>
            </div>
          </a-descriptions-item>
        </a-descriptions>
      </div>
      <div class="plugin-dialog-actions">
        <a-space direction="vertical" :size="12" style="width: 100%">
          <div class="plugin-dialog-actions__option">
            <a-checkbox v-model="exportIncludeData"> 导出包含数据库数据（不勾选则只会导出结构） </a-checkbox>
          </div>
          <a-space>
            <a-button type="primary" @click="exportPlugin(currentPlugin)" v-hasPerm="['system:pluginsmanager:export']">
              <template #icon><IconDownload /></template>
              <span>导出插件</span>
            </a-button>
            <a-popconfirm
              title="确定要卸载此插件吗？"
              content="卸载后将删除插件的所有文件和数据库表。"
              type="warning"
              @ok="handleDeletePlugin"
            >
              <a-button type="primary" status="danger" v-hasPerm="['system:pluginsmanager:uninstall']">
                <template #icon><IconDelete /></template>
                <span>卸载插件</span>
              </a-button>
            </a-popconfirm>
            <a-button @click="detailVisible = false">
              <template #icon><IconClose /></template>
              <span>退出</span>
            </a-button>
          </a-space>
        </a-space>
      </div>
    </a-modal>

    <!-- 导入插件弹窗组件 -->
    <PluginImportModal v-model="importModalVisible" @success="handleImportSuccess" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from "vue";
import { getPluginsExportAPI, exportPluginAPI, deletePluginAPI, type PluginExport } from "@/api/pluginsmanager";
import useGlobalProperties from "@/hooks/useGlobalProperties";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import { truncateString } from "@/utils/common-tools";
import PluginImportModal from "./components/PluginImportModal.vue";
import { IconApps, IconClose, IconDelete, IconDownload, IconRefresh, IconSearch, IconUpload } from "@arco-design/web-vue/es/icon";

const { isMobile } = useDevicesSize();
const layoutMode = computed(() => {
  let info = {
    mobile: {
      width: "95%",
      layout: "vertical"
    },
    desktop: {
      width: "60%",
      layout: "horizontal"
    }
  };
  return isMobile.value ? info.mobile : info.desktop;
});

const proxy = useGlobalProperties();

// 表单数据
const form = ref({
  keyword: ""
});

// 表格相关
const pluginsList = ref<PluginExport[]>([]);
const loading = ref(false);

// 详情弹窗
const detailVisible = ref(false);
const currentPlugin = ref<PluginExport>({} as PluginExport);

// 导出时是否包含数据库数据
const exportIncludeData = ref(true);

// 导入弹窗
const importModalVisible = ref(false);

// 过滤后的插件列表
const filteredPlugins = computed(() => {
  if (!form.value.keyword) {
    return pluginsList.value;
  }
  const keyword = form.value.keyword.toLowerCase();
  return pluginsList.value.filter(
    plugin => plugin.name.toLowerCase().includes(keyword) || plugin.author.toLowerCase().includes(keyword)
  );
});
const emptyDescription = computed(() => (form.value.keyword ? "暂无匹配插件" : "暂无插件数据"));

// 获取插件列表
const getPluginsList = async () => {
  try {
    loading.value = true;
    const res = await getPluginsExportAPI();
    pluginsList.value = res.data.list || [];
  } catch (error) {
    console.error(error);
    proxy.$message.error("获取插件列表失败");
  } finally {
    loading.value = false;
  }
};

// 导出插件
const exportPlugin = async (plugin: PluginExport) => {
  try {
    proxy.$message.loading("插件导出中...");
    const response = await exportPluginAPI(plugin.folderName, exportIncludeData.value);

    // 获取blob并下载
    const blob = new Blob([response], { type: "application/zip" });
    const url = window.URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `${plugin.folderName}_${plugin.version}.zip`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    window.URL.revokeObjectURL(url);

    proxy.$message.success("插件导出成功");
  } catch (error) {
    console.error(error);
  }
};

// 查询
const search = () => {
  // 本地过滤，无需重新请求
};

// 重置
const reset = () => {
  form.value = {
    keyword: ""
  };
};

// 查看详情
const viewDetail = (plugin: PluginExport) => {
  currentPlugin.value = { ...plugin };
  detailVisible.value = true;
};

// 显示导入弹窗
const showImportModal = () => {
  importModalVisible.value = true;
};

// 导入成功回调
const handleImportSuccess = async () => {
  // 刷新插件列表
  await getPluginsList();
};

// 删除插件
const handleDeletePlugin = async () => {
  try {
    proxy.$message.loading("插件卸载中...");
    await deletePluginAPI(currentPlugin.value.folderName);
    proxy.$message.success("插件卸载成功");
    detailVisible.value = false;
    // 刷新插件列表
    await getPluginsList();
  } catch (error) {
    console.error(error);
    proxy.$message.error("插件卸载失败");
  }
};

// 初始化
onMounted(() => {
  getPluginsList();
});
</script>

<style lang="scss" scoped>
.plugin-grid {
  padding: 16px 0 0;
}

.plugin-title {
  font-weight: 600;
  font-size: 14px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.plugin-card {
  width: 100%;
  cursor: pointer;
  transition:
    transform 0.22s ease,
    box-shadow 0.22s ease,
    border-color 0.22s ease;
}

.plugin-card:hover {
  transform: translateY(-2px);
}

.plugin-cover {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  height: 164px;
  color: var(--uvp-brand-strong);
  background: linear-gradient(180deg, #f9fbff 0%, #eef5ff 100%);
}

.plugin-cover__icon {
  display: inline-grid;
  width: 56px;
  height: 56px;
  font-size: 28px;
  place-items: center;
  background: rgb(37 99 235 / 8%);
  border: 1px solid rgb(37 99 235 / 12%);
  border-radius: 18px;
}

.plugin-cover__version {
  position: absolute;
  top: 14px;
  right: 14px;
  padding: 4px 8px;
  font-size: 12px;
  font-weight: 600;
  line-height: 1;
  color: var(--uvp-text-secondary);
  background: rgb(255 255 255 / 86%);
  border: 1px solid rgb(214 226 240 / 92%);
  border-radius: 999px;
}

.plugin-dialog-actions {
  margin-top: 20px;
}

.plugin-dialog-actions__option {
  text-align: right;
}

.plugin-dependencies {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.plugin-dependency {
  color: var(--uvp-text-secondary);
}
</style>
