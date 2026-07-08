<template>
  <a-drawer
    class="uvp-system-drawer"
    body-class="uvp-system-dialog__body"
    :visible="visible"
    :width="layoutMode.width"
    :hide-cancel="true"
    ok-text="关闭"
    @ok="handleCancel"
    @cancel="handleCancel"
    :title="title"
    :ok-loading="loading"
  >
    <div class="tenant-user-container">
      <s-layout-search>
        <template #fields>
          <a-input
            v-model="searchKeyword"
            placeholder="请输入账号或昵称"
            :style="{ width: '220px' }"
            allow-clear
            @press-enter="handleSearch"
          />
        </template>
        <template #actions>
          <a-button type="primary" @click="handleSearch">
            <template #icon><icon-search /></template>
            <span>查询</span>
          </a-button>
          <a-button @click="handleResetSearch" :disabled="!searchKeyword">
            <template #icon><icon-refresh /></template>
            <span>重置</span>
          </a-button>
        </template>
        <template #extra>
          <a-button type="primary" @click="showAddUserModal">
            <template #icon><icon-plus /></template>
            <span>添加其他租户用户</span>
          </a-button>
        </template>
      </s-layout-search>

      <a-table
        class="uvp-data-table"
        row-key="userID"
        :data="tenantUserList"
        :loading="tableLoading"
        :pagination="pagination"
        @page-change="handlePageChange"
        @page-size-change="handlePageSizeChange"
        size="small"
        :bordered="false"
        :scroll="tableScroll"
      >
        <template #columns>
          <a-table-column title="用户名" :width="150" ellipsis tooltip>
            <template #cell="{ record }">
              {{ record.user?.userName }}
            </template>
          </a-table-column>
          <a-table-column title="昵称" :width="150" ellipsis tooltip>
            <template #cell="{ record }">
              {{ record.user?.nickName }}
            </template>
          </a-table-column>
          <a-table-column title="本地" :width="50">
            <template #cell="{ record }">
              {{ record.isDefault ? "是" : "否" }}
            </template>
          </a-table-column>
          <a-table-column title="租户" :width="120" ellipsis tooltip>
            <template #cell="{ record }">
              {{ record.user?.tenant?.name }}
            </template>
          </a-table-column>
          <a-table-column title="操作" :width="124" align="center" :fixed="isMobile ? '' : 'right'">
            <template #cell="{ record }">
              <div v-if="!record.isDefault" class="uvp-table-actions">
                <a-popconfirm content="确定要移除该用户吗?" @ok="removeUser(record)">
                  <a-link class="uvp-table-action uvp-table-action--delete">移除</a-link>
                </a-popconfirm>
                <a-link class="uvp-table-action uvp-table-action--assign" @click="showRoleModal(record)"> 分配角色 </a-link>
              </div>
            </template>
          </a-table-column>
        </template>
      </a-table>
    </div>
  </a-drawer>

  <s-add-user-modal
    :width="layoutMode.width"
    v-model:visible="addUserModalVisible"
    :tenant-id="tenantId"
    @success="handleAddUserSuccess"
  />

  <a-modal
    modal-class="uvp-system-dialog"
    :visible="roleModalVisible"
    :title="`为用户 ${currentUserInfo.userName} 分配角色`"
    :width="layoutMode.width"
    @ok="handleRoleSubmit"
    @cancel="handleRoleCancel"
    :ok-loading="roleModalLoading"
  >
    <a-form class="uvp-system-form" :model="roleForm" :layout="layoutMode.layout" auto-label-width>
      <a-form-item label="用户"> {{ currentUserInfo.userName }} ({{ currentUserInfo.nickName }}) </a-form-item>
      <a-form-item label="角色">
        <a-tree-select
          v-model="roleForm.roles"
          :data="roleList"
          :field-names="{ key: 'id', title: 'name', children: 'children' }"
          multiple
          placeholder="请选择角色"
          :allow-clear="true"
          :tree-checkable="true"
          tree-checked-strategy="all"
          :style="{ width: '100%' }"
        />
      </a-form-item>
    </a-form>
  </a-modal>
</template>

<script setup lang="ts">
import {
  getSysUserTenantList,
  batchDeleteSysUserTenant,
  batchAddSysUserTenant,
  getRolesAllAPI,
  getUserRoleIDs,
  setUserRoles,
  type SysUserTenantListParam,
  type SysUserTenant
} from "@/api/sysusertenant";
import SAddUserModal from "@/components/s-add-user-modal/index.vue";
import type { RoleItem } from "@/api/role";
import { useDevicesSize } from "@/hooks/useDevicesSize";

