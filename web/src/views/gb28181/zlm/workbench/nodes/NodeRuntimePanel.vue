<script setup lang="ts">
import { useRouter } from "vue-router";
import RuntimeSummaryPanel from "../monitoring/RuntimeSummaryPanel.vue";

const props = withDefaults(defineProps<{
  nodeId: number;
  active?: boolean;
}>(), { active: true });

const router = useRouter();

function openMonitoring(view: "streams" | "sessions") {
  void router.push({ path: view === "streams" ? "/gb28181/zlm/streams" : "/gb28181/zlm/sessions", query: { nodeId: String(props.nodeId) } });
}
</script>

<template>
  <!-- T8 owns runtime polling/chart lifecycle; this adapter only binds the canonical route id. -->
  <RuntimeSummaryPanel
    :active="active"
    :node-id="nodeId"
    @drilldown="openMonitoring"
  />
</template>
