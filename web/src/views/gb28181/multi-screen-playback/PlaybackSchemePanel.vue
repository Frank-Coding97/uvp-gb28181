<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ArrowLeft, ChevronLeft, ChevronRight, MoreHorizontal, Play, Plus, Save, Search, Trash2, X } from "lucide-vue-next";
import {
    createPlaybackScheme,
    deletePlaybackScheme,
    getPlaybackScheme,
    listPlaybackSchemes,
    renamePlaybackScheme,
    replacePlaybackSchemeLayout,
    type PlaybackSchemeDetail,
    type PlaybackSchemeLayoutSize,
    type PlaybackSchemePayload,
    type PlaybackSchemeSlotInput,
    type PlaybackSchemeSummary
} from "@/api/gb28181";

const props = defineProps<{
    visible: boolean;
    currentLayout: PlaybackSchemeLayoutSize;
    currentSlots: PlaybackSchemeSlotInput[];
}>();

const emit = defineEmits<{
    "update:visible": [value: boolean];
    apply: [scheme: PlaybackSchemeDetail];
}>();

const list = ref<PlaybackSchemeSummary[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = 10;
const keyword = ref("");
const appliedKeyword = ref("");
const detail = ref<PlaybackSchemeDetail | null>(null);
const loading = ref(false);
const detailLoading = ref(false);
const saving = ref(false);
const error = ref("");
const saveMode = ref(false);
const saveName = ref("");
const menuSchemeId = ref<number | null>(null);

const hasCurrentSlots = computed(() => props.currentSlots.length > 0);
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)));

function close() {
    emit("update:visible", false);
}

function responseData<T>(response: any, message: string): T {
    if (response?.code !== 0 || !response.data) throw new Error(response?.message || message);
    return response.data as T;
}

async function loadList(nextPage = page.value) {
    loading.value = true;
    error.value = "";
    try {
        const data = responseData<{ list: PlaybackSchemeSummary[]; total: number }>(
            await listPlaybackSchemes({ page: nextPage, pageSize, q: appliedKeyword.value || undefined }),
            "加载播放方案失败"
        );
        list.value = data.list || [];
        total.value = data.total || 0;
        page.value = nextPage;
    } catch (reason: any) {
        error.value = reason?.message || "加载播放方案失败";
    } finally {
        loading.value = false;
    }
}

function search() {
    appliedKeyword.value = keyword.value.trim();
    void loadList(1);
}

async function openDetail(scheme: PlaybackSchemeSummary) {
    detailLoading.value = true;
    error.value = "";
    try {
        detail.value = responseData<PlaybackSchemeDetail>(await getPlaybackScheme(scheme.id), "加载方案详情失败");
        menuSchemeId.value = null;
    } catch (reason: any) {
        error.value = reason?.message || "加载方案详情失败";
    } finally {
        detailLoading.value = false;
    }
}

async function applyScheme(scheme: PlaybackSchemeSummary | PlaybackSchemeDetail) {
    detailLoading.value = true;
    error.value = "";
    try {
        const loaded = "slots" in scheme ? scheme : responseData<PlaybackSchemeDetail>(await getPlaybackScheme(scheme.id), "加载方案详情失败");
        detail.value = loaded;
        emit("apply", loaded);
    } catch (reason: any) {
        error.value = reason?.message || "加载方案详情失败";
    } finally {
        detailLoading.value = false;
    }
}

function beginSave() {
    if (!hasCurrentSlots.value) return;
    saveMode.value = true;
    saveName.value = "";
    error.value = "";
}

async function saveCurrent() {
    const name = saveName.value.trim();
    if (!name || !hasCurrentSlots.value) return;
    saving.value = true;
    error.value = "";
    const payload: PlaybackSchemePayload = { name, layoutSize: props.currentLayout, slots: props.currentSlots };
    try {
        responseData<PlaybackSchemeSummary>(await createPlaybackScheme(payload), "保存播放方案失败");
        saveMode.value = false;
        await loadList(1);
    } catch (reason: any) {
        error.value = reason?.message || "保存播放方案失败";
    } finally {
        saving.value = false;
    }
}

async function rename(scheme: PlaybackSchemeSummary) {
    const name = window.prompt("方案名称", scheme.name)?.trim();
    if (!name || name === scheme.name) return;
    saving.value = true;
    try {
        await renamePlaybackScheme(scheme.id, name);
        await loadList(page.value);
    } catch (reason: any) {
        error.value = reason?.message || "重命名失败";
    } finally {
        saving.value = false;
        menuSchemeId.value = null;
    }
}

