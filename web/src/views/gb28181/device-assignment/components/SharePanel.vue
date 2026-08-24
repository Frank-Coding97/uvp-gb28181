<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Building2, UsersRound } from "lucide-vue-next";
import { getAccountListAPI } from "@/api/user";
import { getDivisionAPI, type DivisionItem } from "@/api/department";
import {
    applyPermissionWorkbenchGrants,
    queryPermissionWorkbenchGrants,
    type GrantItem,
    type GrantTarget,
    type GrantTargetType
} from "../api";

export interface ShareDeviceBrief {
    id: number;
    deviceId: string;
    name: string;
}

const props = defineProps<{
    visible: boolean;
    devices: ShareDeviceBrief[];
}>();

const emit = defineEmits<{
    (e: "update:visible", value: boolean): void;
    (e: "changed"): void;
}>();

const visible = computed({
    get: () => props.visible,
    set: (value: boolean) => emit("update:visible", value)
});

/** 单台模式:展示已有共享并可增删;批量模式:仅批量添加 */
const singleMode = computed(() => props.devices.length === 1);
const currentDevice = computed(() => (singleMode.value ? props.devices[0] : null));

const panelTitle = computed(() => (singleMode.value ? "共享管理" : `批量共享(${props.devices.length} 台设备)`));

// ---- 穿梭数据源(Arco transfer 数据项要求 { value, label } 字段) ----
const activeTab = ref<GrantTargetType>("dept");

interface TransferDeptNode {
    value: number;
    label: string;
    children?: TransferDeptNode[];
}

// 部门树(穿梭左栏,含 children)
const deptTree = ref<TransferDeptNode[]>([]);
const deptLoading = ref(false);

// 用户列表(穿梭左栏,扁平)
const userList = ref<Array<{ value: number; label: string }>>([]);
const userLoading = ref(false);

/** id → 部门名(用于授权成功提示) */
const deptNameById = new Map<number, string>();

const toTransferTree = (nodes: DivisionItem[]): TransferDeptNode[] =>
    nodes.map((node) => {
        deptNameById.set(node.id, node.name);
        return {
            value: node.id,
            label: node.name,
            children: node.children?.length ? toTransferTree(node.children) : undefined
        };
    });

// 已共享记录(仅单台模式,用于移除时找 grant id)
const grants = ref<GrantItem[]>([]);
const revisionByDevice = ref(new Map<number, string>());
const initialDeptTargetKeys = ref<number[]>([]);
const initialUserTargetKeys = ref<number[]>([]);

// 穿梭目标 keys
const deptTargetKeys = ref<number[]>([]);
const userTargetKeys = ref<number[]>([]);

const loadDeptTree = async () => {
    deptLoading.value = true;
    try {
        const { data } = await getDivisionAPI();
        deptTree.value = toTransferTree(data?.list ?? []);
    } catch (error: unknown) {
        Message.error(error instanceof Error ? error.message : "加载部门列表失败");
    } finally {
        deptLoading.value = false;
    }
};

const loadUserList = async () => {
    userLoading.value = true;
    try {
        const { data } = await getAccountListAPI({ page: 1, pageSize: 200 });
        userList.value = (data?.list ?? []).map((u) => ({
            value: u.id,
            label: u.nickName || u.userName
        }));
    } catch (error: unknown) {
        Message.error(error instanceof Error ? error.message : "加载用户列表失败");
    } finally {
        userLoading.value = false;
    }
};

const loadGrants = async () => {
    if (!currentDevice.value) return;
    try {
        const { data } = await queryPermissionWorkbenchGrants([currentDevice.value.id]);
        const state = data?.devices?.[0];
        grants.value = state?.grants ?? [];
        revisionByDevice.value = new Map(state ? [[currentDevice.value.id, state.revision]] : []);
        deptTargetKeys.value = grants.value.filter((g) => g.targetType === "dept").map((g) => g.targetId);
        userTargetKeys.value = grants.value.filter((g) => g.targetType === "user").map((g) => g.targetId);
        initialDeptTargetKeys.value = [...deptTargetKeys.value];
        initialUserTargetKeys.value = [...userTargetKeys.value];
    } catch (error: unknown) {
        Message.error(error instanceof Error ? error.message : "加载共享列表失败");
    }
};

