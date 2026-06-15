<template>
  <div class="select-user-container">
    <!-- 已选用户标签组 -->
    <div class="selected-tags-wrapper">
      <a-space v-if="selectedUsers.length > 0" wrap :size="8">
        <a-tooltip v-for="user in selectedUsers" :key="user.id" :content="user.userName">
          <a-tag
            closable
            :color="multiple ? 'arcoblue' : 'green'"
            @close="handleRemoveUser(user.id)"
          >
            {{ truncateText(user.userName, maxLabelLength) }}
          </a-tag>
        </a-tooltip>
      </a-space>
      <span v-else class="placeholder-text">{{ placeholder }}</span>
    </div>

    <!-- 操作按钮组 -->
    <div class="action-buttons">
      <a-button size="small" :disabled="disabled" @click="openModal">
        <template #icon>
          <icon-user />
        </template>
        <span>选择用户</span>
      </a-button>
      <a-button
        v-if="selectedUsers.length > 0"
        size="small"
        :disabled="disabled"
        @click="handleClear"
      >
        <template #icon>
          <icon-delete />
        </template>
        <span>清空</span>
      </a-button>
    </div>

    <!-- 用户选择弹窗 -->
    <a-modal
      v-model:visible="modalVisible"
      title="选择用户"
      :width="800"
      :footer="false"
      :unmount-on-close="true"
      @cancel="handleModalCancel"
    >
      <!-- 搜索区域 -->
      <div class="search-area">
        <a-input
          v-model="searchKeyword"
          placeholder="请输入用户名称或昵称搜索"
          allow-clear
          @input="handleSearch"
        >
          <template #prefix>
            <icon-search />
          </template>
        </a-input>
      </div>

      <!-- 用户列表表格 -->
      <div class="table-area">
        <a-table
          row-key="id"
          :data="userList"
          :loading="loading"
          :pagination="false"
          :bordered="{ cell: true }"
          :row-class-name="getRowClassName"
          @row-click="handleRowClick"
        >
          <template #columns>
            <a-table-column title="ID" data-index="id" :width="70" align="center" />
            <a-table-column title="用户名称" data-index="userName" :width="120" />
            <a-table-column title="昵称" data-index="nickName" :width="120" />
            <a-table-column title="部门" :width="150">
              <template #cell="{ record }">
                {{ record.department ? record.department.name : '-' }}
              </template>
            </a-table-column>
            <a-table-column title="手机号" data-index="phone" :width="130" />
            <a-table-column title="状态" :width="80" align="center">
              <template #cell="{ record }">
                <a-tag
                  bordered
                  size="small"
                  color="arcoblue"
                  v-if="record.status === 1"
                >
                  启用
                </a-tag>
                <a-tag bordered size="small" color="red" v-else>
                  禁用
                </a-tag>
              </template>
            </a-table-column>
            <a-table-column title="操作" :width="80" align="center">
              <template #cell="{ record }">
                <a-tag
                  v-if="isUserSelected(record.id)"
                  color="green"
                  bordered
                >
                  已选
                </a-tag>
                <a-tag v-else color="arcoblue" bordered>
                  选择
                </a-tag>
              </template>
            </a-table-column>
          </template>
        </a-table>
      </div>

      <!-- 分页区域 -->
      <div class="pagination-area">
        <a-pagination
          v-model:current="pagination.current"
          v-model:page-size="pagination.pageSize"
          :total="pagination.total"
          :page-size-options="['10', '20', '50', '100']"
          show-total
          show-jumper
          show-page-size
          @change="handlePageChange"
          @page-size-change="handlePageSizeChange"
        />
      </div>

      <!-- 底部按钮 -->
      <div class="modal-footer">
        <a-space>
          <a-button @click="handleModalCancel">取消</a-button>
          <a-button type="primary" @click="handleModalConfirm">确定</a-button>
        </a-space>
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref,  watch } from 'vue';
import { getAccountListAPI, getAccountDetailAPI } from '@/api/user';
import type { AccountItem } from '@/api/user';

// 用户信息接口（用于已选用户）
interface UserInfo {
  id: number;
  userName: string;
  nickName: string;
}

