<template>
  <template v-for="(run, index) in runs" :key="index"
    ><code v-if="run.code">{{ run.text }}</code
    ><strong v-else-if="run.bold">{{ run.text }}</strong
    ><template v-else>{{ run.text }}</template></template
  >
</template>

<script setup lang="ts">
import type { ReleaseNoteRun } from "./releaseNotes";

defineProps<{ runs: ReleaseNoteRun[] }>();
</script>
