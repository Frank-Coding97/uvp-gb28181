<script setup lang="ts">
/**
 * 通道快照缩略图单元格
 *
 * 通道列表 / ControlConsole 卡片共用,展示"最近一次播放触发抓的 JPEG"。
 * - 有 snapshotUrl → <a-image> 点击放大 + hover tooltip 显示相对时间
 * - 无 snapshotUrl → 灰色摄像头占位
 */
import { computed } from "vue";
import { IconVideoCamera } from "@arco-design/web-vue/es/icon";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import "dayjs/locale/zh-cn";

dayjs.extend(relativeTime);
dayjs.locale("zh-cn");

interface ChannelWithSnapshot {
    snapshotUrl?: string | null;
    snapshotAt?: string | null;
}

interface Props {
    channel: ChannelWithSnapshot;
    /** sm = 100x60(通道列表列), lg = 160x90(ControlConsole 卡片) */
    size?: "sm" | "lg";
}

const props = withDefaults(defineProps<Props>(), { size: "sm" });

const dimensions = computed(() => {
    if (props.size === "lg") return { w: 160, h: 90 };
    return { w: 100, h: 60 };
});

const hasSnapshot = computed(() => {
    const url = props.channel?.snapshotUrl;
    return typeof url === "string" && url.length > 0;
});

// 相对访问的 URL,如 /public/gb-channel-snapshot/2026-07/xxx.jpg
// 项目走同源 nginx 反代,直接用相对路径即可
const fullUrl = computed(() => props.channel?.snapshotUrl ?? "");

const capturedAgo = computed(() => {
    const at = props.channel?.snapshotAt;
    if (!at) return "";
    return dayjs(at).fromNow();
});

const capturedAbs = computed(() => {
    const at = props.channel?.snapshotAt;
    if (!at) return "";
    return dayjs(at).format("YYYY-MM-DD HH:mm:ss");
});
</script>

<template>
    <div class="channel-snapshot-cell" :style="{ width: `${dimensions.w}px`, height: `${dimensions.h}px` }">
        <template v-if="hasSnapshot">
            <a-tooltip :content="`抓拍于 ${capturedAgo}(${capturedAbs})`" position="top">
                <a-image
                    :src="fullUrl"
                    :width="dimensions.w"
                    :height="dimensions.h"
                    fit="cover"
                    show-loader
                    :preview="true"
                    class="channel-snapshot-img"
                />
            </a-tooltip>
        </template>
        <template v-else>
            <div class="channel-snapshot-placeholder">
                <IconVideoCamera :size="size === 'lg' ? 32 : 20" />
                <div v-if="size === 'lg'" class="channel-snapshot-placeholder__text">暂无快照</div>
            </div>
        </template>
    </div>
</template>

<style lang="less" scoped>
.channel-snapshot-cell {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background-color: #f5f6f7;
    border-radius: 4px;
    overflow: hidden;

    :deep(.arco-image),
    :deep(.arco-image-img) {
        border-radius: 4px;
    }
}

.channel-snapshot-img {
    object-fit: cover;
    cursor: zoom-in;
}

.channel-snapshot-placeholder {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    color: #a8abb2;
    width: 100%;
    height: 100%;

    &__text {
        margin-top: 4px;
        font-size: 12px;
    }
}
</style>
