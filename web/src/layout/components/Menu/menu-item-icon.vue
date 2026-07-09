<template>
  <component
    v-if="lucideIcon"
    :is="lucideIcon"
    class="uvp-lucide-menu-icon"
    :size="18"
    :stroke-width="2"
    :absolute-stroke-width="true"
  />
  <s-svg-icon v-else-if="props.svgIcon" :name="props.svgIcon" :size="25" />
  <component v-else-if="props.icon" :is="props.icon"></component>
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
.uvp-lucide-menu-icon {
  display: block;
}
</style>
