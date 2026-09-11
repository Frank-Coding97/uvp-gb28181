<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
    Aperture,
    ArrowDown,
    ArrowDownLeft,
    ArrowDownRight,
    ArrowLeft,
    ArrowRight,
    ArrowUp,
    ArrowUpLeft,
    ArrowUpRight,
    Compass,
    ChevronDown,
    Focus as FocusIcon,
    Gauge,
    ZoomIn,
    ZoomOut
} from "@lucide/vue";
import { controlPtz, fetchPTZDefaultSpeedConfig, getControlCapabilities } from "@/api/gb28181";
import { useUserStoreHook } from "@/store/modules/user";
import type { ChannelVO } from "../device-mgmt/api";
import { DEFAULT_PTZ_SPEED_LEVEL, levelToProtocolSpeed, normalizePtzSpeedLevel } from "../ptzSpeed";

const props = defineProps<{
    channel: ChannelVO | null;
}>();
const emit = defineEmits<{
    actionChange: [value: { channelId: number; action: string } | null];
}>();

const userStore = useUserStoreHook();
const permissions = computed(() => userStore.account.permissions ?? []);
const hasPermission = (permission: string) => permissions.value.includes("*:*:*") || permissions.value.includes(permission);
const canViewPtz = computed(() => hasPermission("gb28181:ptz:view"));
const canControlPtz = computed(() => hasPermission("gb28181:ptz:control"));
const canReadSpeed = computed(() => hasPermission("gb28181:sip:config:view"));
const canRenderPanel = computed(() => canViewPtz.value || canControlPtz.value);

const speed = ref(DEFAULT_PTZ_SPEED_LEVEL);
const collapsed = ref(false);
const capabilityState = ref("unknown");
let capabilityToken = 0;
let activeAction: { channelId: number; action: string } | null = null;

const focusedName = computed(() => props.channel?.name || props.channel?.alias || props.channel?.channelId || "未选择画面");
const disabledReason = computed(() => {
    if (!canControlPtz.value) return "当前账号没有云台控制权限";
    if (!props.channel) return "请先聚焦一个播放画面";
    if (props.channel.status !== 1) return "当前通道离线";
    if (capabilityState.value === "unsupported") return "当前设备不支持基础云台控制";
    return "";
});
const controlsDisabled = computed(() => Boolean(disabledReason.value));

function payload(channelId: number, action: string) {
    return {
        action,
        speed: levelToProtocolSpeed(speed.value),
        idempotencyKey: `${channelId}-${action}-${Date.now()}`
    };
}

async function send(channelId: number, action: string) {
    if (!canControlPtz.value) return;
    try {
        const response = await controlPtz(channelId, payload(channelId, action));
        if (response.code !== 0) throw new Error(response.message || "云台指令失败");
    } catch (error: any) {
        Message.error(error?.message || "云台指令失败");
    }
}

async function stopActive(force = false) {
    const target = activeAction;
    if (!target && !force) return;
    activeAction = null;
    emit("actionChange", null);
    const channelId = target?.channelId || props.channel?.id;
    if (channelId) await send(channelId, "stop");
}

async function startAction(action: string) {
    if (controlsDisabled.value || !props.channel) return;
    if (activeAction?.channelId === props.channel.id && activeAction.action === action) return;
    if (activeAction) await stopActive();
    activeAction = { channelId: props.channel.id, action };
    emit("actionChange", activeAction);
    await send(props.channel.id, action);
}

async function loadCapability(channelId: number, token: number) {
    if (!canViewPtz.value) return;
    try {
        const response = await getControlCapabilities(channelId);
        if (token !== capabilityToken || props.channel?.id !== channelId) return;
        capabilityState.value = response.code === 0 && response.data
            ? response.data.basicPtz?.state || "unknown"
            : "unknown";
        if (capabilityState.value === "unsupported") void stopActive();
    } catch {
        if (token === capabilityToken && props.channel?.id === channelId) capabilityState.value = "unknown";
    }
}

async function loadDefaultSpeed() {
    speed.value = DEFAULT_PTZ_SPEED_LEVEL;
    if (!canReadSpeed.value) return;
    try {
        const response = await fetchPTZDefaultSpeedConfig();
        if (response.code === 0 && response.data) speed.value = normalizePtzSpeedLevel(response.data.level);
    } catch {
        // 配置读取失败时保持默认 6 档，不影响云台控制。
    }
}

function handleVisibilityChange() {
    if (document.visibilityState === "hidden") void stopActive();
}

function handleWindowBlur() {
    void stopActive();
}

watch(
    () => [props.channel?.id, props.channel?.status] as const,
    ([channelId], previous) => {
        if (previous) void stopActive();
        capabilityState.value = "unknown";
        const token = ++capabilityToken;
        if (channelId) void loadCapability(channelId, token);
    },
    { immediate: true }
);

watch(collapsed, value => {
    if (value) void stopActive();
});

