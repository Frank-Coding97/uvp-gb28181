<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { LogOut, RefreshCw } from "lucide-vue-next";
import { getCurrentViewers, kickCurrentViewer, type CurrentViewer } from "../trafficApi";
import { latestRequestGuard } from "../trafficState";

const props = defineProps<{ deviceId: string; channelId: string }>();
const loading = ref(false);
const error = ref("");
const viewers = ref<CurrentViewer[]>([]);
const canKick = ref(false);
const kicking = ref(new Set<string>());
const guard = latestRequestGuard();
let abortController: AbortController | null = null;

async function load() {
    abortController?.abort();
    const controller = new AbortController();
    abortController = controller;
    const version = guard.next();
    loading.value = true;
    error.value = "";
    try {
        const result = await getCurrentViewers({ deviceId: props.deviceId, channelId: props.channelId }, controller.signal);
        if (!guard.current(version)) return;
        if (result.code !== 0) throw new Error(result.message || "当前观看加载失败");
        viewers.value = result.data?.list || [];
        canKick.value = Boolean(result.data?.canKick);
    } catch (cause: unknown) {
        if (!guard.current(version) || controller.signal.aborted) return;
        error.value = cause instanceof Error ? cause.message : "当前观看加载失败";
        viewers.value = [];
    } finally {
        if (guard.current(version)) loading.value = false;
    }
}

function kick(viewer: CurrentViewer) {
    Modal.confirm({
        title: "强退观看连接",
        content: `确认断开 ${viewer.remote} 的 ${viewer.schema.toUpperCase()} 连接？同通道其他观看连接不会受影响。`,
        okText: "强退",
        okButtonProps: { status: "danger" },
        async onOk() {
            const next = new Set(kicking.value);
            next.add(viewer.id);
            kicking.value = next;
            try {
                const result = await kickCurrentViewer(
                    { deviceId: props.deviceId, channelId: props.channelId },
                    { id: viewer.id, schema: viewer.schema }
                );
                if (result.code !== 0) throw new Error(result.message || "强退失败");
                Message.success("目标观看连接已强退");
                await load();
            } catch (cause: unknown) {
                Message.error(cause instanceof Error ? cause.message : "强退失败");
                throw cause;
            } finally {
                const remaining = new Set(kicking.value);
                remaining.delete(viewer.id);
                kicking.value = remaining;
            }
        }
    });
}

watch(() => [props.deviceId, props.channelId], load, { immediate: true });
onBeforeUnmount(() => { guard.cancel(); abortController?.abort(); });
</script>

<template>
    <section class="viewer-table" aria-label="当前观看连接">
        <div class="viewer-toolbar">
            <span>当前 {{ viewers.length }} 个观看连接</span>
            <a-tooltip content="刷新当前观看" position="top">
                <a-button class="viewer-refresh" type="text" shape="circle" aria-label="刷新当前观看" :loading="loading" @click="load">
                    <template #icon><RefreshCw :size="16" /></template>
                </a-button>
            </a-tooltip>
        </div>
        <div v-if="error" class="viewer-error" role="alert"><span>{{ error }}</span><a-button size="small" @click="load">重试</a-button></div>
        <a-table
            v-else
            :data="viewers"
            :loading="loading"
            :pagination="false"
            row-key="id"
            :scroll="{ x: 760, y: 330 }"
            class="uvp-data-table viewer-data-table"
        >
            <template #columns>
                <a-table-column title="通道" data-index="channelId" :width="180" />
                <a-table-column title="协议" :width="100"><template #cell="{ record }"><span class="protocol-tag">{{ record.schema.toUpperCase() }}</span></template></a-table-column>
                <a-table-column title="远端地址" data-index="remote" :width="180" />
                <a-table-column title="连接类型" :width="150"><template #cell="{ record }">{{ record.type || '未知' }}</template></a-table-column>
                <a-table-column title="状态" :width="90"><template #cell><span class="viewer-online"><i></i>观看中</span></template></a-table-column>
                <a-table-column title="操作" :width="100" fixed="right">
                    <template #cell="{ record }">
                        <a-tooltip v-if="canKick && record.kickable" content="强退此连接" position="top">
                            <a-button status="danger" type="text" :loading="kicking.has(record.id)" :aria-label="`强退 ${record.remote} 的观看连接`" @click="kick(record)">
                                <template #icon><LogOut :size="15" /></template>强退
                            </a-button>
                        </a-tooltip>
                        <span v-else class="not-kickable">{{ canKick ? '协议不支持' : '-' }}</span>
                    </template>
                </a-table-column>
            </template>
            <template #empty><a-empty description="当前无人观看" /></template>
        </a-table>
    </section>
</template>

<style scoped>
.viewer-table { min-width: 0; min-height: 420px; }
.viewer-toolbar { display: flex; min-height: 44px; align-items: center; justify-content: space-between; color: var(--color-text-2); font-size: 13px; }
.viewer-refresh { width: 44px; height: 44px; }
.viewer-error { display: flex; min-height: 360px; align-items: center; justify-content: center; gap: 12px; color: rgb(var(--danger-6)); }
.viewer-data-table { min-height: 360px; }
.protocol-tag { color: rgb(var(--primary-6)); font-weight: 600; }
.viewer-online { display: inline-flex; align-items: center; gap: 6px; color: rgb(var(--success-6)); }
.viewer-online i { width: 7px; height: 7px; border-radius: 50%; background: currentColor; }
.not-kickable { color: var(--color-text-3); font-size: 12px; }
@media (prefers-reduced-motion: reduce) { .viewer-table *, .viewer-table *::before, .viewer-table *::after { animation-duration: 0.01ms !important; transition-duration: 0.01ms !important; } }
</style>
