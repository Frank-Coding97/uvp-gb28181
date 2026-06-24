<script setup lang="ts">
import { ref, onMounted, computed } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import {
    listDevices,
    listChannels,
    startPlay,
    stopPlay,
    type GbDevice,
    type GbChannel,
    type PlayResult
} from "@/api/gb28181";
import PlayWindow from "./components/PlayWindow.vue";

// ===== 数据 =====

const devices = ref<GbDevice[]>([]);
const channelsCache = ref<Record<string, GbChannel[]>>({});
const treeLoading = ref(false);
const playLoading = ref(false);

const playing = ref<{
    deviceId: string;
    channelId: string;
    result: PlayResult;
} | null>(null);

interface TreeNode {
    key: string;
    title: string;
    isLeaf?: boolean;
    icon?: string;
    deviceId?: string;
    channelId?: string;
    online?: boolean;
    raw?: GbDevice | GbChannel;
}

// 树构建:第一层 = 设备,第二层 = 通道(懒加载)
const treeData = computed<TreeNode[]>(() =>
    devices.value.map((d) => ({
        key: `dev:${d.deviceId}`,
        title: `${d.deviceId}${d.name ? ` (${d.name})` : ""}`,
        deviceId: d.deviceId,
        online: d.online,
        raw: d,
        children: (channelsCache.value[d.deviceId] || []).map<TreeNode>((c) => ({
            key: `ch:${d.deviceId}:${c.channelId}`,
            title: `${c.channelId}${c.name ? ` ${c.name}` : ""}`,
            isLeaf: true,
            deviceId: d.deviceId,
            channelId: c.channelId,
            online: c.status === 1,
            raw: c
        }))
    }))
);

// ===== 拉数据 =====

async function loadDevices() {
    treeLoading.value = true;
    try {
        const res: any = await listDevices({ page: 1, pageSize: 100 });
        devices.value = res.data?.list || [];
    } catch (err: any) {
        Message.error(`拉设备列表失败: ${err.message || err}`);
    } finally {
        treeLoading.value = false;
    }
}

async function onLoadMore(node: TreeNode) {
    if (!node.deviceId) return;
    if (channelsCache.value[node.deviceId]) return;
    try {
        const res: any = await listChannels(node.deviceId);
        channelsCache.value[node.deviceId] = res.data?.list || [];
    } catch (err: any) {
        Message.error(`拉通道失败: ${err.message || err}`);
    }
}

// ===== 点播 / 停播 =====

async function onTreeSelect(selectedKeys: string[]) {
    if (!selectedKeys || !selectedKeys.length) return;
    const key = String(selectedKeys[0]);
    if (!key.startsWith("ch:")) return; // 只处理通道叶子,设备节点交给展开
    // key 格式: ch:<deviceId>:<channelId>
    const parts = key.split(":");
    if (parts.length < 3) return;
    await onChannelClick({
        key,
        title: "",
        isLeaf: true,
        deviceId: parts[1],
        channelId: parts[2]
    } as TreeNode);
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

async function onChannelClick(node: TreeNode) {
    if (!node.channelId || !node.deviceId) return;
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

onMounted(loadDevices);
</script>

<template>
    <div class="gb28181-page">
        <a-card class="left" :bordered="false" title="设备 / 通道">
            <template #extra>
                <a-button size="mini" @click="loadDevices">刷新</a-button>
            </template>
            <a-spin :loading="treeLoading" style="display: block">
                <a-tree
                    :data="treeData as any"
                    :load-more="onLoadMore as any"
                    :default-expanded-keys="[]"
                    @select="onTreeSelect"
                >
                    <template #title="nodeData">
                        <span :class="{ offline: nodeData.online === false }">{{ nodeData.title }}</span>
                        <a-tag v-if="nodeData.online" color="green" size="small" style="margin-left: 6px">在线</a-tag>
                        <a-tag v-else-if="nodeData.online === false" color="gray" size="small" style="margin-left: 6px">离线</a-tag>
                    </template>
                </a-tree>
            </a-spin>
        </a-card>

        <a-card class="right" :bordered="false">
            <template #title>
                <span v-if="playing">
                    正在播 {{ playing.channelId }}
                    <a-tag color="blue" size="small" style="margin-left: 8px">{{ playing.result.streamId }}</a-tag>
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
    overflow: auto;
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

.offline {
    color: #999;
}
</style>
