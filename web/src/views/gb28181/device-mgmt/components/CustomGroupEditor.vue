<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { AlertTriangle, FolderInput, FolderPlus, Pencil, Trash2 } from "@lucide/vue";
import {
    createCustomGroup,
    deleteCustomGroup,
    moveCustomGroup,
    renameCustomGroup,
    type CustomGroup,
    type DirectoryNode
} from "../api";

export type CustomGroupEditorMode = "create" | "rename" | "move" | "delete";

const props = defineProps<{
    visible: boolean;
    canManage: boolean;
    mode: CustomGroupEditorMode;
    node: DirectoryNode | null;
    tree: DirectoryNode[];
}>();

const emit = defineEmits<{
    "update:visible": [visible: boolean];
    saved: [result: {
        action: CustomGroupEditorMode;
        node: DirectoryNode | null;
        group?: CustomGroup;
        parentKey: string | null;
        removedDeviceCount?: number;
    }];
}>();

const name = ref("");
const targetParentId = ref(0);
const submitting = ref(false);
const errorMessage = ref("");

const currentId = computed(() => groupId(props.node));
const parentKey = computed(() => findParentKey(props.tree, props.node?.key || ""));
const title = computed(() => ({
    create: props.node ? `在“${props.node.name}”下新建分组` : "新建根分组",
    rename: `重命名“${props.node?.name || "分组"}”`,
    move: `移动“${props.node?.name || "分组"}”`,
    delete: `删除“${props.node?.name || "分组"}”`
}[props.mode]));
const submitText = computed(() => ({ create: "创建", rename: "保存", move: "移动", delete: "确认删除" }[props.mode]));

interface MoveTarget {
    id: number;
    name: string;
    depth: number;
}

function groupId(node: DirectoryNode | null | undefined): number {
    if (!node?.key.startsWith("custom:group:")) return 0;
    const value = Number(node.key.slice("custom:group:".length));
    return Number.isInteger(value) && value > 0 ? value : 0;
}

function descendantKeys(node: DirectoryNode): Set<string> {
    const keys = new Set<string>([node.key]);
    const visit = (children: DirectoryNode[]) => children.forEach((child) => {
        keys.add(child.key);
        visit(child.children || []);
    });
    visit(node.children || []);
    return keys;
}

function flattenGroups(nodes: DirectoryNode[], excluded: Set<string> = new Set()): MoveTarget[] {
    return nodes.flatMap((node) => {
        const own = node.type === "group" && !node.readOnly && !excluded.has(node.key)
            ? [{ id: groupId(node), name: node.name, depth: node.depth }]
            : [];
        return [...own, ...flattenGroups(node.children || [], excluded)];
    }).filter((item) => item.id > 0);
}

const moveTargets = computed(() => flattenGroups(props.tree, props.node ? descendantKeys(props.node) : new Set()));

function findParentKey(nodes: DirectoryNode[], targetKey: string, parent: string | null = null): string | null {
    for (const node of nodes) {
        if (node.key === targetKey) return parent;
        const found = findParentKey(node.children || [], targetKey, node.key);
        if (found !== null) return found;
    }
    return null;
}

function resetForm() {
    name.value = props.mode === "rename" ? props.node?.name || "" : "";
    targetParentId.value = parentKey.value ? groupId({ key: parentKey.value } as DirectoryNode) : 0;
    errorMessage.value = "";
    submitting.value = false;
}

watch(() => [props.visible, props.mode, props.node?.key] as const, ([visible]) => {
    if (visible) resetForm();
}, { immediate: true });

function errorCode(error: any): string {
    return error?.response?.data?.data?.errorCode || error?.data?.errorCode || "";
}

function mapError(error: any) {
    const code = errorCode(error);
    if (code === "GROUP_NAME_CONFLICT") return "同级分组名称已存在，请换一个名称";
    if (code === "GROUP_HAS_CHILDREN") return "该分组包含子分组，请先处理子分组";
    if (code === "GROUP_CYCLE") return "不能把分组移动到自身或后代";
    if (code === "GROUP_NOT_FOUND") return "分组不存在或已被删除，请刷新目录";
    return error?.response?.data?.message || error?.message || "分组操作失败，请重试";
}

function close() {
    if (!submitting.value) emit("update:visible", false);
}

