<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Building2, UsersRound } from "lucide-vue-next";
import { getAccountListAPI } from "@/api/user";
import { getDivisionAPI, type DivisionItem } from "@/api/department";
import { addGrants, listGrants, removeGrant, type GrantTargetType, type GrantVO } from "../api";

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
const grants = ref<GrantVO[]>([]);

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
        const { data } = await listGrants(currentDevice.value.id);
        grants.value = data ?? [];
        deptTargetKeys.value = grants.value.filter((g) => g.targetType === "dept").map((g) => g.targetId);
        userTargetKeys.value = grants.value.filter((g) => g.targetType === "user").map((g) => g.targetId);
    } catch (error: unknown) {
        Message.error(error instanceof Error ? error.message : "加载共享列表失败");
    }
};

// ---- 穿梭变更(对比前后 keys 差集) ----
const diffKeys = (before: number[], after: number[]) => ({
    added: after.filter((id) => !before.includes(id)),
    removed: before.filter((id) => !after.includes(id))
});

watch(deptTargetKeys, async (after, before) => {
    const { added, removed } = diffKeys(before, after);
    if (added.length) await doAddGrants("dept", added);
    if (removed.length) await doRemoveGrants("dept", removed);
});

watch(userTargetKeys, async (after, before) => {
    const { added, removed } = diffKeys(before, after);
    if (added.length) await doAddGrants("user", added);
    if (removed.length) await doRemoveGrants("user", removed);
});

const doAddGrants = async (type: GrantTargetType, ids: number[]) => {
    const names =
        type === "dept"
            ? ids.map((id) => deptNameById.get(id) ?? `部门 #${id}`)
            : ids.map((id) => userList.value.find((u) => u.value === id)?.label ?? `用户 #${id}`);
    let addedTotal = 0;
    let skippedTotal = 0;
    for (const device of props.devices) {
        const { data } = await addGrants(
            device.id,
            ids.map((id, i) => ({ type, id, name: names[i] }))
        );
        addedTotal += data?.added ?? 0;
        skippedTotal += data?.skipped ?? 0;
    }
    Message.success(`已授权 ${addedTotal} 项${skippedTotal ? `,跳过重复 ${skippedTotal} 项` : ""}`);
    emit("changed");
};

const doRemoveGrants = async (type: GrantTargetType, ids: number[]) => {
    // 仅单台模式可精确移除(grant id 可定位);批量模式不支持穿梭移除
    if (!singleMode.value || !currentDevice.value) {
        Message.warning("批量模式下不支持移除,请在单台设备的共享管理中操作");
        return;
    }
    for (const id of ids) {
        const grant = grants.value.find((g) => g.targetType === type && g.targetId === id);
        if (!grant) continue;
        try {
            await removeGrant(currentDevice.value.id, grant.id);
        } catch (error: unknown) {
            Message.error(error instanceof Error ? error.message : `移除共享失败(${grant.targetName})`);
        }
    }
    Message.success("已收回共享");
    emit("changed");
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
                    勾选左侧对象,移入右侧即生效
                    <template v-if="singleMode">,右侧移回可收回授权</template>
                    <template v-else>(批量模式仅支持新增)</template>
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
