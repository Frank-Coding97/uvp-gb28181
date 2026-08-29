<script setup lang="ts">
import { computed, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { kickZLMSession, type ZLMStreamViewer } from "@/api/gb28181-zlm-runtime";
import ZLMDangerActionDialog from "./components/ZLMDangerActionDialog.vue";
import { streamIdentityKey } from "./streamManagementState";

const props = defineProps<{
  visible: boolean;
  nodeName: string;
  viewer: ZLMStreamViewer | null;
}>();

const emit = defineEmits<{
  "update:visible": [visible: boolean];
  done: [result: { kicked: boolean; alreadyDisconnected: boolean; uncertain: boolean }];
}>();

const busy = ref(false);
const targetKey = computed(() => props.viewer
  ? `${streamIdentityKey(props.viewer.nodeId, props.viewer.media)}\u001f${props.viewer.identifier}`
  : "");
const targetLabel = computed(() => props.viewer
  ? `${props.viewer.media.app}/${props.viewer.media.stream} · ${props.viewer.identifier}`
  : "未选择观看者");
const impacts = computed(() => props.viewer
  ? [
      `媒体：${props.viewer.media.schema}://${props.viewer.media.vhost}/${props.viewer.media.app}/${props.viewer.media.stream}`,
      `远端：${props.viewer.peerIp}:${props.viewer.peerPort}`,
      `本地：${props.viewer.localIp}:${props.viewer.localPort}`,
      "仅踢除当前后端返回且标记 kickable 的观看会话"
    ]
  : []);

function close() {
  emit("update:visible", false);
}

async function confirm(payload: { nodeId: number; targetKey: string }) {
  const viewer = props.viewer;
  if (!viewer || payload.nodeId !== viewer.nodeId || payload.targetKey !== targetKey.value || !viewer.kickable) {
    Message.error("观看会话快照已变化，未执行踢除");
    close();
    return;
  }
  busy.value = true;
  try {
    const response = await kickZLMSession(viewer.nodeId, viewer.media, viewer.identifier);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "踢除观看会话失败");
    emit("done", response.data);
    close();
  } catch (error) {
    Message.error((error as Error)?.message || "踢除观看会话失败");
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <ZLMDangerActionDialog
    v-if="viewer"
    :visible="visible"
    :node-id="viewer.nodeId"
    :node-name="nodeName"
    :target-key="targetKey"
    :target-label="targetLabel"
    :impacts="impacts"
    :confirm-phrase="`踢除 ${viewer.identifier}`"
    :require-reason="false"
    action-label="确认踢除"
    :busy="busy"
    @confirm="confirm"
    @stale="close"
    @update:visible="emit('update:visible', $event)"
  />
</template>