async function replace(scheme: PlaybackSchemeSummary) {
    if (!hasCurrentSlots.value || !window.confirm(`确认用当前画面覆盖“${scheme.name}”吗？`)) return;
    saving.value = true;
    try {
        await replacePlaybackSchemeLayout(scheme.id, { layoutSize: props.currentLayout, slots: props.currentSlots });
        await loadList(page.value);
    } catch (reason: any) {
        error.value = reason?.message || "覆盖失败";
    } finally {
        saving.value = false;
        menuSchemeId.value = null;
    }
}

async function remove(scheme: PlaybackSchemeSummary) {
    if (!window.confirm(`确认删除“${scheme.name}”吗？`)) return;
    saving.value = true;
    try {
        await deletePlaybackScheme(scheme.id);
        if (detail.value?.id === scheme.id) detail.value = null;
        await loadList(Math.min(page.value, pageCount.value));
    } catch (reason: any) {
        error.value = reason?.message || "删除失败";
    } finally {
        saving.value = false;
        menuSchemeId.value = null;
    }
}

function availabilityLabel(value: string) {
    return { available: "可播放", offline: "离线", missing: "通道不存在", forbidden: "无权访问" }[value] || "不可用";
}

function formatUpdatedAt(value: string) {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString("zh-CN", { hour12: false });
}

watch(() => props.visible, visible => {
    if (visible) {
        detail.value = null;
        saveMode.value = false;
        void loadList(1);
    }
}, { immediate: true });
</script>

<template>
    <div v-if="visible" class="scheme-layer" @click.self="close">
        <aside class="scheme-panel" aria-labelledby="scheme-panel-title">
            <header class="scheme-header">
                <button v-if="detail" type="button" data-test="scheme-back" class="icon-button" aria-label="返回播放方案列表" title="返回" @click="detail = null"><ArrowLeft :size="17" aria-hidden="true" /></button>
                <div class="scheme-heading"><strong id="scheme-panel-title">{{ detail ? detail.name : "播放方案" }}</strong><span v-if="!detail">{{ total }} 个方案</span></div>
                <button type="button" data-test="close-schemes" class="icon-button" aria-label="关闭播放方案" title="关闭" @click="close"><X :size="17" aria-hidden="true" /></button>
            </header>

            <template v-if="detail">
                <div class="scheme-detail-toolbar"><span>{{ detail.layoutSize }} 分屏 · {{ detail.slotCount }} 个通道</span><button type="button" data-test="apply-detail" class="scheme-apply" aria-label="应用并替换当前画面" title="应用并替换当前画面" @click="applyScheme(detail)"><Play :size="14" aria-hidden="true" />应用并替换当前画面</button></div>
                <div v-if="detailLoading" class="scheme-empty">正在加载详情</div>
                <ol v-else class="scheme-slots">
                    <li v-for="slot in detail.slots" :key="slot.id || `${slot.deviceCode}-${slot.channelCode}-${slot.slotIndex}`">
                        <span class="slot-index">槽位 {{ slot.slotIndex + 1 }}</span>
                        <div class="slot-summary"><strong>{{ slot.channelName || slot.channelCode }}</strong><span>{{ slot.deviceName || slot.deviceCode }}</span></div>
                        <span class="slot-availability" :class="`availability-${slot.availability}`">{{ availabilityLabel(slot.availability) }}</span>
                    </li>
                </ol>
            </template>

            <template v-else>
                <div class="scheme-list-toolbar">
                    <div class="scheme-search"><Search :size="15" aria-hidden="true" /><input v-model="keyword" type="search" placeholder="搜索方案名称" aria-label="搜索方案名称" @keyup.enter="search" /><button type="button" aria-label="执行方案搜索" title="搜索" @click="search"><Search :size="14" aria-hidden="true" /></button></div>
                    <button type="button" data-test="new-scheme" class="scheme-save" :disabled="!hasCurrentSlots || saving" aria-label="保存当前画面为新方案" title="保存当前画面为新方案" @click="beginSave"><Plus :size="15" aria-hidden="true" />保存当前画面</button>
                </div>
                <div v-if="saveMode" class="scheme-save-form"><input v-model="saveName" data-test="scheme-name-input" maxlength="64" autofocus placeholder="输入方案名称" aria-label="方案名称" @keyup.enter="saveCurrent" /><button type="button" data-test="confirm-save" class="icon-button primary" :disabled="saving || !saveName.trim()" aria-label="确认保存方案" title="保存" @click="saveCurrent"><Save :size="15" aria-hidden="true" /></button></div>
                <p v-if="!hasCurrentSlots" class="scheme-hint">先选择通道，再保存当前画面</p>
                <p v-if="error" class="scheme-error" role="alert">{{ error }}</p>
                <div v-if="loading" class="scheme-empty">正在加载方案</div>
                <div v-else-if="!list.length" class="scheme-empty">暂无播放方案</div>
                <ul v-else class="scheme-list">
                    <li v-for="scheme in list" :key="scheme.id" class="scheme-row">
                        <button type="button" class="scheme-row-main" :data-test="`scheme-name-${scheme.id}`" @click="openDetail(scheme)"><strong>{{ scheme.name }}</strong><span>{{ scheme.layoutSize }} 分屏 · {{ scheme.slotCount }} 个通道 · {{ formatUpdatedAt(scheme.updatedAt) }}</span></button>
                        <button type="button" class="icon-button apply-button" :data-test="`apply-scheme-${scheme.id}`" aria-label="应用并替换当前画面" title="应用并替换当前画面" :disabled="saving" @click="applyScheme(scheme)"><Play :size="15" aria-hidden="true" /></button>
                        <button type="button" class="icon-button" :data-test="`scheme-menu-${scheme.id}`" aria-label="播放方案更多操作" title="更多操作" @click="menuSchemeId = menuSchemeId === scheme.id ? null : scheme.id"><MoreHorizontal :size="17" aria-hidden="true" /></button>
                        <div v-if="menuSchemeId === scheme.id" class="scheme-menu"><button type="button" @click="rename(scheme)">重命名</button><button type="button" :disabled="!hasCurrentSlots" @click="replace(scheme)">用当前画面覆盖</button><button type="button" class="danger" @click="remove(scheme)"><Trash2 :size="14" aria-hidden="true" />删除</button></div>
                    </li>
                </ul>
                <footer v-if="pageCount > 1" class="scheme-pagination"><button type="button" class="icon-button" aria-label="上一页" title="上一页" :disabled="page <= 1 || loading" @click="loadList(page - 1)"><ChevronLeft :size="16" aria-hidden="true" /></button><span>{{ page }} / {{ pageCount }}</span><button type="button" class="icon-button" aria-label="下一页" title="下一页" :disabled="page >= pageCount || loading" @click="loadList(page + 1)"><ChevronRight :size="16" aria-hidden="true" /></button></footer>
            </template>
        </aside>
    </div>
