<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import {
    fetchDirectoryTree,
    startPlay,
    stopPlay,
    type DirectoryDimension,
    type DirectoryNode,
    type PlayResult
} from "@/api/gb28181";
import DirectoryDimensionTabs from "./components/DirectoryDimensionTabs.vue";
import DirectoryTree from "./components/DirectoryTree.vue";
import PlayWindow from "./components/PlayWindow.vue";

// ==== 三 tab 状态 ====
const currentDimension = ref<DirectoryDimension>("native");

// ==== 播放状态(全局,三 tab 共享)====
const playing = ref<{
    deviceId: string;
    channelId: string;
    result: PlayResult;
} | null>(null);
const playLoading = ref(false);

// ==== 自动刷新(D-10 决策:三 tab 共享)====
const autoRefresh = ref(true);
const refreshTimer = ref<number | null>(null);
const REFRESH_INTERVAL_MS = 10_000; // 10s

// ==== 未分配桶徽章(civil_code tab 用)====
const unassignedCount = ref(0);

// ==== 三个 tree 组件的 ref(用于触发 reload)====
const nativeTreeRef = ref<InstanceType<typeof DirectoryTree> | null>(null);
const bizGroupTreeRef = ref<InstanceType<typeof DirectoryTree> | null>(null);
const civilCodeTreeRef = ref<InstanceType<typeof DirectoryTree> | null>(null);

// ==== 播放标题 ====
const currentStreamTitle = computed(() => {
    if (!playing.value) return "未选择通道";
    return `${playing.value.deviceId} / ${playing.value.channelId}`;
});

/**
 * 拉未分配通道数(civil_code tab 徽章)
 * 单独的轻量查询,不影响主目录
 */
async function refreshUnassignedCount() {
    try {
        const res: any = await fetchDirectoryTree({
            dimension: "civil_code",
            withCounts: true
        });
        const list: DirectoryNode[] = res.data?.list || [];
        const bucket = list.find((n) => n.nodeType === "unassigned");
        unassignedCount.value = bucket?.channelCount || 0;
    } catch {
        // 静默失败,徽章保持
    }
}

/**
 * 通道点击 → 触发点播(三 tab 共用)
 */
async function onNodeSelect(node: DirectoryNode) {
    if (node.nodeType !== "channel") return;
    if (!node.channelId || !node.deviceId) return;

    // 已在播同通道 → 提示
    if (playing.value?.channelId === node.channelId) {
        Message.info("当前通道已在播");
        return;
    }

    await ensureStopCurrent();

    playLoading.value = true;
    try {
        const res: any = await startPlay(node.deviceId, node.channelId);
        if (res.code !== 0) {
            Message.error(res.message || "点播失败");
            return;
        }
        playing.value = {
            deviceId: node.deviceId,
            channelId: node.channelId,
            result: res.data
        };
        Message.success(`点播成功 streamId=${res.data.streamId}`);
    } catch (err: any) {
        Message.error(`点播失败: ${err.message || err}`);
    } finally {
        playLoading.value = false;
    }
}

async function ensureStopCurrent() {
    if (!playing.value) return;
    try {
        await stopPlay(playing.value.result.streamId);
    } catch (err) {
        console.warn("停旧流失败,继续", err);
    }
    playing.value = null;
}

async function onStopClick() {
    if (!playing.value) return;
    Modal.confirm({
        title: "停播",
        content: `确认停掉通道 ${playing.value.channelId} ?`,
        onOk: async () => {
            try {
                await stopPlay(playing.value!.result.streamId);
                Message.success("已停播");
            } catch (err: any) {
                Message.warning(`停播请求异常(本地状态已清): ${err.message || err}`);
            } finally {
                playing.value = null;
            }
        }
    });
}

function onPlayerError(msg: string) {
    Message.warning(`播放器: ${msg}`);
}

/**
 * 手动刷新:重载当前 tab 的树
 */
function onManualRefresh() {
    reloadCurrentTree();
    refreshUnassignedCount();
}

function reloadCurrentTree() {
    switch (currentDimension.value) {
        case "native":
            nativeTreeRef.value?.reload();
            break;
        case "biz_group":
            bizGroupTreeRef.value?.reload();
            break;
        case "civil_code":
            civilCodeTreeRef.value?.reload();
            break;
    }
}

/**
 * 自动刷新开关
 */
function startAutoRefresh() {
    if (refreshTimer.value !== null) return;
    refreshTimer.value = window.setInterval(() => {
        reloadCurrentTree();
        refreshUnassignedCount();
    }, REFRESH_INTERVAL_MS);
}

