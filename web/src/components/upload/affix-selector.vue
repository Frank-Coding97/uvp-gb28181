<template>
    <a-modal
        :width="800"
        :visible="props.visible"
        :title="title"
        :mask-closable="false"
        :footer="false"
        @update:visible="(val: boolean) => emit('update:visible', val)"
        @close="handleClose"
    >
        <div class="affix-selector">
            <!-- 搜索区域 -->
            <a-space wrap class="search-box" size="medium">
                <a-input v-model="form.name" placeholder="请输入图片名称" allow-clear />
                <a-button type="primary" @click="search">
                    <template #icon><icon-search /></template>
                    <span>查询</span>
                </a-button>
                <a-button @click="reset">
                    <template #icon><icon-refresh /></template>
                    <span>重置</span>
                </a-button>
            </a-space>

            <!-- 图片列表 -->
            <a-table
                row-key="id"
                :data="affixList"
                :bordered="{ cell: true }"
                :loading="loading"
                :scroll="{ x: '100%', y: 400 }"
                :pagination="pagination"
                :row-selection="rowSelection"
                v-model:selected-keys="selectedKeys"
                @page-change="handlePageChange"
                @page-size-change="handlePageSizeChange"
            >
                <template #columns>
                    <a-table-column title="ID" data-index="id" :width="70" align="center"></a-table-column>
                    <a-table-column title="图片名称" data-index="name" :ellipsis="true" tooltip
                        :width="200"></a-table-column>
                    <a-table-column title="图片预览" :width="120" align="center">
                        <template #cell="{ record }">
                            <div class="image-preview" @click="previewImage(record)">
                                <img :src="handleUrl(record.url)" alt="预览" />
                            </div>
                        </template>
                    </a-table-column>
                    <a-table-column title="文件大小" data-index="size" :width="120">
                        <template #cell="{ record }">{{ formatFileSize(record.size) }}</template>
                    </a-table-column>
                    <a-table-column title="创建时间" data-index="createdAt" :width="180">
                        <template #cell="{ record }">{{ formatTime(record.createdAt) }}</template>
                    </a-table-column>
                </template>
            </a-table>

            <!-- 底部操作按钮 -->
            <div class="footer-actions">
                <a-space>
                    <a-button @click="handleClose">取消</a-button>
                    <a-button type="primary" :disabled="selectedKeys.length === 0" @click="handleConfirm">
                        确认选择 ({{ selectedKeys.length }})
                    </a-button>
                </a-space>
            </div>

            <!-- 图片预览模态框 -->
            <a-modal
                v-model:visible="previewVisible"
                :footer="false"
                :width="600"
                title="图片预览"
            >
                <div style="text-align: center;">
                    <img :src="previewImageUrl" style="max-width: 100%; max-height: 400px;" />
                </div>
            </a-modal>
        </div>
    </a-modal>
</template>

<script setup lang="ts">
import { ref, reactive, watch } from 'vue';
import { getAffixListAPI } from "@/api/file";
import { Message } from "@arco-design/web-vue";
import { handleUrl } from "@/utils/app";
import { formatTime } from "@/globals";

// 定义组件属性
const props = defineProps({
    visible: {
        type: Boolean,
        default: false
    },
    multiple: {
        type: Boolean,
        default: false
    },
    title: {
        type: String,
        default: '选择图片'
    }
});

// 定义事件
const emit = defineEmits(['update:visible', 'confirm', 'close']);

// 搜索表单
const form = ref({
    name: "",
    ftype: "image" // 固定为图片类型
});

// 文件列表
const loading = ref(false);
const affixList = ref<any[]>([]);
const selectedKeys = ref<(string | number)[]>([]);

// 分页配置
const pagination = ref({
    current: 1,
    pageSize: 10,
    total: 0,
    showPageSize: true,
    showTotal: true,

});

// 图片预览
const previewVisible = ref(false);
const previewImageUrl = ref('');

// 行选择配置
const rowSelection = reactive({
    type: props.multiple ? 'checkbox' : 'radio',
    showCheckedAll: props.multiple,
    onlyCurrent: false,
});

// 搜索
const search = () => {
    pagination.value.current = 1;
    getAffixList();
};

// 重置
const reset = () => {
    form.value = {
        name: "",
        ftype: "image"
    };
    pagination.value.current = 1;
    getAffixList();
};

// 获取文件列表
const getAffixList = async () => {
    loading.value = true;
    const params = {
        pageNum: pagination.value.current,
        pageSize: pagination.value.pageSize,
        order: "id desc",
        ...form.value
    };

    try {
        const { data } = await getAffixListAPI(params);
        affixList.value = data.list;
        pagination.value.total = data.total;
    } catch (error) {
        // 错误已在http拦截器中处理
    } finally {
        loading.value = false;
    }
};

// 分页变化事件处理
const handlePageChange = (page: number) => {
    pagination.value.current = page;
    getAffixList();
};

const handlePageSizeChange = (pageSize: number) => {
    pagination.value.pageSize = pageSize;
    pagination.value.current = 1;
    getAffixList();
};

// 预览图片
const previewImage = (record: any) => {
    previewImageUrl.value = handleUrl(record.url);
    previewVisible.value = true;
};

// 格式化文件大小
const formatFileSize = (bytes: number): string => {
    if (bytes === 0) return "0 Bytes";
    const k = 1024;
    const sizes = ["Bytes", "KB", "MB", "GB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
};

// 确认选择
const handleConfirm = () => {
    if (selectedKeys.value.length === 0) {
        Message.warning('请选择至少一个图片');
        return;
    }

    // 根据选中的keys获取对应的项目
    const selectedItems = affixList.value.filter(item => selectedKeys.value.includes(item.id));
    
    // 提取URL数组
    const urls = selectedItems.map(item => item.url);
    
    // 根据单选/多选模式发送不同格式的数据
    if (props.multiple) {
        emit('confirm', urls);
    } else {
        emit('confirm', urls[0]);
    }
    
    handleClose();
};

// 关闭模态框
const handleClose = () => {
    selectedKeys.value = [];
    emit('update:visible', false);
    emit('close');
};

// 监听multiple变化，更新行选择配置
watch(() => props.multiple, (newVal) => {
    rowSelection.type = newVal ? 'checkbox' : 'radio';
    rowSelection.showCheckedAll = newVal;
    // 切换模式时清空选择
    selectedKeys.value = [];
});

// 监听visible变化
watch(() => props.visible, (newVal) => {
    if (newVal) {
        getAffixList();
    } else {
        selectedKeys.value = [];
    }
});
</script>

<style scoped>
.affix-selector {
    display: flex;
    flex-direction: column;
    height: 600px;
}

.search-box {

    background: var(--color-bg-2);
    border-radius: 4px;
}

.footer-actions {
    margin-top: 16px;
    padding-top: 16px;
    padding-bottom: 16px;
    border-top: 1px solid var(--color-border-2);
    text-align: right;
}

.image-preview {
    width: 80px;
    height: 60px;
    cursor: pointer;
    border-radius: 4px;
    overflow: hidden;
    border: 1px solid var(--color-border-2);
}

.image-preview img {
    width: 100%;
    height: 100%;
    object-fit: cover;
}

.image-preview:hover {
    border-color: var(--color-primary);
}
</style>