<script setup lang="ts">
import { computed } from "vue";
import type { ZLMNodeState } from "@/api/gb28181-zlm";

const props = defineProps<{
    state: ZLMNodeState;
}>();

const config = computed(() => {
    switch (props.state) {
        case "active":
            return { color: "green", text: "活跃" };
        case "maintenance":
            return { color: "orange", text: "维护中" };
        case "offline":
            return { color: "gray", text: "离线" };
        default:
            return { color: "gray", text: props.state };
    }
});
</script>

<template>
    <a-tag :color="config.color">{{ config.text }}</a-tag>
</template>
