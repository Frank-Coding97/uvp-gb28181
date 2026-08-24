<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Building2, Check, CircleAlert, UserRound, UsersRound } from "lucide-vue-next";
import { useDebounceFn } from "@vueuse/core";
import {
    applyPermissionWorkbenchGrants,
    queryPermissionWorkbenchGrants,
    searchPermissionWorkbenchGrantTargets,
    type GrantApplyResult,
    type GrantTarget,
    type GrantTargetOption,
    type GrantTargetType
} from "../api";

export interface ShareDeviceBrief {
    id: number;
    deviceId: string;
    name: string;
}

type ShareMode = "add" | "remove";

const props = defineProps<{
    visible: boolean;
    devices: ShareDeviceBrief[];
}>();

const emit = defineEmits<{
    (event: "update:visible", visible: boolean): void;
    (event: "submitted", result: GrantApplyResult): void;
}>();

const mode = ref<ShareMode>("add");
const targetType = ref<GrantTargetType>("dept");
const loading = ref(false);
const submitting = ref(false);
const searchKeyword = ref("");
const targetPage = ref(1);
const targetPageSize = 30;
const targetTotal = ref(0);
const targetOptions = ref<GrantTargetOption[]>([]);
const grantsByKey = ref(new Map<string, { count: number; invalidCount: number; names: string[] }>());
const revisions = ref(new Map<number, string>());
const addDraft = ref(new Set<string>());
const removeDraft = ref(new Set<string>());

const visible = computed({
    get: () => props.visible,
    set: (value: boolean) => (value ? emit("update:visible", true) : requestClose())
});
const title = computed(() => (props.devices.length === 1 ? "共享授权" : `共享授权 · ${props.devices.length} 台设备`));
const activeDraft = computed(() => (mode.value === "add" ? addDraft.value : removeDraft.value));
const draftCount = computed(() => activeDraft.value.size);
const hasDraft = computed(() => addDraft.value.size > 0 || removeDraft.value.size > 0);
const currentStats = computed(() =>
    [...grantsByKey.value.entries()]
        .filter(([key]) => key.startsWith(`${targetType.value}:`))
        .map(([key, value]) => ({ key, ...value, targetId: Number(key.split(":")[1]) }))
);
const currentOptions = computed(() => {
    const byKey = new Map(targetOptions.value.map((item) => [`${item.type}:${item.id}`, item]));
    if (mode.value === "remove") {
        for (const item of currentStats.value) {
            if (!byKey.has(item.key)) {
                byKey.set(item.key, {
                    id: item.targetId,
                    type: targetType.value,
                    name: item.names[0] || `目标 #${item.targetId}`
                });
            }
        }
    }
    const keyword = searchKeyword.value.trim().toLowerCase();
    return [...byKey.values()].filter((item) => !keyword || item.name.toLowerCase().includes(keyword) || item.deptName?.toLowerCase().includes(keyword));
});

const optionStats = (option: GrantTargetOption) => grantsByKey.value.get(`${option.type}:${option.id}`);
const optionState = (option: GrantTargetOption) => {
    const count = optionStats(option)?.count ?? 0;
    if (!props.devices.length || count === 0) return "none";
    if (count === props.devices.length) return "all";
    return "partial";
};
const isDisabled = (option: GrantTargetOption) => mode.value === "add" && optionState(option) === "all";
const isChecked = (option: GrantTargetOption) => activeDraft.value.has(`${option.type}:${option.id}`);

const clearDraft = () => {
    addDraft.value = new Set();
    removeDraft.value = new Set();
};

const requestClose = () => {
    if (!hasDraft.value) {
        emit("update:visible", false);
        return;
    }
    Modal.confirm({
        title: "放弃未保存变更？",
        content: "当前共享调整尚未保存，关闭后这些选择会丢失。",
        okText: "放弃变更",
        cancelText: "继续编辑",
        onOk: () => {
            clearDraft();
            emit("update:visible", false);
        }
    });
};

const toggleDraft = (option: GrantTargetOption) => {
    if (isDisabled(option)) return;
    const next = new Set(activeDraft.value);
    const key = `${option.type}:${option.id}`;
    if (next.has(key)) next.delete(key);
    else next.add(key);
    if (mode.value === "add") addDraft.value = next;
    else removeDraft.value = next;
};

