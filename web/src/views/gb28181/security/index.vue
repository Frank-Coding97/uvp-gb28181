<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { AlertTriangle, Ban, CheckCircle2, Pause, Play, RefreshCw, ShieldCheck, WifiOff } from "lucide-vue-next";
import { buildSecurityStreamUrl, getSecurityPolicy, getSecuritySnapshot, listSecurityBans, listSecurityEvents, unbanSecurity, updateSecurityPolicy, type FirewallBan, type SecurityPolicy } from "@/api/gb28181-security";
import { applySecuritySnapshot, createSecurityState, setSecurityDegraded } from "./securityState";

const state = reactive(createSecurityState());
const policyForm = ref<SecurityPolicy | null>(null);
const savingPolicy = ref(false);
const refreshing = ref(false);
let source: EventSource | null = null;
let pollTimer: number | null = null;

const modeLabel = computed(() => ({ observe: "观察", protect: "保护", strict: "严格" }[state.snapshot?.mode || "observe"]));
const activeBans = computed(() => state.bans.filter(item => item.status === "active"));
const latestEvents = computed(() => state.events.slice().sort((a, b) => new Date(b.lastSeenAt).getTime() - new Date(a.lastSeenAt).getTime()).slice(0, 8));

async function refresh() {
  if (state.paused) return;
  refreshing.value = true; state.loading = true;
  try {
    const [snapshot, events, bans, policy] = await Promise.all([getSecuritySnapshot(), listSecurityEvents({ limit: 100 }), listSecurityBans(), getSecurityPolicy()]);
    if (snapshot.code === 0 && snapshot.data) applySecuritySnapshot(state, snapshot.data);
    if (events.code === 0) state.events = events.data?.items || [];
    if (bans.code === 0) state.bans = bans.data?.items || [];
    if (policy.code === 0) policyForm.value = policy.data || null;
    setSecurityDegraded(state, false);
  } catch (error: any) { setSecurityDegraded(state, true); Message.warning(error?.message || "安全状态暂时不可用"); }
  finally { refreshing.value = false; state.loading = false; }
}
function startPolling() { stopPolling(); pollTimer = window.setInterval(refresh, 10000); }
function stopPolling() { if (pollTimer !== null) { window.clearInterval(pollTimer); pollTimer = null; } }
function connectStream() {
  source?.close(); source = new EventSource(buildSecurityStreamUrl());
  source.addEventListener("snapshot", event => { try { applySecuritySnapshot(state, JSON.parse((event as MessageEvent).data)); setSecurityDegraded(state, false); } catch { setSecurityDegraded(state, true); } });
  source.onerror = () => { setSecurityDegraded(state, true); source?.close(); source = null; startPolling(); };
}
function togglePaused() { state.paused = !state.paused; if (!state.paused) refresh(); }
function confirmUnban(item: FirewallBan) { Modal.warning({ title: "确认解封", content: `确定解封 ${item.decision.sourceIp} 吗？`, hideCancel: false, onOk: async () => { await unbanSecurity(item.decision.decisionId); Message.success("已提交解封"); await refresh(); } }); }
async function savePolicy() { if (!policyForm.value) return; savingPolicy.value = true; try { const result = await updateSecurityPolicy(policyForm.value); if (result.code === 0) Message.success("策略已保存"); else Message.error(result.message || "策略保存失败"); } finally { savingPolicy.value = false; } }
onMounted(async () => { await refresh(); connectStream(); });
onBeforeUnmount(() => { source?.close(); stopPolling(); });
</script>

