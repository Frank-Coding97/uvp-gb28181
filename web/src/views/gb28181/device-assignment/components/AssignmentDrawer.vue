<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Building2, Check, Folder, FolderOpen } from "lucide-vue-next";
import type { DivisionItem } from "@/api/department";
import {
    applyPermissionWorkbenchAssignments,
    applyPermissionWorkbenchDepartmentAssignment,
    resolvePermissionWorkbenchDevices,
    type AssignmentItem,
    type AssignmentResult
} from "../api";

export interface AssignmentDeviceBrief {
    id: number;
    deviceId: string;
    name: string;
    ownerDeptId: number;
    ownerDeptName?: string;
}

const props = withDefaults(
    defineProps<{
        visible: boolean;
        mode: "devices" | "department";
        devices: AssignmentDeviceBrief[];
        sourceDeptId?: number;
        sourceDeptName?: string;
        sourceDirectCount?: number;
        sourceSubtreeCount?: number;
        departments: DivisionItem[];
    }>(),
    { sourceDeptId: 0, sourceDeptName: "", sourceDirectCount: 0, sourceSubtreeCount: 0 }
);

const emit = defineEmits<{
    (event: "update:visible", visible: boolean): void;
    (event: "submitted", result: AssignmentResult, targetDeptId: number): void;
}>();

const visible = computed({ get: () => props.visible, set: (value) => emit("update:visible", value) });
const targetDeptId = ref<number>();
const includeChildren = ref(false);
const submitting = ref(false);
const expectedCount = computed(() =>
    props.mode === "department"
        ? includeChildren.value
            ? props.sourceSubtreeCount
            : props.sourceDirectCount
        : props.devices.length
);
const resolvedDevices = ref<AssignmentDeviceBrief[]>([]);

const title = computed(() => (props.mode === "department" ? "调整部门设备归属" : `调整 ${props.devices.length} 台设备归属`));
const targetName = computed(() => {
    const find = (nodes: DivisionItem[]): string => {
        for (const node of nodes) {
            if (node.id === targetDeptId.value) return node.name;
            if (node.children?.length) {
                const name = find(node.children);
                if (name) return name;
            }
        }
        return "";
    };
    return find(props.departments);
});

const assignmentPreview = computed(() => {
    if (props.mode === "department" || !targetDeptId.value) return undefined;
    const changed = resolvedDevices.value.filter((device) => device.ownerDeptId !== targetDeptId.value).length;
    return { changed, skipped: resolvedDevices.value.length - changed };
});

const isTargetDisabled = (node: DivisionItem) => {
    if (props.mode === "department") return node.id === props.sourceDeptId;
    return resolvedDevices.value.length > 0 && resolvedDevices.value.every((device) => device.ownerDeptId === node.id);
};

const targetDepartments = computed(() => {
    const map = (nodes: DivisionItem[]): Array<DivisionItem & { disabled?: boolean }> =>
        nodes.map((node) => ({
            ...node,
            disabled: isTargetDisabled(node),
            children: node.children?.length ? map(node.children) : undefined
        }));
    return map(props.departments);
});

const resolveDevices = async () => {
    if (props.mode !== "devices" || !props.devices.length) return;
    const result = await resolvePermissionWorkbenchDevices(props.devices.map((device) => device.id));
    resolvedDevices.value = result.data?.devices ?? [];
    if (result.data?.unavailableIds?.length) Message.warning(`有 ${result.data.unavailableIds.length} 台设备已不可用`);
};

watch(
    () => props.visible,
    (open) => {
        if (!open) return;
        targetDeptId.value = undefined;
        includeChildren.value = false;
        resolvedDevices.value = [...props.devices];
        void resolveDevices();
    }
);

const submit = async () => {
    if (!targetDeptId.value || submitting.value) return;
    submitting.value = true;
    try {
        const response =
            props.mode === "department"
                ? await applyPermissionWorkbenchDepartmentAssignment({
                      sourceDeptId: props.sourceDeptId,
                      targetDeptId: targetDeptId.value,
                      includeChildren: includeChildren.value,
                      expectedCount: expectedCount.value ?? 0
                  })
                : await applyPermissionWorkbenchAssignments({
                      items: resolvedDevices.value.map<AssignmentItem>((device) => ({
                          deviceId: device.id,
                          expectedOwnerDeptId: device.ownerDeptId
                      })),
                      targetDeptId: targetDeptId.value
                  });
        if (response.data) {
            emit("submitted", response.data, targetDeptId.value);
            visible.value = false;
            if (response.data.summary.failed > 0) Message.warning(`部分失败：${response.data.summary.failed} 台失败，详见结果`);
            else Message.success("设备归属调整完成");
        }
    } catch (error: unknown) {
        Message.error(error instanceof Error ? error.message : "设备归属调整失败");
    } finally {
        submitting.value = false;
    }
};
</script>