const loadGrants = async () => {
    if (!props.devices.length) return;
    loading.value = true;
    try {
        const { data } = await queryPermissionWorkbenchGrants(props.devices.map((device) => device.id));
        const next = new Map<string, { count: number; invalidCount: number; names: string[] }>();
        const nextRevisions = new Map<number, string>();
        for (const device of data?.devices ?? []) {
            nextRevisions.set(device.deviceId, device.revision);
            for (const grant of device.grants) {
                const key = `${grant.targetType}:${grant.targetId}`;
                const state = next.get(key) ?? { count: 0, invalidCount: 0, names: [] };
                state.count += 1;
                if (grant.invalid) state.invalidCount += 1;
                if (grant.targetName && !state.names.includes(grant.targetName)) state.names.push(grant.targetName);
                next.set(key, state);
            }
        }
        grantsByKey.value = next;
        revisions.value = nextRevisions;
    } catch (error: unknown) {
        Message.error(error instanceof Error ? error.message : "加载共享授权失败");
    } finally {
        loading.value = false;
    }
};

const loadTargets = async () => {
    if (mode.value === "remove") return;
    try {
        const { data } = await searchPermissionWorkbenchGrantTargets({
            type: targetType.value,
            q: searchKeyword.value.trim() || undefined,
            page: targetPage.value,
            pageSize: targetPageSize
        });
        targetOptions.value = data?.list ?? [];
        targetTotal.value = data?.total ?? 0;
    } catch (error: unknown) {
        Message.error(error instanceof Error ? error.message : "加载授权目标失败");
    }
};
const debouncedLoadTargets = useDebounceFn(loadTargets, 250);

const switchMode = (next: ShareMode) => {
    mode.value = next;
    searchKeyword.value = "";
    targetPage.value = 1;
    if (next === "remove") {
        targetOptions.value = [];
        targetTotal.value = 0;
    }
    void loadTargets();
};
const switchTargetType = (next: GrantTargetType) => {
    targetType.value = next;
    searchKeyword.value = "";
    targetPage.value = 1;
    void loadTargets();
};
const submit = async () => {
    if (!draftCount.value || submitting.value) return;
    submitting.value = true;
    try {
        const targets: GrantTarget[] = [...activeDraft.value].map((key) => {
            const [type, id] = key.split(":");
            return { type: type as GrantTargetType, id: Number(id) };
        });
        const { data } = await applyPermissionWorkbenchGrants({
            items: props.devices.map((device) => ({ deviceId: device.id, expectedRevision: revisions.value.get(device.id) ?? "" })),
            mode: mode.value,
            targets
        });
        if (data) {
            clearDraft();
            emit("submitted", data);
            emit("update:visible", false);
            Message.success("共享变更已保存");
        }
    } catch (error: unknown) {
        Message.error(error instanceof Error ? error.message : "保存共享变更失败");
    } finally {
        submitting.value = false;
    }
};

watch(
    () => props.visible,
    (open) => {
        if (!open) return;
        mode.value = "add";
        targetType.value = "dept";
        searchKeyword.value = "";
        targetPage.value = 1;
        clearDraft();
        void loadGrants();
        void loadTargets();
    }
);
</script>