<template>
  <div class="security-page">
    <header class="page-header">
      <div><div class="eyebrow">GB28181 / SIP</div><h1>国标接入安全</h1><p>观察公网信令、临时封禁和主机 agent 状态。</p></div>
      <div class="header-actions"><a-button :loading="refreshing" aria-label="刷新安全状态" @click="refresh"><template #icon><RefreshCw :size="16" /></template>刷新</a-button><a-button :type="state.paused ? 'primary' : 'secondary'" :aria-label="state.paused ? '恢复实时刷新' : '暂停实时刷新'" @click="togglePaused"><template #icon><Play v-if="state.paused" :size="16" /><Pause v-else :size="16" /></template>{{ state.paused ? "恢复" : "暂停" }}</a-button></div>
    </header>
    <a-alert v-if="state.degraded" type="warning" class="degraded" role="status"><template #icon><WifiOff :size="16" /></template>实时通道不可用，正在使用 10 秒轮询。</a-alert>
    <section class="summary-grid" aria-label="安全概览">
      <div class="metric"><span>保护模式</span><strong>{{ modeLabel }}</strong><small>{{ state.snapshot?.mode || "observe" }}</small></div>
      <div class="metric"><span>已拦截</span><strong>{{ state.snapshot?.dropped ?? 0 }}</strong><small>当前运行周期</small></div>
      <div class="metric"><span>临时封禁</span><strong>{{ activeBans.length }}</strong><small>动态规则</small></div>
      <div class="metric"><span>Agent</span><strong :class="state.agent.connected ? 'ok' : 'warn'">{{ state.agent.connected ? "在线" : "降级" }}</strong><small>{{ state.agent.appliedRules }} 条规则</small></div>
    </section>
    <div class="content-grid">
      <section class="panel events-panel"><div class="panel-title"><div><h2>最近事件</h2><span>聚合展示，不保存未知来源原始报文</span></div><ShieldCheck :size="20" /></div><a-table :data="latestEvents" :pagination="false" row-key="sourceIp" :scroll="{ x: 680 }"><template #columns><a-table-column title="来源" data-index="sourceIp" /><a-table-column title="方法" data-index="method" /><a-table-column title="原因" data-index="reason" /><a-table-column title="动作" data-index="action" /><a-table-column title="次数" data-index="count" /></template></a-table></section>
      <section class="panel bans-panel"><div class="panel-title"><div><h2>临时封禁</h2><span>所有自动规则均有 TTL</span></div><Ban :size="20" /></div><a-empty v-if="!activeBans.length" description="暂无生效中的封禁" /><div v-for="ban in activeBans" :key="ban.decision.decisionId" class="ban-row"><div><strong>{{ ban.decision.sourceIp }}</strong><small>{{ ban.decision.reason }} · {{ ban.agentState }}</small></div><a-button type="text" size="small" aria-label="解封来源地址" @click="confirmUnban(ban)">解封</a-button></div></section>
      <section class="panel policy-panel"><div class="panel-title"><div><h2>策略</h2><span>默认 observe，保存前请确认</span></div><AlertTriangle :size="20" /></div><a-form v-if="policyForm" layout="vertical"><a-form-item field="mode" label="保护模式"><a-select v-model="policyForm.mode"><a-option value="observe">观察</a-option><a-option value="protect">保护</a-option><a-option value="strict">严格</a-option></a-select></a-form-item><a-form-item field="banScore" label="封禁评分阈值"><a-input-number v-model="policyForm.banScore" :min="1" /></a-form-item><a-form-item field="maxPacketBytes" label="最大报文字节数"><a-input-number v-model="policyForm.maxPacketBytes" :min="1024" /></a-form-item><a-button type="primary" :loading="savingPolicy" @click="savePolicy">保存策略</a-button></a-form><a-empty v-else description="策略加载中" /></section>
    </div>
    <footer class="health-line"><CheckCircle2 :size="16" /> 最近刷新：{{ state.snapshot?.asOf || "--" }}</footer>
  </div>
</template>

<style scoped>
.security-page { min-height: 100%; padding: 24px; color: var(--color-text-1); background: var(--color-fill-1); }
.page-header { display:flex; justify-content:space-between; gap:24px; align-items:flex-start; margin-bottom:20px; }.eyebrow { color:var(--color-text-3); font-size:12px; letter-spacing:1px; }.page-header h1 { margin:4px 0 8px; font-size:24px; }.page-header p { margin:0; color:var(--color-text-3); }.header-actions { display:flex; gap:8px; }.degraded { margin-bottom:16px; }.summary-grid { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:12px; margin-bottom:16px; }.metric,.panel { background:var(--color-bg-2); border:1px solid var(--color-border-2); border-radius:8px; }.metric { padding:16px; min-height:100px; display:flex; flex-direction:column; gap:8px; }.metric span,.metric small,.panel-title span,.ban-row small { color:var(--color-text-3); font-size:12px; }.metric strong { font-size:24px; }.metric .ok { color:rgb(22 163 74); }.metric .warn { color:rgb(217 119 6); }.content-grid { display:grid; grid-template-columns:minmax(0,1.5fr) minmax(280px,1fr); gap:16px; }.panel { padding:16px; }.events-panel { grid-row:span 2; }.panel-title { display:flex; justify-content:space-between; align-items:flex-start; margin-bottom:14px; }.panel-title h2 { margin:0 0 4px; font-size:16px; }.ban-row { display:flex; justify-content:space-between; align-items:center; padding:12px 0; border-top:1px solid var(--color-border-2); }.ban-row div { display:flex; flex-direction:column; gap:4px; }.health-line { display:flex; gap:6px; align-items:center; color:var(--color-text-3); font-size:12px; margin-top:16px; }.health-line :deep(svg) { color:rgb(22 163 74); }
@media (max-width: 768px) { .security-page { padding:16px; }.page-header { flex-direction:column; }.header-actions { width:100%; }.header-actions .arco-btn { flex:1; }.summary-grid { grid-template-columns:repeat(2,minmax(0,1fr)); }.content-grid { grid-template-columns:1fr; }.events-panel { grid-row:auto; } }
@media (prefers-reduced-motion: reduce) { * { transition:none !important; animation:none !important; } }
</style>