// ---- 穿梭变更(对比前后 keys 差集) ----
const diffKeys = (before: number[], after: number[]) => ({
    added: after.filter((id) => !before.includes(id)),
    removed: before.filter((id) => !after.includes(id))
});

const saveChanges = async () => {
    const changes: Array<{ mode: "add" | "remove"; targets: GrantTarget[] }> = [];
    const collect = (type: GrantTargetType, before: number[], after: number[]) => {
        const { added, removed } = diffKeys(before, after);
        if (added.length) changes.push({ mode: "add", targets: added.map((id) => ({ type, id })) });
        if (removed.length) changes.push({ mode: "remove", targets: removed.map((id) => ({ type, id })) });
    };
    collect("dept", initialDeptTargetKeys.value, deptTargetKeys.value);
    collect("user", initialUserTargetKeys.value, userTargetKeys.value);
    if (!changes.length) {
        Message.info("没有待保存的变更");
        return;
    }
    for (const change of changes) {
        await applyPermissionWorkbenchGrants({
            items: props.devices.map((device) => ({ deviceId: device.id, expectedRevision: revisionByDevice.value.get(device.id) ?? "" })),
            mode: change.mode,
            targets: change.targets
        });
    }
    Message.success("共享变更已保存");
    emit("changed");
    visible.value = false;
};

// ---- 生命周期 ----
onMounted(() => {
    void loadDeptTree();
    void loadUserList();
});

watch(
    () => props.visible,
    (value) => {
        if (value) {
            deptTargetKeys.value = [];
            userTargetKeys.value = [];
            void loadGrants();
        }
    }
);
</script>

<template>
    <a-modal
        v-model:visible="visible"
        modal-class="uvp-system-dialog"
        :width="780"
        :footer="false"
        @cancel="emit('update:visible', false)"
    >
        <template #title>
            <div class="share-panel__title">
                <span class="share-panel__title-main">{{ panelTitle }}</span>
                <span class="share-panel__title-hint">
                    调整共享对象后保存变更
                </span>
            </div>
        </template>

        <div v-if="devices.length > 0" class="share-panel">
            <a-tabs v-model:active-key="activeTab">
                <a-tab-pane key="dept">
                    <template #title>
                        <span class="share-panel__tab-title"><Building2 :size="15" />按部门授权</span>
                    </template>
                    <a-transfer
                        :data="deptTree"
                        v-model:target-keys="deptTargetKeys"
                        :title="['可选部门', '已授权部门']"
                        :show-search="true"
                        :input-search-props="{ placeholder: '搜索部门' }"
                        :loading="deptLoading"
                        class="share-panel__transfer"
                    />
                </a-tab-pane>
                <a-tab-pane key="user">
                    <template #title>
                        <span class="share-panel__tab-title"><UsersRound :size="15" />按用户授权</span>
                    </template>
                    <a-transfer
                        :data="userList"
                        v-model:target-keys="userTargetKeys"
                        :title="['可选用户', '已授权用户']"
                        :show-search="true"
                        :input-search-props="{ placeholder: '搜索用户' }"
                        :loading="userLoading"
                        class="share-panel__transfer"
                    />
                </a-tab-pane>
            </a-tabs>
            <div class="share-panel__footer">
                <a-button @click="visible = false">取消</a-button>
                <a-button type="primary" @click="saveChanges">保存变更</a-button>
            </div>
        </div>
    </a-modal>
</template>

<style scoped lang="less">
.share-panel {
    &__title {
        display: flex;
        align-items: baseline;
        gap: 12px;
    }
    &__title-main {
        font-size: 16px;
        font-weight: 680;
        color: var(--uvp-text-primary);
    }
    &__title-hint {
        font-size: 12px;
        font-weight: 400;
        color: var(--uvp-text-tertiary);
    }
    &__tab-title {
        display: inline-flex;
        align-items: center;
        gap: 6px;
    }
    &__transfer {
        width: 100%;

        :deep(.arco-transfer-view) {
            flex: 1 1 0;
            min-width: 0;
            height: 320px;
        }
    }
}

:deep(.arco-tabs-nav-tab) {
    color: var(--uvp-text-secondary);

    &.arco-tabs-nav-tab-active {
        color: var(--uvp-brand);
        font-weight: 600;
    }
}

:deep(.arco-tabs-content) {
    padding-top: 14px;
}
</style>