</template>

<style scoped>
@import "@/styles/zlm-tokens.css";

.scheme-layer { position: absolute; z-index: 30; inset: 0; display: flex; justify-content: flex-end; background: rgb(15 23 42 / 20%); }
.scheme-panel { display: flex; width: min(420px, 100%); min-height: 0; flex-direction: column; color: var(--zlm-text-1); background: var(--zlm-card); border-left: 1px solid var(--zlm-border); box-shadow: var(--zlm-shadow-lg); }
.scheme-header, .scheme-list-toolbar, .scheme-detail-toolbar, .scheme-pagination { display: flex; align-items: center; }
.scheme-header { justify-content: space-between; gap: 8px; padding: 14px 16px; border-bottom: 1px solid var(--zlm-border); }
.scheme-heading { display: flex; min-width: 0; flex: 1; flex-direction: column; gap: 3px; }
.scheme-heading strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 14px; }
.scheme-heading span, .scheme-detail-toolbar > span, .scheme-row-main span, .scheme-hint { color: var(--zlm-text-3); font-size: 11px; }
.icon-button { display: inline-flex; width: 30px; height: 30px; flex: 0 0 auto; align-items: center; justify-content: center; padding: 0; color: var(--zlm-text-3); background: transparent; border: 1px solid transparent; border-radius: var(--zlm-radius-sm); cursor: pointer; }
.icon-button:hover { color: var(--zlm-text-1); background: var(--zlm-fill-2); border-color: var(--zlm-border); }
.icon-button:focus-visible, .scheme-save:focus-visible, .scheme-apply:focus-visible, .scheme-row-main:focus-visible, .scheme-menu button:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }
.icon-button:disabled, .scheme-save:disabled, .scheme-apply:disabled { cursor: not-allowed; opacity: .45; }
.icon-button.primary { color: #fff; background: var(--zlm-brand-600); border-color: var(--zlm-brand-600); }
.scheme-list-toolbar { gap: 8px; padding: 12px 16px; border-bottom: 1px solid var(--zlm-border); }
.scheme-search { display: flex; min-width: 0; flex: 1; align-items: center; gap: 6px; padding: 0 8px; color: var(--zlm-text-3); background: var(--zlm-bg); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-sm); }
.scheme-search input, .scheme-save-form input { min-width: 0; flex: 1; height: 32px; color: var(--zlm-text-1); background: transparent; border: 0; outline: 0; font: inherit; font-size: 12px; }
.scheme-search button { display: inline-flex; padding: 0; color: inherit; background: transparent; border: 0; cursor: pointer; }
.scheme-save, .scheme-apply { display: inline-flex; flex: 0 0 auto; align-items: center; gap: 5px; height: 32px; padding: 0 10px; color: #fff; background: var(--zlm-brand-600); border: 1px solid var(--zlm-brand-600); border-radius: var(--zlm-radius-sm); cursor: pointer; font: inherit; font-size: 11px; }
.scheme-save-form { display: flex; gap: 8px; padding: 10px 16px; border-bottom: 1px solid var(--zlm-border); }
.scheme-save-form input { padding: 0 8px; background: var(--zlm-bg); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-sm); }
.scheme-hint, .scheme-error { margin: 10px 16px 0; }
.scheme-error { color: var(--zlm-danger); font-size: 12px; }
.scheme-list { min-height: 0; flex: 1; margin: 0; padding: 4px 0; overflow: auto; list-style: none; }
.scheme-row { position: relative; display: flex; align-items: center; gap: 4px; min-height: 58px; padding: 8px 12px 8px 16px; border-bottom: 1px solid var(--zlm-border); }
.scheme-row-main { display: flex; min-width: 0; flex: 1; flex-direction: column; gap: 5px; padding: 0; color: inherit; text-align: left; background: transparent; border: 0; cursor: pointer; font: inherit; }
.scheme-row-main strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
.scheme-apply { height: 30px; padding: 0 8px; font-size: 11px; }
.scheme-menu { position: absolute; z-index: 2; top: 42px; right: 12px; display: grid; min-width: 136px; padding: 4px; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-sm); box-shadow: var(--zlm-shadow-md); }
.scheme-menu button { display: flex; align-items: center; gap: 6px; padding: 7px 9px; color: var(--zlm-text-2); text-align: left; background: transparent; border: 0; border-radius: 3px; cursor: pointer; font: inherit; font-size: 12px; }
.scheme-menu button:hover { background: var(--zlm-fill-2); }
.scheme-menu button.danger { color: var(--zlm-danger); }
.scheme-pagination { justify-content: center; gap: 12px; padding: 10px 16px; border-top: 1px solid var(--zlm-border); }
.scheme-pagination span { color: var(--zlm-text-3); font-size: 11px; }
.scheme-detail-toolbar { justify-content: space-between; gap: 8px; padding: 12px 16px; border-bottom: 1px solid var(--zlm-border); }
.scheme-slots { min-height: 0; flex: 1; margin: 0; padding: 8px 16px; overflow: auto; list-style: none; }
.scheme-slots li { display: flex; align-items: center; gap: 8px; min-height: 52px; border-bottom: 1px solid var(--zlm-border); }
.slot-index { width: 48px; flex: 0 0 auto; color: var(--zlm-text-3); font-size: 10px; }
.slot-summary { display: flex; min-width: 0; flex: 1; flex-direction: column; gap: 3px; }
.slot-summary strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.slot-summary span { overflow: hidden; color: var(--zlm-text-3); text-overflow: ellipsis; white-space: nowrap; font-size: 10px; }
.slot-availability { flex: 0 0 auto; font-size: 10px; }
.availability-available { color: var(--zlm-success); }
.availability-offline, .availability-missing { color: var(--zlm-warning); }
.availability-forbidden { color: var(--zlm-danger); }
.scheme-empty { display: grid; min-height: 180px; flex: 1; padding: 20px; color: var(--zlm-text-3); place-items: center; font-size: 12px; }

@media (max-width: 480px) {
    .scheme-panel { width: 100%; }
    .scheme-list-toolbar { align-items: stretch; flex-direction: column; }
    .scheme-save { justify-content: center; }
}
</style>