// Props 定义
interface Props {
  /** 绑定值：单选时为数值，多选时为逗号分隔的字符串 */
  modelValue: number | string | undefined;
  /** 是否多选模式 */
  multiple?: boolean;
  /** 是否禁用 */
  disabled?: boolean;
  /** 占位符文本 */
  placeholder?: string;
  /** 标签文本最大长度，超出显示省略号 */
  maxLabelLength?: number;
}

// Emits 定义
interface Emits {
  (e: 'update:modelValue', value: number | string | undefined): void;
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: undefined,
  multiple: false,
  disabled: false,
  placeholder: '请选择用户',
  maxLabelLength: 10
});

const emit = defineEmits<Emits>();

// 弹窗显示状态
const modalVisible = ref(false);

// 搜索关键字
const searchKeyword = ref('');

// 加载状态
const loading = ref(false);

// 用户列表数据
const userList = ref<AccountItem[]>([]);

// 已选用户列表（用于显示）
const selectedUsers = ref<UserInfo[]>([]);

// 临时选中用户ID集合（弹窗中使用）
const tempSelectedIds = ref<Set<number>>(new Set());

// 分页配置
const pagination = ref({
  current: 1,
  pageSize: 10,
  total: 0
});

// 计算属性：判断用户是否已选中
const isUserSelected = (userId: number): boolean => {
  return tempSelectedIds.value.has(userId);
};

// 计算属性：获取表格行样式
const getRowClassName = (record: AccountItem): string => {
  return isUserSelected(record.id) ? 'selected-row' : '';
};

// 截断文本函数
const truncateText = (text: string, maxLength: number): string => {
  if (!text || text.length <= maxLength) {
    return text;
  }
  return text.substring(0, maxLength) + '...';
};

// 监听 modelValue 变化，同步更新 selectedUsers
watch(
  () => props.modelValue,
  (newValue) => {
    // 处理 undefined 或 null 或空值的情况
    if (newValue === undefined || newValue === null || newValue === '') {
      selectedUsers.value = [];
      return;
    }

    if (props.multiple) {
      // 多选模式：从逗号分隔的字符串解析用户ID
      const ids = String(newValue)
        .split(',')
        .map((id) => parseInt(id.trim()))
        .filter((id) => !isNaN(id));
      syncSelectedUsers(ids);
    } else {
      // 单选模式：直接使用数值
      const id = Number(newValue);
      syncSelectedUsers(id > 0 ? [id] : []);
    }
  },
  { immediate: true }
);

// 同步已选用户信息
const syncSelectedUsers = async (userIds: number[]) => {
  if (userIds.length === 0) {
    selectedUsers.value = [];
    return;
  }

  try {
    // 获取用户详情信息
    const promises = userIds.map((id) => getUserInfo(id));
    const results = await Promise.allSettled(promises);
    const users: UserInfo[] = results
      .filter((r) => r.status === 'fulfilled' && r.value)
      .map((r) => (r as PromiseFulfilledResult<UserInfo>).value);
    selectedUsers.value = users;
  } catch (error) {
    console.error('获取用户信息失败:', error);
  }
};

// 获取单个用户信息
const getUserInfo = async (userId: number): Promise<UserInfo | null> => {
  try {
    const { data } = await getAccountDetailAPI(userId);
    return {
      id: data.id,
      userName: data.userName,
      nickName: data.nickName
    };
  } catch (error) {
    console.error(`获取用户 ${userId} 信息失败:`, error);
    return null;
  }
};

// 打开弹窗
const openModal = () => {
  modalVisible.value = true;
  // 初始化临时选中ID集合
  if (props.modelValue === undefined || props.modelValue === null || props.modelValue === '') {
    tempSelectedIds.value = new Set();
  } else if (props.multiple) {
    const ids = String(props.modelValue)
      .split(',')
      .map((id) => parseInt(id.trim()))
      .filter((id) => !isNaN(id));
    tempSelectedIds.value = new Set(ids);
  } else {
    const id = Number(props.modelValue);
    tempSelectedIds.value = id > 0 ? new Set([id]) : new Set();
  }
  // 重置搜索和分页
  searchKeyword.value = '';
  pagination.value.current = 1;
  // 加载用户列表
  loadUserList();
};

