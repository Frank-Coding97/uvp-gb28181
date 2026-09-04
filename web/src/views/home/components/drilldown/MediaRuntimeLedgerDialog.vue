<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { MediaRuntimeLedger, MediaRuntimeLedgerKind } from "../../dashboardDrilldownState";

const props = defineProps<{ visible: boolean; kind: MediaRuntimeLedgerKind; ledger: MediaRuntimeLedger }>();
const emit = defineEmits<{ close: []; kind: [value: MediaRuntimeLedgerKind] }>();
const query = ref("");
const page = ref(1);
const pageSize = 20;
const kinds: Array<{ key: MediaRuntimeLedgerKind; label: string }> = [
  { key: "streams", label: "在线流" }, { key: "viewers", label: "观看者" },
  { key: "sessions", label: "网络会话" }, { key: "recordings", label: "录制中" }
];
const title = computed(() => `流媒体运行态 · ${kinds.find(item => item.key === props.kind)?.label ?? "在线流"}`);
const rows = computed(() => {
  if (props.kind === "sessions") {
    return props.ledger.sessions.map(item => ({ key: `node-${item.nodeId}`, primary: item.nodeName, secondary: `节点 ${item.nodeId}`, value: item.sampled ? `${item.sessions} 个会话` : "未采样", detail: item.sampled ? "已采样" : "覆盖不足" }));
  }
  const source = props.kind === "viewers" ? props.ledger.viewers : props.kind === "recordings" ? props.ledger.recordings : props.ledger.streams;
  return source.map(item => ({
    key: `${item.nodeId}/${item.media.schema}/${item.media.vhost}/${item.media.app}/${item.media.stream}`,
    primary: item.media.stream,
    secondary: `${item.media.app} · ${item.media.schema} · 节点 ${item.nodeId}`,
    value: props.kind === "viewers" ? `${Math.max(0, item.readerCount)} 位观看者` : props.kind === "recordings" ? [item.recordingMp4 && "MP4", item.recordingHls && "HLS"].filter(Boolean).join(" + ") : `${Math.max(0, item.readerCount)} 位观看者`,
    detail: `${Math.max(0, item.bytesSpeed).toLocaleString("zh-CN")} B/s`
  }));
});
const filteredRows = computed(() => {
  const keyword = query.value.trim().toLowerCase();
  return keyword ? rows.value.filter(item => `${item.primary} ${item.secondary} ${item.value}`.toLowerCase().includes(keyword)) : rows.value;
});
const pageCount = computed(() => Math.max(1, Math.ceil(filteredRows.value.length / pageSize)));
const visibleRows = computed(() => filteredRows.value.slice((page.value - 1) * pageSize, page.value * pageSize));
watch(() => [props.kind, props.visible], () => { query.value = ""; page.value = 1; });
watch(query, () => { page.value = 1; });
</script>

<template>
  <a-modal :visible="visible" modal-class="uvp-system-dialog" :title="title" :width="900" :footer="false" unmount-on-close @cancel="emit('close')">
    <div class="media-ledger-dialog">
      <div class="media-ledger-toolbar">
        <div class="media-ledger-tabs" role="tablist" aria-label="运行态台账类型">
          <button v-for="item in kinds" :key="item.key" type="button" role="tab" :aria-selected="kind === item.key" :class="{ active: kind === item.key }" @click="emit('kind', item.key)">{{ item.label }} {{ ledger.totals[item.key] }}</button>
        </div>
        <input v-model="query" type="search" placeholder="搜索流、应用或节点" aria-label="搜索运行态台账" />
      </div>
      <div v-if="ledger.partial" class="media-ledger-warning" role="status">
        当前快照覆盖不完整<span v-if="ledger.warnings.length">：{{ ledger.warnings.join("；") }}</span>
      </div>
      <p class="media-ledger-meta">快照时间 {{ ledger.asOf || "未知" }} · 共 {{ filteredRows.length.toLocaleString("zh-CN") }} 条</p>
      <div class="media-ledger-list">
        <article v-for="row in visibleRows" :key="row.key"><div><strong>{{ row.primary }}</strong><small>{{ row.secondary }}</small></div><div><strong>{{ row.value }}</strong><small>{{ row.detail }}</small></div></article>
        <p v-if="!visibleRows.length" class="media-ledger-empty">当前条件下暂无台账</p>
      </div>
      <footer v-if="pageCount > 1"><button type="button" :disabled="page === 1" @click="page--">上一页</button><span>{{ page }} / {{ pageCount }}</span><button type="button" :disabled="page === pageCount" @click="page++">下一页</button></footer>
    </div>
  </a-modal>
</template>

<style scoped>
.media-ledger-dialog{display:grid;gap:12px;max-height:min(74vh,700px);overflow-y:auto;color:var(--uvp-text-primary)}.media-ledger-toolbar{display:flex;align-items:center;justify-content:space-between;gap:12px}.media-ledger-tabs{display:flex;flex-wrap:wrap;gap:6px}.media-ledger-tabs button,.media-ledger-dialog footer button{padding:6px 10px;color:var(--uvp-text-secondary);cursor:pointer;background:var(--uvp-panel-bg);border:1px solid var(--uvp-panel-border);border-radius:7px}.media-ledger-tabs button.active{color:#fff;background:var(--uvp-brand);border-color:var(--uvp-brand)}.media-ledger-toolbar input{min-width:210px;padding:7px 10px;color:var(--uvp-text-primary);background:var(--uvp-panel-bg);border:1px solid var(--uvp-panel-border);border-radius:7px}.media-ledger-warning{padding:8px 10px;color:var(--uvp-warning);background:color-mix(in srgb,var(--uvp-warning) 10%,transparent);border-radius:7px}.media-ledger-meta{margin:0;color:var(--uvp-text-tertiary);font-size:12px}.media-ledger-list{border:1px solid var(--uvp-panel-border);border-radius:8px}.media-ledger-list article{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:10px 12px;border-bottom:1px solid var(--uvp-panel-border)}.media-ledger-list article:last-child{border-bottom:0}.media-ledger-list article>div{display:flex;min-width:0;flex-direction:column;gap:3px}.media-ledger-list article>div:last-child{align-items:flex-end;text-align:right}.media-ledger-list strong,.media-ledger-list small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.media-ledger-list small{color:var(--uvp-text-tertiary)}.media-ledger-empty{padding:32px;text-align:center;color:var(--uvp-text-tertiary)}.media-ledger-dialog footer{display:flex;align-items:center;justify-content:flex-end;gap:10px}.media-ledger-dialog footer button:disabled{cursor:not-allowed;opacity:.5}
@media(max-width:650px){.media-ledger-toolbar{align-items:stretch;flex-direction:column}.media-ledger-toolbar input{box-sizing:border-box;width:100%}.media-ledger-list article{align-items:flex-start;flex-direction:column}.media-ledger-list article>div:last-child{align-items:flex-start;text-align:left}}
</style>