<template>
    <a-drawer v-model:visible="visible" :width="560" :title="title" unmount-on-close>
        <div class="share-drawer">
            <div class="share-drawer__devices">
                <div class="share-drawer__device-icon"><UsersRound :size="17" /></div>
                <div>
                    <strong>{{ devices.length === 1 ? devices[0]?.name : `已选择 ${devices.length} 台设备` }}</strong>
                    <span>{{ devices.length === 1 ? devices[0]?.deviceId : "新增或收回共享授权将同时作用于所选设备" }}</span>
                </div>
            </div>

            <a-radio-group :model-value="mode" type="button" @change="(value: string | number) => switchMode(value as ShareMode)">
                <a-radio value="add">新增共享</a-radio>
                <a-radio value="remove">收回共享</a-radio>
            </a-radio-group>
            <a-radio-group :model-value="targetType" type="button" @change="(value: string | number) => switchTargetType(value as GrantTargetType)">
                <a-radio value="dept"><Building2 :size="14" /> 部门</a-radio>
                <a-radio value="user"><UserRound :size="14" /> 用户</a-radio>
            </a-radio-group>

            <a-alert v-if="mode === 'remove'" type="warning">
                收回模式包含已停用或已删除的历史目标，仅用于清理已有授权，不会新增关系。
            </a-alert>

            <div class="share-drawer__search">
                <a-input v-model="searchKeyword" allow-clear :placeholder="mode === 'add' ? '搜索可授权目标' : '从已有授权中筛选'" @input="debouncedLoadTargets" />
            </div>
            <div v-if="loading" class="share-drawer__loading">正在读取当前授权…</div>
            <div v-else class="share-drawer__targets">
                <button
                    v-for="option in currentOptions"
                    :key="`${option.type}:${option.id}`"
                    type="button"
                    class="share-drawer__target"
                    :class="{ checked: isChecked(option), disabled: isDisabled(option) }"
                    :disabled="isDisabled(option)"
                    @click="toggleDraft(option)"
                >
                    <span class="share-drawer__target-check"><Check v-if="isChecked(option)" :size="14" /></span>
                    <span class="share-drawer__target-main">
                        <strong>{{ option.name }}</strong>
                        <small v-if="option.deptName">{{ option.deptName }}</small>
                    </span>
                    <span class="share-drawer__target-state">
                        {{ optionState(option) === "all" ? "全部" : optionState(option) === "partial" ? "部分" : "未授权" }}
                        <template v-if="optionStats(option)?.invalidCount"> · 含失效</template>
                    </span>
                </button>
                <div v-if="!currentOptions.length" class="share-drawer__empty">
                    <CircleAlert :size="18" />
                    <span>{{ mode === "add" ? "没有可授权目标" : "当前设备没有该类型的共享授权" }}</span>
                </div>
            </div>
            <a-pagination
                v-if="mode === 'add' && targetTotal > targetPageSize"
                :total="targetTotal"
                :current="targetPage"
                :page-size="targetPageSize"
                size="small"
                show-total
                @change="(page: number) => { targetPage = page; void loadTargets(); }"
            />
            <div v-if="draftCount" class="share-drawer__draft"><Check :size="15" /> 已暂存 {{ draftCount }} 个目标，点击保存后才会生效</div>
        </div>
        <template #footer>
            <a-space>
                <a-button @click="requestClose">取消</a-button>
                <a-button type="primary" :loading="submitting" :disabled="!draftCount" @click="submit">保存变更</a-button>
            </a-space>
        </template>
    </a-drawer>
</template>

<style scoped lang="less">
.share-drawer { display: flex; flex-direction: column; gap: 12px; }
.share-drawer__devices { display: flex; align-items: center; gap: 10px; padding: 11px 12px; border: 1px solid var(--uvp-panel-border); border-radius: 8px; background: var(--uvp-dialog-bg); }
.share-drawer__devices strong, .share-drawer__devices span { display: block; }
.share-drawer__devices strong { color: var(--uvp-text-primary); font-size: 14px; }
.share-drawer__devices span { margin-top: 3px; color: var(--uvp-text-tertiary); font-size: 12px; }
.share-drawer__device-icon { display: grid; width: 32px; height: 32px; color: var(--uvp-brand-strong); background: var(--uvp-brand-soft); border-radius: 8px; place-items: center; }
.share-drawer__search { display: flex; gap: 8px; }
.share-drawer__targets { display: flex; min-height: 180px; max-height: 360px; flex-direction: column; gap: 6px; overflow: auto; }
.share-drawer__target { display: flex; align-items: center; gap: 9px; padding: 9px 10px; color: var(--uvp-text-primary); border: 1px solid var(--uvp-panel-border); border-radius: 8px; background: transparent; text-align: left; cursor: pointer; }
.share-drawer__target:hover, .share-drawer__target.checked { border-color: color-mix(in srgb, var(--uvp-brand) 35%, var(--uvp-panel-border)); background: var(--uvp-brand-soft); }
.share-drawer__target.disabled { opacity: .6; cursor: not-allowed; }
.share-drawer__target-check { display: grid; flex: 0 0 20px; width: 20px; height: 20px; color: white; border: 1px solid var(--uvp-panel-border); border-radius: 5px; place-items: center; }
.share-drawer__target.checked .share-drawer__target-check { border-color: var(--uvp-brand); background: var(--uvp-brand); }
.share-drawer__target-main { min-width: 0; flex: 1; }
.share-drawer__target-main strong, .share-drawer__target-main small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.share-drawer__target-main strong { font-size: 13px; }
.share-drawer__target-main small { margin-top: 2px; color: var(--uvp-text-tertiary); font-size: 11px; }
.share-drawer__target-state { color: var(--uvp-text-tertiary); font-size: 11px; }
.share-drawer__draft { display: flex; align-items: center; gap: 6px; padding: 8px 10px; color: var(--uvp-brand-strong); border-radius: 7px; background: var(--uvp-brand-soft); font-size: 12px; }
.share-drawer__loading, .share-drawer__empty { display: flex; min-height: 180px; align-items: center; justify-content: center; gap: 7px; color: var(--uvp-text-tertiary); font-size: 12px; }
</style>