onMounted(() => {
    if (canReadSpeed.value) void loadDefaultSpeed();
    window.addEventListener("blur", handleWindowBlur);
    document.addEventListener("visibilitychange", handleVisibilityChange);
});

onBeforeUnmount(() => {
    capabilityToken += 1;
    window.removeEventListener("blur", handleWindowBlur);
    document.removeEventListener("visibilitychange", handleVisibilityChange);
    void stopActive();
});
</script>

<template>
    <section v-if="canRenderPanel" class="basic-ptz" :class="{ collapsed }" aria-label="基础云台控制">
        <header class="ptz-head">
            <div class="ptz-head-info">
                <div class="ptz-title"><Compass :size="14" aria-hidden="true" /><span>云台控制</span></div>
                <strong :title="focusedName">{{ focusedName }}</strong>
            </div>
            <button class="ptz-toggle" type="button" :aria-expanded="!collapsed" :aria-label="collapsed ? '展开云台控制' : '收起云台控制'" :title="collapsed ? '展开云台控制' : '收起云台控制'" @click="collapsed = !collapsed"><ChevronDown :size="16" aria-hidden="true" /></button>
        </header>

        <div v-show="!collapsed" class="ptz-content" :class="{ disabled: controlsDisabled }">
            <div class="ptz-pad" aria-label="云台方向">
                <button title="左上" :disabled="controlsDisabled" @pointerdown.prevent="startAction('left_up')" @pointerup.prevent="stopActive()" @pointerleave="stopActive()" @pointercancel="stopActive()"><ArrowUpLeft :size="16" /></button>
                <button data-test="ptz-up" title="上" :disabled="controlsDisabled" @pointerdown.prevent="startAction('up')" @pointerup.prevent="stopActive()" @pointerleave="stopActive()" @pointercancel="stopActive()"><ArrowUp :size="16" /></button>
                <button title="右上" :disabled="controlsDisabled" @pointerdown.prevent="startAction('right_up')" @pointerup.prevent="stopActive()" @pointerleave="stopActive()" @pointercancel="stopActive()"><ArrowUpRight :size="16" /></button>
                <button title="左" :disabled="controlsDisabled" @pointerdown.prevent="startAction('left')" @pointerup.prevent="stopActive()" @pointerleave="stopActive()" @pointercancel="stopActive()"><ArrowLeft :size="16" /></button>
                <button class="ptz-stop" title="停止" :disabled="!channel" @click="stopActive(true)"><span /></button>
                <button data-test="ptz-right" title="右" :disabled="controlsDisabled" @pointerdown.prevent="startAction('right')" @pointerup.prevent="stopActive()" @pointerleave="stopActive()" @pointercancel="stopActive()"><ArrowRight :size="16" /></button>
                <button title="左下" :disabled="controlsDisabled" @pointerdown.prevent="startAction('left_down')" @pointerup.prevent="stopActive()" @pointerleave="stopActive()" @pointercancel="stopActive()"><ArrowDownLeft :size="16" /></button>
                <button title="下" :disabled="controlsDisabled" @pointerdown.prevent="startAction('down')" @pointerup.prevent="stopActive()" @pointerleave="stopActive()" @pointercancel="stopActive()"><ArrowDown :size="16" /></button>
                <button title="右下" :disabled="controlsDisabled" @pointerdown.prevent="startAction('right_down')" @pointerup.prevent="stopActive()" @pointerleave="stopActive()" @pointercancel="stopActive()"><ArrowDownRight :size="16" /></button>
            </div>

            <label class="speed-control">
                <span><Gauge :size="12" aria-hidden="true" />速度</span>
                <input v-model.number="speed" type="range" min="1" max="10" :disabled="controlsDisabled" aria-label="云台速度" />
                <output>{{ speed }}</output>
            </label>

            <div class="lens-controls">
                <div class="lens-row">
                    <span><ZoomIn :size="12" aria-hidden="true" />变倍</span>
                    <div>
                        <button title="放大" :disabled="controlsDisabled" @pointerdown.prevent="startAction('zoom_in')" @pointerup.prevent="stopActive()" @pointerleave="stopActive()" @pointercancel="stopActive()"><ZoomIn :size="14" /></button>
                        <button title="缩小" :disabled="controlsDisabled" @pointerdown.prevent="startAction('zoom_out')" @pointerup.prevent="stopActive()" @pointerleave="stopActive()" @pointercancel="stopActive()"><ZoomOut :size="14" /></button>
                    </div>
                </div>
                <div class="lens-row">
                    <span><FocusIcon :size="12" aria-hidden="true" />聚焦</span>
                    <div>
                        <button title="远焦" :disabled="controlsDisabled" @pointerdown.prevent="startAction('focus_far')" @pointerup.prevent="stopActive()" @pointerleave="stopActive()" @pointercancel="stopActive()">远</button>
                        <button title="近焦" :disabled="controlsDisabled" @pointerdown.prevent="startAction('focus_near')" @pointerup.prevent="stopActive()" @pointerleave="stopActive()" @pointercancel="stopActive()">近</button>
                    </div>
                </div>
                <div class="lens-row">
                    <span><Aperture :size="12" aria-hidden="true" />光圈</span>
                    <div>
                        <button title="光圈开大" :disabled="controlsDisabled" @pointerdown.prevent="startAction('iris_open')" @pointerup.prevent="stopActive()" @pointerleave="stopActive()" @pointercancel="stopActive()">+</button>
                        <button title="光圈缩小" :disabled="controlsDisabled" @pointerdown.prevent="startAction('iris_close')" @pointerup.prevent="stopActive()" @pointerleave="stopActive()" @pointercancel="stopActive()">-</button>
                    </div>
                </div>
            </div>
        </div>
        <p v-show="!collapsed && disabledReason" class="ptz-disabled-reason">{{ disabledReason }}</p>
    </section>