const { isMobile } = useDevicesSize();
const layoutMode = computed(() => {
  const info = {
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

interface Props {
  visible: boolean;
  tenantId?: number;
  tenantName?: string;
}

interface Emits {
  (e: "update:visible", value: boolean): void;
  (e: "success"): void;
}

const props = withDefaults(defineProps<Props>(), {
  visible: false,
  tenantId: 0,
  tenantName: ""
});

const emit = defineEmits<Emits>();

const title = computed(() => (props.tenantName ? `用户分配 - ${props.tenantName}` : "用户分配"));

const loading = ref(false);
const tableLoading = ref(false);
const searchKeyword = ref("");
const tenantUserList = ref<SysUserTenant[]>([]);
const tableScroll = computed(() => ({
  x: "100%",
  ...(tenantUserList.value.length > 0 ? { y: 400 } : {})
}));
const pagination = ref({
  current: 1,
  pageSize: 10,
  total: 0,
  showPageSize: true,
  showTotal: true,
  pageSizeOptions: ["10", "20", "50", "100"]
});

const addUserModalVisible = ref(false);

const showAddUserModal = () => {
  addUserModalVisible.value = true;
};

const handleAddUserSuccess = async (selectedUserIds: number[]) => {
  if (!props.tenantId || selectedUserIds.length === 0) {
    arcoMessage("warning", "请选择要添加的用户");
    return;
  }

  try {
    await batchAddSysUserTenant({
      userIDs: selectedUserIds,
      tenantID: props.tenantId
    });
    arcoMessage("success", "用户添加成功");
    addUserModalVisible.value = false;
    pagination.value.current = 1;
    loadTenantUserList();
    emit("success");
  } catch (error) {
    console.error("添加用户失败:", error);
    arcoMessage("error", "添加用户失败");
  }
};

const loadTenantUserList = async () => {
  if (!props.tenantId) return;

  try {
    tableLoading.value = true;
    const params: SysUserTenantListParam = {
      tenantID: props.tenantId,
      pageNum: pagination.value.current,
      pageSize: pagination.value.pageSize
    };

    if (searchKeyword.value.trim()) {
      params.key = searchKeyword.value.trim();
    }

    const res = await getSysUserTenantList(params);
    tenantUserList.value = res.data.list;
    pagination.value.total = res.data.total;
  } catch (error) {
    console.error("获取租户用户列表失败:", error);
    arcoMessage("error", "获取租户用户列表失败");
  } finally {
    tableLoading.value = false;
  }
};

const removeUser = async (record: SysUserTenant) => {
  if (!props.tenantId) return;

  try {
    await batchDeleteSysUserTenant({
      userIDs: [record.userID],
      tenantID: props.tenantId
    });
    arcoMessage("success", "用户移除成功");
    loadTenantUserList();
  } catch (error) {
    console.error("移除用户失败:", error);
    arcoMessage("error", "移除用户失败");
  }
};

const handlePageChange = (page: number) => {
  pagination.value.current = page;
  loadTenantUserList();
};

const handlePageSizeChange = (pageSize: number) => {
  pagination.value.pageSize = pageSize;
  pagination.value.current = 1;
  loadTenantUserList();
};

const handleSearch = async () => {
  pagination.value.current = 1;
  await loadTenantUserList();
};

const handleResetSearch = async () => {
  searchKeyword.value = "";
  pagination.value.current = 1;
  await loadTenantUserList();
};

const handleCancel = () => {
  emit("update:visible", false);
};

watch(
  () => props.visible,
  newVal => {
    if (newVal) {
      pagination.value.current = 1;
      searchKeyword.value = "";
      loadTenantUserList();
    }
  }
);

watch(
  () => searchKeyword.value,
  (newVal, oldVal) => {
    if (oldVal && oldVal.trim() && !newVal.trim()) {
      pagination.value.current = 1;
      loadTenantUserList();
    }
  }
);

const roleModalVisible = ref(false);
const roleModalLoading = ref(false);
const roleList = ref<RoleItem[]>([]);
const currentUserInfo = ref({
  userID: 0,
  userName: "",
  nickName: ""
});
const roleForm = ref({
  roles: [] as number[]
});

const showRoleModal = async (record: SysUserTenant) => {
  currentUserInfo.value = {
    userID: record.userID,
    userName: record.user?.userName || "",
    nickName: record.user?.nickName || ""
  };
  await loadRoleList();
  await loadUserRoles(record.userID);
  roleModalVisible.value = true;
};

const loadRoleList = async () => {
  if (!props.tenantId) return;

  try {
    const res = await getRolesAllAPI({ tenantID: props.tenantId });
    roleList.value = res.data.list;
  } catch (error) {
    console.error("获取角色列表失败:", error);
    arcoMessage("error", "获取角色列表失败");
  }
};

const loadUserRoles = async (userId: number) => {
  if (!props.tenantId) return;

  try {
    const res = await getUserRoleIDs({
      userID: userId,
      tenantID: props.tenantId
    });
    roleForm.value.roles = res.data || [];
  } catch (error) {
    console.error("获取用户角色失败:", error);
    arcoMessage("error", "获取用户角色失败");
  }
};

const handleRoleSubmit = async () => {
  if (!props.tenantId) return;

  try {
    roleModalLoading.value = true;
    await setUserRoles({
      userID: currentUserInfo.value.userID,
      roles: roleForm.value.roles,
      tenantID: props.tenantId
    });
    arcoMessage("success", "角色分配成功");
    roleModalVisible.value = false;
  } catch (error) {
    console.error("角色分配失败:", error);
    arcoMessage("error", "角色分配失败");
  } finally {
    roleModalLoading.value = false;
  }
};

const handleRoleCancel = () => {
  roleModalVisible.value = false;
  roleForm.value.roles = [];
  currentUserInfo.value = {
    userID: 0,
    userName: "",
    nickName: ""
  };
};
</script>

<style lang="scss" scoped>
.tenant-user-container {
  display: flex;
  flex-direction: column;
  gap: 12px;

  :deep(.arco-input-wrapper) {
    min-width: 180px;
  }
}
</style>
