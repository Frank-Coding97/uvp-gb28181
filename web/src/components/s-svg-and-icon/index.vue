<template>
  <component
    v-if="lucideIcon"
    :is="lucideIcon"
    class="uvp-lucide-icon"
    :size="22"
    :stroke-width="2"
    :absolute-stroke-width="true"
  />
  <s-svg-icon v-else-if="props.svgIcon" :name="props.svgIcon" :size="25" />
  <component v-else-if="props.icon" :is="props.icon" :size="25"></component>
</template>

<script setup lang="ts">
import { getLucideIconComponent } from "@/utils/lucide-menu-icons";

const props = defineProps({
  svgIcon: {
    type: String,
    default: ""
  },
  icon: {
    type: String,
    default: ""
  }
});

const lucideIcon = computed(() => getLucideIconComponent(props.icon));
</script>

<style lang="scss" scoped>
.uvp-lucide-icon {
  display: block;
}
</style>
