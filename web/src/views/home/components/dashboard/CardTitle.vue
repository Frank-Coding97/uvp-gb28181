<template><header class="card-title"><component :is="iconComponent" :size="15" /><strong>{{ title }}</strong><span /></header></template>
<script setup lang="ts">
import { computed } from "vue";
import { Activity, CircleGauge, HeartPulse, ListOrdered, Network, PlayCircle, RadioTower, Server, TrafficCone, Video } from "lucide-vue-next";
const props = defineProps<{ icon: string; title: string }>();
const icons: Record<string, any> = { activity: Activity, radio: RadioTower, play: PlayCircle, traffic: TrafficCone, server: Server, device: RadioTower, channel: Video, rate: Network, health: HeartPulse, ranking: ListOrdered };
const iconComponent = computed(() => icons[props.icon] ?? CircleGauge);
</script>
<style scoped>.card-title{display:flex;gap:8px;align-items:center;height:20px;font-size:13px;color:var(--uvp-text-secondary)}.card-title strong{font-weight:600}.card-title span{flex:1;border-top:1px solid var(--uvp-panel-border)}</style>
