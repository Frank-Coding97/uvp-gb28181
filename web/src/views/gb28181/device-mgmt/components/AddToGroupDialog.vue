<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { FolderPlus } from "@lucide/vue";
import { addDevicesToGroup, listDirectoryTree, type DirectoryNode } from "../api";

const props = defineProps<{
    visible: boolean;
    deviceIds: number[];
}>();

const emit = defineEmits<{
    "update:visible": [visible: boolean];
    saved: [result: { groupId: number; requestedCount: number; addedCount: number; skippedCount: number }];
}>();

interface GroupOption {
    id: number;
    name: string;
    depth: number;
}

const loading = ref(false);
const submitting = ref(false);
const groupId = ref(0);
const groups = ref<GroupOption[]>([]);
const errorMessage = ref("");

const canSubmit = computed(() => groupId.value > 0 && props.deviceIds.length > 0 && !submitting.value);

function flattenGroups(nodes: DirectoryNode[]): GroupOption[] {
    return nodes.flatMap((node) => {
        const match = node.key.match(/^custom:group:([1-9]\d*)$/);
        const own = node.type === "group" && !node.readOnly && match
            ? [{ id: Number(match[1]), name: node.name, depth: node.depth }]
            : [];
        return [...own, ...flattenGroups(node.children || [])];
    });
}

async function loadGroups() {
    loading.value = true;
    errorMessage.value = "";
    try {
        const response = await listDirectoryTree("custom");
        if (response.code !== 0) throw new Error(response.message || "分组加载失败");
        groups.value = flattenGroups(response.data?.list || []);
    } catch (error: any) {
        groups.value = [];
        errorMessage.value = error?.message || "分组加载失败";
    } finally {
        loading.value = false;
    }
}

watch(() => props.visible, (visible) => {
    if (!visible) return;
    groupId.value = 0;
    errorMessage.value = "";
    loadGroups();
}, { immediate: true });

function close() {
    if (!submitting.value) emit("update:visible", false);
}

async function submit() {
    if (!canSubmit.value) return;
    submitting.value = true;
    errorMessage.value = "";
    try {
        const response = await addDevicesToGroup(groupId.value, props.deviceIds);
        if (response.code !== 0) throw new Error(response.message || "添加到分组失败");
        emit("saved", { groupId: groupId.value, ...response.data });
        emit("update:visible", false);
    } catch (error: any) {
        errorMessage.value = error?.response?.data?.message || error?.message || "添加到分组失败";
    } finally {
        submitting.value = false;
    }
}
</script>

<template>
    <a-modal
        :visible="visible"
        modal-class="uvp-system-dialog add-to-group-dialog"
        title="添加到分组"
        :width="460"
        :mask-closable="!submitting"
        :closable="!submitting"
        unmount-on-close
        @cancel="close"
        @update:visible="emit('update:visible', $event)"
    >
        <div class="dialog-body">
            <p>已选择 {{ deviceIds.length }} 台设备</p>
            <label for="add-to-custom-group">目标分组</label>
            <select id="add-to-custom-group" v-model.number="groupId" data-testid="group-target" :disabled="loading || submitting">
                <option :value="0" disabled>{{ loading ? '正在加载分组...' : '请选择分组' }}</option>
                <option v-for="group in groups" :key="group.id" :value="group.id">
                    {{ `${'　'.repeat(group.depth)}${group.name}` }}
                </option>
            </select>
            <a-alert v-if="errorMessage" type="error">{{ errorMessage }}</a-alert>
            <span v-else-if="!loading && groups.length === 0" class="empty-hint">暂无可用分组，请先在左侧新建自定义分组。</span>
        </div>
        <template #footer>
            <div class="dialog-footer">
                <a-button :disabled="submitting" @click="close">取消</a-button>
                <a-button
                    data-testid="add-to-group-submit"
                    type="primary"
                    :disabled="!canSubmit"
                    :loading="submitting"
                    @click="submit"
                >
                    <template #icon><FolderPlus :size="14" /></template>
                    添加
                </a-button>
            </div>
        </template>
    </a-modal>
</template>

<style scoped>
.dialog-body { display: grid; gap: 10px; }
.dialog-body p { margin: 0; color: var(--uvp-text-tertiary); font-size: 12px; }
.dialog-body label { color: var(--uvp-text-primary); font-size: 13px; font-weight: 620; }
.dialog-body select {
    width: 100%;
    height: 36px;
    padding: 0 10px;
    color: var(--uvp-text-primary);
    background: var(--uvp-page-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 6px;
}
.dialog-body select:focus { outline: 1px solid var(--uvp-brand); border-color: var(--uvp-brand); }
.empty-hint { color: var(--uvp-text-tertiary); font-size: 12px; }
.dialog-footer { display: flex; justify-content: flex-end; gap: 8px; }
</style>