function stopAutoRefresh() {
    if (refreshTimer.value !== null) {
        clearInterval(refreshTimer.value);
        refreshTimer.value = null;
    }
}

function onToggleAutoRefresh(checked: boolean | string | number) {
    autoRefresh.value = !!checked;
    if (checked) {
        startAutoRefresh();
    } else {
        stopAutoRefresh();
    }
}

onMounted(() => {
    refreshUnassignedCount();
    if (autoRefresh.value) startAutoRefresh();
});

onUnmounted(() => {
    stopAutoRefresh();
});
</script>

<template>
    <div class="gb28181-page">
        <a-card class="left" :bordered="false" title="设备目录">
            <template #extra>
                <a-space size="small">
                    <a-switch
                        v-model="autoRefresh"
                        size="small"
                        @change="onToggleAutoRefresh"
                    >
                        <template #checked>自动</template>
                        <template #unchecked>手动</template>
                    </a-switch>
                    <a-button size="mini" @click="onManualRefresh">刷新</a-button>
                </a-space>
            </template>

            <DirectoryDimensionTabs
                v-model="currentDimension"
                :unassigned-count="unassignedCount"
            >
                <template #native>
                    <DirectoryTree
                        ref="nativeTreeRef"
                        dimension="native"
                        @select="onNodeSelect"
                    />
                </template>
                <template #biz_group>
                    <DirectoryTree
                        ref="bizGroupTreeRef"
                        dimension="biz_group"
                        @select="onNodeSelect"
                    >
                        <template #empty>
                            <a-empty>
                                <template #image>
                                    <div class="empty-icon">📂</div>
                                </template>
                                <template #description>
                                    还没有业务分组
                                    <div class="empty-hint">
                                        建组、拖通道、删组请到"分组管理"页
                                    </div>
                                </template>
                            </a-empty>
                        </template>
                    </DirectoryTree>
                </template>
                <template #civil_code>
                    <DirectoryTree
                        ref="civilCodeTreeRef"
                        dimension="civil_code"
                        :with-counts="true"
                        @select="onNodeSelect"
                    >
                        <template #empty>
                            <a-empty>
                                <template #description>
                                    暂无行政区划数据
                                    <div class="empty-hint">
                                        检查下级设备是否上报了 CivilCode 字段
                                    </div>
                                </template>
                            </a-empty>
                        </template>
                    </DirectoryTree>
                </template>
            </DirectoryDimensionTabs>
        </a-card>

        <a-card class="right" :bordered="false">
            <template #title>
                <span v-if="playing">
                    正在播 {{ currentStreamTitle }}
                    <a-tag color="blue" size="small" style="margin-left: 8px">
                        {{ playing.result.streamId }}
                    </a-tag>
                </span>
                <span v-else>播放</span>
            </template>
            <template #extra>
                <a-button
                    v-if="playing"
                    status="danger"
                    size="small"
                    :loading="playLoading"
                    @click="onStopClick"
                >
                    停播
                </a-button>
            </template>
            <a-spin :loading="playLoading" style="display: block">
                <PlayWindow
                    :url="playing?.result.httpFlvUrl || ''"
                    @error="onPlayerError"
                />
                <div v-if="playing" class="play-meta">
                    <div><b>SSRC:</b> {{ playing.result.ssrc }}</div>
                    <div><b>http-flv:</b> {{ playing.result.httpFlvUrl }}</div>
                    <div><b>ws-flv:</b> {{ playing.result.wsflvUrl }}</div>
                    <div><b>HLS:</b> {{ playing.result.hlsUrl }}</div>
                </div>
            </a-spin>
        </a-card>
    </div>
</template>

<style scoped>
.gb28181-page {
    display: grid;
    grid-template-columns: 360px 1fr;
    gap: 12px;
    padding: 12px;
    height: calc(100vh - 100px);
}
.left {
    overflow: hidden;
    display: flex;
    flex-direction: column;
}
.left :deep(.arco-card-body) {
    flex: 1;
    padding: 0;
    overflow: hidden;
}
.right {
    display: flex;
    flex-direction: column;
}
.right :deep(.arco-card-body) {
    flex: 1;
    display: flex;
    flex-direction: column;
}
.play-meta {
    margin-top: 12px;
    font-size: 12px;
    color: #666;
    line-height: 1.6;
    word-break: break-all;
}
.empty-icon {
    font-size: 40px;
    margin-bottom: 8px;
}
.empty-hint {
    font-size: 12px;
    color: var(--color-text-3);
    margin-top: 4px;
}
</style>
