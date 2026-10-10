<script setup lang="ts">
import DeviceConfigOsdBlocks from "../../device-mgmt/DeviceConfigOsdBlocks.vue";

type OsdBlocksBag = {
  timeEnable: boolean;
  timeType: string;
  timeX: string;
  timeY: string;
  textEnable: boolean;
  items: import("../../device-mgmt/deviceConfigGroups").ConfigTextItem[];
  canvas: { width: number; height: number } | null;
  disabled: boolean;
  maxItems: number;
};

defineProps<{ bag: OsdBlocksBag; editing: boolean }>();
const emit = defineEmits<{
  (e: "update:time-enable", value: boolean): void;
  (e: "update:time-type", value: string): void;
  (e: "update:time-x", value: string): void;
  (e: "update:time-y", value: string): void;
  (e: "update:text-enable", value: boolean): void;
  (e: "update:items", value: import("../../device-mgmt/deviceConfigGroups").ConfigTextItem[]): void;
  (e: "locate", value: { kind: "time" | "item"; index: number }): void;
  (e: "toggleEdit"): void;
}>();
</script>

<template>
  <section class="linked-section picture-osd-cell" data-testid="picture-osd-cell">
    <DeviceConfigOsdBlocks
      v-bind="bag"
      :editing="editing"
      :canvas-linked="true"
      layout="row"
      @update:time-enable="emit('update:time-enable', $event)"
      @update:time-type="emit('update:time-type', $event)"
      @update:time-x="emit('update:time-x', $event)"
      @update:time-y="emit('update:time-y', $event)"
      @update:text-enable="emit('update:text-enable', $event)"
      @update:items="emit('update:items', $event)"
      @locate="emit('locate', $event)"
      @toggle-edit="emit('toggleEdit')"
    />
  </section>
</template>
