<script setup lang="ts">
import { computed } from "vue";
import type { ZLMNodeState } from "@/api/gb28181-zlm";
import { useDictLabel } from "@/hooks/useDictOptions";
import { DICT_CODE_MEDIA_NODE_STATE, MEDIA_NODE_STATE_LABEL_FALLBACK } from "../../mediaNodeState";

const props = defineProps<{
  state: ZLMNodeState;
}>();

/** 文案走 `media_node_state` 字典；**语义色留代码**（颜色不是字典值域）。 */
const stateLabel = useDictLabel(DICT_CODE_MEDIA_NODE_STATE, MEDIA_NODE_STATE_LABEL_FALLBACK);

const color = computed(() => {
  switch (props.state) {
    case "active":
      return "green";
    case "maintenance":
      return "orange";
    case "offline":
      return "gray";
    default:
      return "gray";
  }
});
</script>

<template>
  <a-tag :color="color">{{ stateLabel(state) }}</a-tag>
</template>
