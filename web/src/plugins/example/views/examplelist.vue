<template>
  <div class="snow-page">
    <div class="snow-inner uvp-page-shell-flat">
      <s-layout-search>
        <template #fields>
          <a-input
            v-model="searchForm.name"
            placeholder="请输入名称"
            style="width: 220px"
            allow-clear
            @press-enter="handleSearch"
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
        <template #extra>
          <a-button type="primary" @click="handleCreate" v-hasPerm="['plugins:example:add']">
            <template #icon><icon-plus /></template>
            <span>新增</span>
          </a-button>
        </template>
      </s-layout-search>

      <a-table
        class="uvp-data-table"
        row-key="id"
        :data="dataList"
        :loading="loading"
        :bordered="false"
        :pagination="paginationConfig"
        :scroll="tableScroll"
        @page-change="handlePageChange"
        @page-size-change="handlePageSizeChange"
      >
        <template #columns>
          <a-table-column title="名称" data-index="name" :width="180" ellipsis tooltip />
          <a-table-column title="描述" data-index="description" :width="260" ellipsis tooltip />
          <a-table-column title="操作" :width="112" align="center" :fixed="isMobile ? '' : 'right'">
            <template #cell="{ record }">
              <div class="uvp-table-actions">
                <a-link
                  class="uvp-table-action uvp-table-action--edit"
                  @click="handleEdit(record)"
                  v-hasPerm="['plugins:example:edit']"
                >
                  <template #icon><icon-edit /></template>
                  <span>编辑</span>
                </a-link>
                <a-popconfirm type="warning" content="确定要删除这条数据吗？" @ok="handleDelete(record.id)">
                  <a-link class="uvp-table-action uvp-table-action--delete" v-hasPerm="['plugins:example:delete']">
                    <template #icon><icon-delete /></template>
                    <span>删除</span>
                  </a-link>
                </a-popconfirm>
              </div>
            </template>
          </a-table-column>
        </template>
      </a-table>
    </div>

    <a-modal
      modal-class="uvp-system-dialog"
      v-model:visible="modalVisible"
      :width="layoutMode.width"
      @close="afterClose"
      @cancel="afterClose"
      :on-before-ok="handleSave"
    >
      <template #title>{{ isEditMode ? "编辑数据" : "新增数据" }}</template>
      <div>
        <a-form ref="formRef" :layout="layoutMode.layout" auto-label-width :model="editingData" :rules="rules">
          <a-form-item field="name" label="名称" validate-trigger="blur">
            <a-input v-model="editingData.name" placeholder="请输入名称" allow-clear />
          </a-form-item>
          <a-form-item field="description" label="描述" validate-trigger="blur">
            <a-textarea v-model="editingData.description" placeholder="请输入描述" allow-clear />
          </a-form-item>
        </a-form>
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { Message } from "@arco-design/web-vue";
import { computed, onMounted, reactive, ref } from "vue";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import type { ExampleData } from "../api/example";
import { useExamplePluginHook } from "../hooks/example";

const { isMobile } = useDevicesSize();
const {
  dataList,
  loading,
  total,
  currentPage,
  pageSize,
  fetchDataList,
  createData,
  updateData,
  deleteData,
  getDetail,
  resetSearchParams
} = useExamplePluginHook();

const layoutMode = computed(() => {
  const info = {
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

const modalVisible = ref(false);
const isEditMode = ref(false);
const formRef = ref();

const searchForm = reactive({
  name: ""
});

const editingData = reactive({
  id: 0,
  name: "",
  description: ""
});

const rules = {
  name: [{ required: true, message: "请输入名称" }],
  description: [{ required: true, message: "请输入描述" }]
};

const paginationConfig = computed(() => ({
  total: total.value,
  current: currentPage.value,
  pageSize: pageSize.value,
  showTotal: true,
  showJumper: true,
  showPageSize: true,
  pageSizeOptions: [10, 20, 30, 50]
}));

const tableScroll = computed(() => ({
  x: "100%",
  minWidth: 760,
  ...(dataList.value.length > 0 ? { y: "100%" } : {})
}));

const resetEditingData = () => {
  Object.assign(editingData, {
    id: 0,
    name: "",
    description: ""
  });
};

const loadData = async (pageNum: number = currentPage.value, pageSizeVal: number = pageSize.value) => {
  await fetchDataList({
    pageNum,
    pageSize: pageSizeVal,
    name: searchForm.name || undefined
  });
};

const handlePageChange = (page: number) => {
  loadData(page, pageSize.value);
};

const handlePageSizeChange = (size: number) => {
  loadData(1, size);
};

const handleSearch = () => {
  loadData(1);
};

const handleReset = () => {
  searchForm.name = "";
  resetSearchParams();
  loadData(1);
};

const handleCreate = () => {
  resetEditingData();
  isEditMode.value = false;
  modalVisible.value = true;
};

const handleEdit = async (record: ExampleData) => {
  const detail = await getDetail(record.id);
  Object.assign(editingData, detail.data);
  isEditMode.value = true;
  modalVisible.value = true;
};

const handleDelete = async (id: number) => {
  try {
    await deleteData(id);
    Message.success("删除成功");
    await loadData();
  } catch (error) {
    console.error("删除失败:", error);
    Message.error("删除失败");
  }
};

const handleSave = async () => {
  const isValid = await formRef.value?.validate();
  if (isValid) return false;

  try {
    if (isEditMode.value) {
      await updateData(editingData);
      Message.success("修改成功");
    } else {
      await createData({
        name: editingData.name,
        description: editingData.description
      });
      Message.success("新增成功");
    }
    await loadData();
  } catch (error) {
    console.error("保存失败:", error);
    Message.error("保存失败");
    return false;
  }

  return true;
};

const afterClose = () => {
  formRef.value?.resetFields();
  resetEditingData();
  isEditMode.value = false;
  modalVisible.value = false;
};

onMounted(async () => {
  await loadData();
});
</script>