<template>
    <a-drawer v-model:visible="visible" :width="560" :title="title" unmount-on-close>
        <div class="assignment-drawer">
            <div class="assignment-drawer__identity">
                <div class="assignment-drawer__identity-icon"><Building2 :size="18" /></div>
                <div>
                    <strong v-if="mode === 'department'">{{ sourceDeptName || `部门 #${sourceDeptId}` }}</strong>
                     <strong v-else>{{ resolvedDevices.length }} 台设备</strong>
                     <span v-if="mode === 'department'">预计影响 {{ expectedCount ?? 0 }} 台设备</span>
                     <span v-else>已解析 {{ resolvedDevices.length }} 台设备，提交时会校验最新归属</span>
                </div>
            </div>

            <a-alert v-if="mode === 'department'" type="info" class="assignment-drawer__scope">
                <template #icon><Folder :size="16" /></template>
                <span>当前范围：{{ includeChildren ? "直属部门及所有子部门" : "仅直属设备" }}</span>
            </a-alert>

            <a-checkbox v-if="mode === 'department'" v-model="includeChildren" class="assignment-drawer__children">
                包含子部门设备
            </a-checkbox>

            <div class="assignment-drawer__target-label">目标部门</div>
            <div class="assignment-drawer__tree">
                <a-tree
                    :data="targetDepartments"
                    :field-names="{ key: 'id', title: 'name', children: 'children' }"
                    :selected-keys="targetDeptId ? [targetDeptId] : []"
                    default-expand-all
                     @select="(keys: Array<string | number>) => (targetDeptId = keys.length ? Number(keys[0]) : undefined)"
                >
                    <template #icon="{ isLeaf, expanded }">
                        <component :is="isLeaf ? Building2 : expanded ? FolderOpen : Folder" :size="14" />
                    </template>
                </a-tree>
            </div>
            <div class="assignment-drawer__selected" :class="{ active: targetDeptId }">
                <Check :size="15" />
                <span>{{ targetName ? `将迁入：${targetName}` : "请选择目标部门" }}</span>
            </div>
            <div v-if="assignmentPreview" class="assignment-drawer__preview">
                <span>本次预计变更 {{ assignmentPreview.changed }} 台</span>
                <span v-if="assignmentPreview.skipped">已有目标归属 {{ assignmentPreview.skipped }} 台，将跳过</span>
            </div>
            <p class="assignment-drawer__hint">归属变更会同步设备关联资源，并使原有数据权限立即失效。</p>
        </div>
        <template #footer>
            <a-space>
                <a-button @click="visible = false">取消</a-button>
                <a-button type="primary" :loading="submitting" :disabled="!targetDeptId" @click="submit">保存调整</a-button>
            </a-space>
        </template>
    </a-drawer>
</template>

<style scoped lang="less">
.assignment-drawer {
    display: flex;
    flex-direction: column;
    gap: 14px;

    &__identity {
        display: flex;
        align-items: center;
        gap: 10px;
        padding: 12px;
        border: 1px solid var(--uvp-panel-border);
        border-radius: 8px;
        background: var(--uvp-dialog-bg);

        strong,
        span { display: block; }
        strong { color: var(--uvp-text-primary); font-size: 14px; }
        span { margin-top: 3px; color: var(--uvp-text-tertiary); font-size: 12px; }
    }

    &__identity-icon { display: grid; width: 34px; height: 34px; color: var(--uvp-brand-strong); background: var(--uvp-brand-soft); border-radius: 8px; place-items: center; }
    &__children { margin-top: -6px; }
    &__target-label { color: var(--uvp-text-secondary); font-size: 13px; font-weight: 650; }
    &__tree { max-height: 300px; overflow: auto; padding: 8px; border: 1px solid var(--uvp-panel-border); border-radius: 8px; }
    &__selected { display: flex; align-items: center; gap: 7px; padding: 9px 10px; color: var(--uvp-text-tertiary); border: 1px solid var(--uvp-panel-border); border-radius: 8px; font-size: 12px; }
    &__selected.active { color: var(--uvp-brand-strong); border-color: color-mix(in srgb, var(--uvp-brand) 28%, var(--uvp-panel-border)); background: var(--uvp-brand-soft); }
    &__hint { margin: 0; color: var(--uvp-text-tertiary); font-size: 12px; line-height: 18px; }
}
</style>