// 加载用户列表
const loadUserList = async () => {
  loading.value = true;
  try {
    const params: any = {
      pageNum: pagination.value.current,
      pageSize: pagination.value.pageSize,
      order: 'id desc'
    };

    // 添加搜索条件
    if (searchKeyword.value) {
      params.name = searchKeyword.value;
    }

    const { data } = await getAccountListAPI(params);
    userList.value = data.list;
    pagination.value.total = data.total;
  } catch (error) {
    console.error('加载用户列表失败:', error);
  } finally {
    loading.value = false;
  }
};

// 搜索处理
const handleSearch = () => {
  pagination.value.current = 1;
  loadUserList();
};

// 分页变化
const handlePageChange = (page: number) => {
  pagination.value.current = page;
  loadUserList();
};

// 每页数量变化
const handlePageSizeChange = (pageSize: number) => {
  pagination.value.pageSize = pageSize;
  pagination.value.current = 1;
  loadUserList();
};

// 行点击处理
const handleRowClick = (record: AccountItem) => {
  if (props.multiple) {
    // 多选模式：切换选中状态
    if (tempSelectedIds.value.has(record.id)) {
      tempSelectedIds.value.delete(record.id);
    } else {
      tempSelectedIds.value.add(record.id);
    }
  } else {
    // 单选模式：直接替换
    tempSelectedIds.value = new Set([record.id]);
  }
};

// 移除用户
const handleRemoveUser = (userId: number) => {
  if (props.disabled) return;

  if (props.multiple) {
    const currentValue = props.modelValue ?? '';
    const ids = String(currentValue)
      .split(',')
      .map((id) => parseInt(id.trim()))
      .filter((id) => !isNaN(id) && id !== userId);
    emit('update:modelValue', ids.join(','));
  } else {
    emit('update:modelValue', 0);
  }
};

// 清空所有选中
const handleClear = () => {
  if (props.disabled) return;

  if (props.multiple) {
    emit('update:modelValue', '');
  } else {
    emit('update:modelValue', 0);
  }
};

// 弹窗取消
const handleModalCancel = () => {
  modalVisible.value = false;
};

// 弹窗确定
const handleModalConfirm = () => {
  if (props.multiple) {
    // 多选模式：输出逗号分隔的字符串，空时返回空字符串
    const ids = Array.from(tempSelectedIds.value).join(',');
    emit('update:modelValue', ids || '');
  } else {
    // 单选模式：输出数值，空时返回 0
    const ids = Array.from(tempSelectedIds.value);
    const id = ids.length > 0 ? ids[0] : 0;
    emit('update:modelValue', id);
  }
  modalVisible.value = false;
};
</script>

<style lang="scss" scoped>
.select-user-container {
  display: flex;
  flex-direction: column;
  gap: 8px;

  .selected-tags-wrapper {
    min-height: 32px;
    padding: 4px 8px;
    border: 1px solid var(--color-border-2);
    border-radius: 4px;
    background-color: var(--color-bg-2);
    display: flex;
    align-items: center;
    transition: all 0.2s;

    &:hover {
      border-color: var(--color-border-3);
    }

    .placeholder-text {
      color: var(--color-text-3);
      font-size: 14px;
    }
  }

  .action-buttons {
    display: flex;
    gap: 8px;
  }
}

// 弹窗样式
.search-area {
  margin-bottom: 16px;
}

.table-area {
  margin-bottom: 16px;
  max-height: 400px;
  overflow-y: auto;

  :deep(.arco-table) {
    .arco-table-body {
      max-height: 350px;
      overflow-y: auto;
    }

    // 已选行高亮样式
    .selected-row {
      background-color: var(--color-primary-light-1);
      cursor: pointer;

      &:hover {
        background-color: var(--color-primary-light-2);
      }
    }

    // 未选中行样式
    .arco-table-tr {
      cursor: pointer;

      &:hover {
        background-color: var(--color-fill-2);
      }
    }
  }
}

.pagination-area {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 16px;
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  padding-top: 16px;
  border-top: 1px solid var(--color-border-1);
}
</style>