</template>

<style scoped>
.basic-ptz {
    flex: 0 0 auto;
    color: var(--zlm-text-2);
    background: var(--zlm-card);
    border: 1px solid var(--zlm-border);
    border-radius: var(--zlm-radius-md);
    box-shadow: var(--zlm-shadow-sm);
}
.ptz-head,
.ptz-head-info,
.ptz-title,
.speed-control,
.speed-control span,
.lens-row,
.lens-row > span,
.lens-row > div {
    display: flex;
    align-items: center;
}
.ptz-head { height: 42px; justify-content: space-between; gap: 8px; padding: 0 8px 0 12px; border-bottom: 1px solid var(--zlm-divider); }
.ptz-head-info { min-width: 0; flex: 1; gap: 8px; }
.ptz-title { gap: 7px; color: var(--zlm-text-1); font-size: 13px; font-weight: 700; }
.ptz-head-info strong { min-width: 0; overflow: hidden; flex: 1; color: var(--zlm-brand-600); text-overflow: ellipsis; white-space: nowrap; font-size: 11px; }
.ptz-toggle { display: inline-grid; width: 28px; height: 28px; flex: 0 0 28px; padding: 0; color: var(--zlm-text-3); background: transparent; border: 0; border-radius: 5px; place-items: center; cursor: pointer; }
.ptz-toggle:hover, .ptz-toggle:focus-visible { color: var(--zlm-brand-600); background: var(--zlm-brand-50); outline: 0; }
.ptz-toggle svg { transition: transform 0.15s ease; }
.ptz-toggle[aria-expanded="false"] svg { transform: rotate(180deg); }
.basic-ptz.collapsed .ptz-head { border-bottom-color: transparent; }
.ptz-content { display: grid; grid-template-columns: 114px minmax(0, 1fr); gap: 10px 12px; padding: 10px 12px 8px; }
.ptz-content.disabled { opacity: 0.56; }
.ptz-pad { display: grid; grid-row: span 2; grid-template-columns: repeat(3, 34px); grid-template-rows: repeat(3, 34px); gap: 4px; }
.ptz-pad button,
.lens-row button {
    display: inline-grid;
    min-width: 0;
    height: 34px;
    padding: 0;
    place-items: center;
    color: var(--zlm-text-2);
    background: var(--zlm-bg);
    border: 1px solid var(--zlm-border);
    border-radius: 6px;
    cursor: pointer;
    font: inherit;
    font-size: 11px;
    touch-action: none;
    user-select: none;
}
.ptz-pad button:hover:not(:disabled),
.lens-row button:hover:not(:disabled),
.ptz-pad button:focus-visible,
.lens-row button:focus-visible { color: var(--zlm-brand-600); background: var(--zlm-brand-50); border-color: var(--zlm-brand-100); outline: 0; }
.ptz-pad button:active:not(:disabled),
.lens-row button:active:not(:disabled) { color: #FFFFFF; background: var(--zlm-brand-600); border-color: var(--zlm-brand-600); }
.ptz-pad button:disabled,
.lens-row button:disabled { cursor: not-allowed; }
.ptz-stop span { width: 10px; height: 10px; background: var(--zlm-danger-500); border-radius: 2px; }
.speed-control { min-width: 0; gap: 6px; color: var(--zlm-text-3); font-size: 11px; }
.speed-control span { gap: 4px; flex: 0 0 auto; }
.speed-control input { min-width: 0; flex: 1; accent-color: var(--zlm-brand-600); }
.speed-control output { width: 14px; color: var(--zlm-text-1); text-align: right; font-size: 11px; }
.lens-controls { display: grid; gap: 5px; }
.lens-row { min-width: 0; justify-content: space-between; gap: 6px; }
.lens-row > span { gap: 5px; color: var(--zlm-text-3); font-size: 11px; }
.lens-row > div { flex: 0 0 auto; gap: 4px; }
.lens-row button { width: 34px; height: 28px; }
.ptz-disabled-reason { margin: 0; padding: 0 12px 9px; color: var(--zlm-text-4); font-size: 11px; text-align: center; }
@media (max-width: 768px) {
    .ptz-content { grid-template-columns: 114px minmax(150px, 1fr); }
}
</style>