async function submit() {
    if (!props.canManage || submitting.value) return;
    errorMessage.value = "";
    const normalizedName = name.value.trim();
    if ((props.mode === "create" || props.mode === "rename") && (normalizedName.length < 1 || normalizedName.length > 64)) {
        errorMessage.value = "请输入 1-64 个字符的分组名称";
        return;
    }
    if (!currentId.value && props.mode !== "create") {
        errorMessage.value = "分组不存在或已被删除，请刷新目录";
        return;
    }

    submitting.value = true;
    try {
        if (props.mode === "create") {
            const parentId = currentId.value || null;
            const response = await createCustomGroup({ name: normalizedName, parentId });
            if (response.code !== 0) throw response;
            emit("saved", { action: "create", node: props.node, group: response.data, parentKey: props.node?.key || null });
        } else if (props.mode === "rename") {
            const response = await renameCustomGroup(currentId.value, normalizedName);
            if (response.code !== 0) throw response;
            emit("saved", { action: "rename", node: props.node, parentKey: parentKey.value });
        } else if (props.mode === "move") {
            const response = await moveCustomGroup(currentId.value, targetParentId.value || null);
            if (response.code !== 0) throw response;
            emit("saved", { action: "move", node: props.node, parentKey: targetParentId.value ? `custom:group:${targetParentId.value}` : null });
        } else {
            const response = await deleteCustomGroup(currentId.value);
            if (response.code !== 0) throw response;
            emit("saved", {
                action: "delete",
                node: props.node,
                parentKey: parentKey.value,
                removedDeviceCount: response.data?.removedDeviceCount || 0
            });
        }
        emit("update:visible", false);
    } catch (error: any) {
        errorMessage.value = mapError(error);
    } finally {
        submitting.value = false;
    }
}
</script>

<template>
    <a-modal
        v-if="canManage"
        :visible="visible"
        modal-class="uvp-system-dialog custom-group-editor"
        :title="title"
        :width="460"
        :mask-closable="!submitting"
        :closable="!submitting"
        unmount-on-close
        @cancel="close"
        @update:visible="emit('update:visible', $event)"
    >
        <a-form v-if="mode === 'create' || mode === 'rename'" layout="vertical">
            <a-form-item label="分组名称" :validate-status="errorMessage ? 'error' : undefined" :help="errorMessage || undefined">
                <a-input
                    v-model="name"
                    data-testid="group-name"
                    :max-length="64"
                    show-word-limit
                    allow-clear
                    placeholder="请输入分组名称"
                    @press-enter="submit"
                />
            </a-form-item>
        </a-form>

        <div v-else-if="mode === 'move'" class="editor-body">
            <label for="custom-group-move-target">目标父分组</label>
            <select id="custom-group-move-target" v-model.number="targetParentId" data-testid="move-target">
                <option :value="0">自定义分组根目录</option>
                <option v-for="target in moveTargets" :key="target.id" :value="target.id">
                    {{ `${'　'.repeat(target.depth)}${target.name}` }}
                </option>
            </select>
            <p>移动后设备成员关系保持不变。</p>
        </div>

        <div v-else class="delete-warning">
            <AlertTriangle :size="20" />
            <div>
                <strong>删除后无法恢复</strong>
                <p>只删除当前分组及其成员关系，<b>不会删除设备</b>。如果分组包含子分组，请先移动或删除子分组。</p>
            </div>
        </div>

        <a-alert v-if="errorMessage && mode !== 'create' && mode !== 'rename'" type="error" class="editor-error">{{ errorMessage }}</a-alert>

        <template #footer>
            <div class="editor-footer">
                <a-button :disabled="submitting" @click="close">取消</a-button>
                <a-button
                    data-testid="group-submit"
                    :type="mode === 'delete' ? 'primary' : 'primary'"
                    :status="mode === 'delete' ? 'danger' : undefined"
                    :loading="submitting"
                    @click="submit"
                >
                    <template #icon>
                        <FolderPlus v-if="mode === 'create'" :size="14" />
                        <Pencil v-else-if="mode === 'rename'" :size="14" />
                        <FolderInput v-else-if="mode === 'move'" :size="14" />
                        <Trash2 v-else :size="14" />
                    </template>
                    {{ submitText }}
                </a-button>
            </div>
        </template>
    </a-modal>
</template>

<style scoped>
.editor-body { display: grid; gap: 8px; }
.editor-body label { color: var(--uvp-text-primary); font-size: 13px; font-weight: 620; }
.editor-body select {
    width: 100%;
    height: 36px;
    padding: 0 10px;
    color: var(--uvp-text-primary);
    background: var(--uvp-page-bg);
    border: 1px solid var(--uvp-panel-border);
    border-radius: 6px;
}
.editor-body select:focus { outline: 1px solid var(--uvp-brand); border-color: var(--uvp-brand); }
.editor-body p,
.delete-warning p { margin: 0; color: var(--uvp-text-tertiary); font-size: 12px; line-height: 1.6; }
.delete-warning { display: flex; align-items: flex-start; gap: 12px; color: var(--uvp-danger); }
.delete-warning > svg { flex: 0 0 auto; margin-top: 2px; }
.delete-warning strong { display: block; margin-bottom: 5px; color: var(--uvp-text-primary); }
.delete-warning b { color: var(--uvp-text-primary); }
.editor-error { margin-top: 14px; }
.editor-footer { display: flex; justify-content: flex-end; gap: 8px; }
</style>
