<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import VChart from "@visactor/vchart";
import {
  Activity,
  Ban,
  BrickWall,
  Check,
  CheckCircle2,
  ChevronRight,
  CloudCog,
  Fingerprint,
  Globe2,
  LockKeyhole,
  Pause,
  Play,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
  ShieldOff,
  SlidersHorizontal,
  Trash2,
  TriangleAlert,
  UserRoundCheck,
  Zap
} from "lucide-vue-next";
import {
  createSecurityAccessRule,
  deleteSecurityAccessRule,
  getSecurityAgentHealth,
  getSecurityPolicy,
  getSecuritySnapshot,
  listSecurityAccessRules,
  listSecurityBans,
  listSecurityEvents,
  unbanSecurity,
  updateSecurityAccessRule,
  updateSecurityPolicy,
  type AgentStatus,
  type FirewallBan,
  type SecurityAccessRule,
  type SecurityEventAggregate,
  type SecurityPolicy,
  type SecuritySnapshot
} from "@/api/gb28181-security";
import { buildSecurityTrend, type SecurityTrendPeriod } from "./securityTrend";

type TabKey = "overview" | "events" | "bans" | "blacklist" | "allowlist" | "policy";
type RuleKind = "blacklist" | "allowlist";
type ProtectionMode = "observe" | "protect" | "strict";

interface SecurityEvent {
  id: number;
  severity: "高危" | "中危" | "低危";
  source: string;
  location: string;
  method: string;
  userAgent: string;
  rule: string;
  action: string;
  count: number;
  time: string;
}

interface AccessRule {
  id: number;
  listType: RuleKind;
  matchType: SecurityAccessRule["matchType"];
  type: "IP" | "CIDR" | "User-Agent";
  value: string;
  note: string;
  scope: string;
  expires: string;
  enabled: boolean;
}

interface AutoBanRecord {
  id: number;
  decisionId: string;
  source: string;
  location: string;
  method: string;
  reason: string;
  evidence: string;
  mode: "保护" | "严格";
  firewallState: "已生效" | "待同步" | "同步失败";
  createdAt: string;
  expires: string;
  blocked: number;
}

interface ProtectionModeOption {
  key: ProtectionMode;
  title: string;
  badge: string;
  desc: string;
  headline: string;
  summary: string;
  entryResult: string;
  protocolResult: string;
  banResult: string;
  icon: typeof Globe2;
}

const events = ref<SecurityEvent[]>([]);
const securityEvents = ref<SecurityEventAggregate[]>([]);
const blackRules = ref<AccessRule[]>([]);
const autoBans = ref<AutoBanRecord[]>([]);
const allowRules = ref<AccessRule[]>([]);
const securitySnapshot = ref<SecuritySnapshot | null>(null);
const securityPolicy = ref<SecurityPolicy | null>(null);
const securityAgent = ref<AgentStatus>({ connected: false, appliedRules: 0, lastError: "", checkedAt: undefined });

const activeTab = ref<TabKey>("overview");
const live = ref(true);
const period = ref<SecurityTrendPeriod>("24h");
const trendPeriods: SecurityTrendPeriod[] = ["1h", "24h", "7d"];
const eventSeverity = ref("全部风险");
const eventSearch = ref("");
const protectionModes: ProtectionModeOption[] = [
  {
    key: "observe",
    title: "观察",
    badge: "仅记录",
    desc: "不影响现有 SIP 通信",
    headline: "发现 SIP 风险，但不执行拦截",
    summary: "适合上线前校准规则。公网攻击仍会进入服务，不建议长期用于生产环境。",
    entryResult: "记录入口包大小、来源和未知方法风险，不拦截",
    protocolResult: "记录 REGISTER 的 Digest、Nonce、设备身份风险，以及 MESSAGE 解析结果",
    banResult: "只累计风险，不写入主机防火墙",
    icon: Globe2
  },
  {
    key: "protect",
    title: "保护",
    badge: "推荐",
    desc: "自动拦截明确攻击",
    headline: "陌生 INVITE 与异常入口先拦截，重复风险自动封禁",
    summary: "兼顾公网防护与设备兼容性，适合大多数国标平台。",
    entryResult: "陌生 INVITE、超大包和未知方法风险先丢弃，不进入业务处理",
    protocolResult: "REGISTER 执行 Digest、Nonce 和设备身份校验；MESSAGE 继续经过协议解析链路",
    banResult: "多类风险累计达到阈值后封禁来源 IP，并同步 Agent",
    icon: ShieldCheck
  },
  {
    key: "strict",
    title: "严格",
    badge: "高安全",
    desc: "仅允许可信端点",
    headline: "仅允许认证注册和可信 SIP 端点",
    summary: "适合设备与上级平台来源固定的封闭部署，启用前需要补齐信任关系。",
    entryResult: "对陌生 INVITE、超大包和连接异常更早拒绝，可信来源需提前加入名单",
    protocolResult: "REGISTER 必须通过 Digest、Nonce 和设备身份校验；其他方法仍走协议处理",
    banResult: "任一来源的组合风险更快升级为主机封禁",
    icon: LockKeyhole
  }
];

const selectedMode = ref<ProtectionMode>("protect");
const ruleDrawerVisible = ref(false);
const editingRuleKind = ref<RuleKind>("blacklist");
const ruleType = ref<AccessRule["type"]>("IP");
const ruleValue = ref("");
const ruleNote = ref("");
const ruleExpiry = ref("1 天");
const ruleFormModel = computed(() => ({
  type: ruleType.value,
  value: ruleValue.value,
  note: ruleNote.value,
  expiry: ruleExpiry.value
}));
const maxUdpThreshold = ref(120);
const banThreshold = ref(100);
const lastRefreshAt = ref<string | undefined>();
const dataUnavailable = ref(false);
const chartElement = ref<HTMLElement | null>(null);
let chart: VChart | null = null;
let resizeObserver: ResizeObserver | null = null;
let themeObserver: MutationObserver | null = null;
let refreshTimer: number | null = null;
let refreshInFlight = false;

const filteredEvents = computed(() =>
  events.value.filter(item => {
    const severityMatches = eventSeverity.value === "全部风险" || item.severity === eventSeverity.value;
    const keyword = eventSearch.value.trim().toLowerCase();
    const keywordMatches = !keyword || [item.source, item.method, item.userAgent, item.rule].some(value => value.toLowerCase().includes(keyword));
    return severityMatches && keywordMatches;
  })
);

const currentRules = computed(() => (activeTab.value === "allowlist" ? allowRules.value : blackRules.value));
const selectedModeConfig = computed(() => protectionModes.find(item => item.key === selectedMode.value) ?? protectionModes[1]);
const trendData = computed(() => buildSecurityTrend(securityEvents.value, period.value));
const recognizedCount = computed(() => securityEvents.value.reduce((total, item) => total + Math.max(0, Number(item.count) || 0), 0));
const blockedCount = computed(() => securityEvents.value.reduce((total, item) => total + (["drop", "ban"].includes(item.action) ? Math.max(0, Number(item.count) || 0) : 0), 0));
const attentionCount = computed(() => autoBans.value.length + (allowRules.value.length > 0 ? 1 : 0));
const enabledDefenseLayers = computed(() => 1 + (securityAgent.value.connected ? 1 : 0));
const policyWindowLabel = computed(() => securityPolicy.value ? `${securityPolicy.value.window} 秒窗口` : "策略窗口读取中");
const lastRefreshLabel = computed(() => lastRefreshAt.value ? new Date(lastRefreshAt.value).toLocaleTimeString() : "--");
const liveStatus = computed(() => {
  if (dataUnavailable.value) return { label: "数据不可用", color: "red" as const };
  if (selectedMode.value === "observe") return { label: "仅观察", color: "orange" as const };
  return { label: securityAgent.value.connected ? "防护运行中" : "应用层防护运行中", color: securityAgent.value.connected ? "green" as const : "orange" as const };
});
const agentStatus = computed(() => {
  if (securityAgent.value.connected) {
    return {
      title: "主机防火墙联动正常",
      detail: `最后心跳 ${formatAgentHeartbeat(securityAgent.value.checkedAt)} · 已应用 ${securityAgent.value.appliedRules} 条规则`,
      label: "健康",
      color: "green"
    };
  }
  return {
    title: "主机防火墙未连接",
    detail: securityAgent.value.lastError ? `${securityAgent.value.lastError} · 应用层拦截仍生效` : "尚未收到 Agent 心跳 · 应用层拦截仍生效",
    label: "降级",
    color: "orange"
  };
});

function formatAgentHeartbeat(checkedAt?: string) {
  if (!checkedAt) return "尚未收到";
  const timestamp = Date.parse(checkedAt);
  if (!Number.isFinite(timestamp)) return "时间未知";
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
  if (seconds < 60) return `${seconds} 秒前`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} 分钟前`;
  return `${Math.floor(minutes / 60)} 小时前`;
}

function themeColor(name: string, fallback: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback;
}

function renderTrendChart() {
  if (!chartElement.value || activeTab.value !== "overview" || !trendData.value.hasData) {
    chart?.release();
    chart = null;
    return;
  }
  const brand = themeColor("--uvp-brand", "#2563eb");
  const danger = themeColor("--uvp-danger", "#d14343");
  const tertiary = themeColor("--uvp-text-tertiary", "#6b7280");
  const border = themeColor("--uvp-panel-border", "#e8edf5");
  chart?.release();
  chart = new VChart(
    {
      type: "line",
      data: [{ id: "trend", values: trendData.value.points }],
      xField: "time",
      yField: "value",
      seriesField: "series",
      color: [brand, danger],
      background: "transparent",
      padding: { top: 14, right: 12, bottom: 4, left: 4 },
      point: { visible: false },
      line: { style: { lineWidth: 2 } },
      area: { visible: true, style: { fillOpacity: 0.06 } },
      axes: [
        { orient: "left", grid: { visible: true, style: { stroke: border, lineDash: [3, 4] } }, label: { style: { fill: tertiary, fontSize: 11 } }, domainLine: { visible: false }, tick: { visible: false } },
        { orient: "bottom", label: { style: { fill: tertiary, fontSize: 11 } }, domainLine: { style: { stroke: border } }, tick: { visible: false } }
      ],
      legends: { visible: false },
      tooltip: { mark: { content: [{ key: (datum: { series: string }) => datum.series, value: (datum: { value: number }) => `${datum.value} 次` }] } },
      animationAppear: { duration: 360, easing: "cubicOut" }
    } as any,
    { dom: chartElement.value }
  );
  chart.renderSync();
}

function openRuleDrawer(kind: RuleKind) {
  editingRuleKind.value = kind;
  ruleType.value = kind === "blacklist" ? "IP" : "CIDR";
  ruleValue.value = "";
  ruleNote.value = "";
  ruleExpiry.value = kind === "blacklist" ? "1 天" : "永久";
  ruleDrawerVisible.value = true;
}

function formatRemaining(createdAt: string, ttl: number) {
  const ttlMs = ttl > 1_000_000_000 ? ttl / 1_000_000 : ttl * 1_000;
  const remaining = Math.max(0, new Date(createdAt).getTime() + ttlMs - Date.now());
  if (remaining <= 0) return "已过期";
  const minutes = Math.floor(remaining / 60000);
  return minutes >= 60 ? `剩余 ${Math.floor(minutes / 60)} 小时` : `剩余 ${Math.max(1, minutes)} 分钟`;
}

function uiRuleType(type: SecurityAccessRule["matchType"]): AccessRule["type"] {
  return type === "user_agent" ? "User-Agent" : type.toUpperCase() as AccessRule["type"];
}

function mapRule(rule: SecurityAccessRule): AccessRule {
  return { id: rule.id, listType: rule.listType, matchType: rule.matchType, type: uiRuleType(rule.matchType), value: rule.matchValue, note: rule.note, scope: rule.scope, expires: rule.expiresAt ? new Date(rule.expiresAt).toLocaleString() : "永久", enabled: rule.status === "enabled" };
}

function expiryToIso(expiry: string) {
  const durations: Record<string, number> = { "10 分钟": 10 * 60_000, "1 小时": 60 * 60_000, "1 天": 24 * 60 * 60_000, "7 天": 7 * 24 * 60 * 60_000 };
  const duration = durations[expiry];
  return duration ? new Date(Date.now() + duration).toISOString() : undefined;
}

function mapEvent(event: SecurityEventAggregate, index: number): SecurityEvent {
  const highRisk = event.action === "ban" || event.reason.includes("nonce") || event.reason.includes("digest");
  return { id: index + 1, severity: highRisk ? "高危" : event.action === "drop" ? "中危" : "低危", source: event.sourceIp, location: "公网来源", method: event.method || "未知", userAgent: event.userAgent || "未上报", rule: event.reason, action: event.action, count: event.count, time: new Date(event.lastSeenAt).toLocaleString() };
}

function mapBan(ban: FirewallBan, index: number): AutoBanRecord {
  const decision = ban.decision;
  const applied = ban.agentState === "applied";
  const permanent = decision.permanent === true || decision.ttl === 0;
  return { id: index + 1, decisionId: decision.decisionId, source: decision.sourceIp, location: "公网来源", method: decision.triggerMethod || "未知", reason: decision.reason, evidence: `${decision.windowSeconds || 0} 秒内 ${decision.triggerCount || 0} 次，阈值 ${decision.triggerThreshold || decision.score}，累计评分 ${decision.score}`, mode: decision.policyMode === "strict" ? "严格" : "保护", firewallState: applied ? "已生效" : ban.agentState === "failed" ? "同步失败" : "待同步", createdAt: new Date(decision.createdAt).toLocaleTimeString(), expires: permanent ? "永久，人工解封" : formatRemaining(decision.createdAt, decision.ttl), blocked: ban.blockedCountAfterBan || 0 };
}

async function refreshPreview(showMessage = false) {
  if (refreshInFlight) return;
  refreshInFlight = true;
  try {
    const [snapshot, eventResult, banResult, policy, black, allow, agentHealth] = await Promise.all([getSecuritySnapshot(), listSecurityEvents({ limit: 500 }), listSecurityBans(), getSecurityPolicy(), listSecurityAccessRules("blacklist"), listSecurityAccessRules("allowlist"), getSecurityAgentHealth()]);
    const successful = [snapshot, eventResult, banResult, policy, black, allow].filter(result => result.code === 0).length;
    dataUnavailable.value = successful === 0;
    if (snapshot.code === 0 && snapshot.data) {
      securitySnapshot.value = snapshot.data;
      lastRefreshAt.value = snapshot.data.asOf;
      securityAgent.value = {
        connected: !!snapshot.data.agent?.connected,
        appliedRules: snapshot.data.agent?.appliedRules ?? 0,
        lastError: snapshot.data.agent?.lastError || "",
        checkedAt: snapshot.data.agent?.checkedAt
      };
    }
    if (agentHealth.code === 0 && agentHealth.data) {
      securityAgent.value = agentHealth.data;
    }
    if (eventResult.code === 0) {
      const items = (eventResult.data?.items || []).slice().sort((left, right) => Date.parse(right.lastSeenAt) - Date.parse(left.lastSeenAt));
      securityEvents.value = items;
      events.value = items.map(mapEvent);
    }
    if (banResult.code === 0) autoBans.value = (banResult.data?.items || []).filter(item => item.status === "active" || item.status === "agent_failed").map(mapBan);
    if (policy.code === 0 && policy.data) { securityPolicy.value = policy.data; selectedMode.value = policy.data.mode; maxUdpThreshold.value = policy.data.maxUdpPerWindow; banThreshold.value = policy.data.banScore; }
    if (black.code === 0) blackRules.value = (black.data?.items || []).map(mapRule);
    if (allow.code === 0) allowRules.value = (allow.data?.items || []).map(mapRule);
    if (showMessage) Message.success("安全数据已刷新");
  } catch (error: any) {
    Message.warning(error?.message || "安全数据暂时不可用");
  } finally {
    refreshInFlight = false;
  }
}

function stopLiveRefresh() {
  if (refreshTimer !== null) {
    window.clearInterval(refreshTimer);
    refreshTimer = null;
  }
}

function startLiveRefresh() {
  stopLiveRefresh();
  if (live.value) refreshTimer = window.setInterval(() => refreshPreview(), 10000);
}

async function saveRule() {
  if (!ruleValue.value.trim()) {
    Message.warning("请输入匹配内容");
    return;
  }
  const type = ruleType.value === "User-Agent" ? "user_agent" : ruleType.value.toLowerCase() as "ip" | "cidr";
  const result = await createSecurityAccessRule({ listType: editingRuleKind.value === "blacklist" ? "blacklist" : "allowlist", matchType: type, matchValue: ruleValue.value.trim(), scope: ruleType.value === "User-Agent" ? "public" : "all_sip", status: "enabled", expiresAt: expiryToIso(ruleExpiry.value), note: ruleNote.value.trim() || "手动添加" });
  if (result.code !== 0) { Message.error(result.message || "规则保存失败"); return; }
  ruleDrawerVisible.value = false;
  await refreshPreview();
  Message.success("规则已保存");
}

async function toggleRule(rule: AccessRule, enabled: boolean) {
  const previous = rule.enabled;
  rule.enabled = enabled;
  const result = await updateSecurityAccessRule(rule.id, { listType: rule.listType, matchType: rule.matchType, matchValue: rule.value, scope: rule.scope, status: enabled ? "enabled" : "disabled", note: rule.note });
  if (result.code !== 0) {
    rule.enabled = previous;
    Message.error(result.message || "规则状态更新失败");
    return;
  }
  await refreshPreview();
}

function toggleRuleFromEvent(rule: AccessRule, value: unknown) {
  return toggleRule(rule, value === true);
}

async function removeRule(_kind: RuleKind, id: number) {
  const result = await deleteSecurityAccessRule(id);
  if (result.code !== 0) { Message.error(result.message || "规则删除失败"); return; }
  await refreshPreview();
  Message.success("规则已移除");
}

function showBanEvents(item: AutoBanRecord) {
  eventSearch.value = item.source;
  activeTab.value = "events";
}

async function unbanPreview(item: AutoBanRecord) {
  const result = await unbanSecurity(item.decisionId);
  if (result.code !== 0) { Message.error(result.message || "解除封禁失败"); return; }
  await refreshPreview();
  Message.success(`${item.source} 已解除自动封禁`);
}

async function promoteToBlacklist(item: AutoBanRecord) {
  if (!blackRules.value.some(rule => rule.type === "IP" && rule.value === item.source)) await createSecurityAccessRule({ listType: "blacklist", matchType: "ip", matchValue: item.source, scope: "all_sip", status: "enabled", note: item.reason });
  await refreshPreview();
  activeTab.value = "blacklist";
  Message.success(`${item.source} 已转为手动黑名单`);
}

async function savePolicyPreview() {
  if (!securityPolicy.value) return;
  securityPolicy.value.mode = selectedMode.value;
  securityPolicy.value.banScore = Math.max(1, banThreshold.value);
  securityPolicy.value.maxUdpPerWindow = Math.max(1, maxUdpThreshold.value);
  const result = await updateSecurityPolicy(securityPolicy.value);
  if (result.code !== 0) { Message.error(result.message || "防护策略保存失败"); return; }
  await refreshPreview();
  Message.success("防护策略已保存");
}

watch([activeTab, period, trendData], async () => {
  await nextTick();
  renderTrendChart();
});

watch(live, startLiveRefresh);

onMounted(async () => {
  await refreshPreview();
  startLiveRefresh();
  await nextTick();
  renderTrendChart();
  resizeObserver = new ResizeObserver(() => {
    if (chart && chartElement.value) chart.resize(chartElement.value.clientWidth, chartElement.value.clientHeight);
  });
  if (chartElement.value) resizeObserver.observe(chartElement.value);
  themeObserver = new MutationObserver(() => renderTrendChart());
  themeObserver.observe(document.body, { attributes: true, attributeFilter: ["arco-theme", "uvp-dark-style"] });
});

onBeforeUnmount(() => {
  chart?.release();
  resizeObserver?.disconnect();
  themeObserver?.disconnect();
  stopLiveRefresh();
});
</script>

<template>
  <main class="snow-page security-preview">
    <div class="snow-inner security-shell">
      <div class="security-nav">
        <a-tabs v-model:active-key="activeTab" class="security-tabs" type="line">
          <a-tab-pane key="overview" title="安全总览" />
          <a-tab-pane key="events" title="风险事件" />
          <a-tab-pane key="bans" title="自动封禁" />
          <a-tab-pane key="blacklist" title="黑名单" />
          <a-tab-pane key="allowlist" title="白名单" />
          <a-tab-pane key="policy" title="防护策略" />
        </a-tabs>
        <div class="tab-actions">
          <a-tag :color="liveStatus.color" bordered><span class="live-dot" />{{ liveStatus.label }}</a-tag>
          <a-tooltip content="刷新安全数据">
            <a-button aria-label="刷新安全数据" @click="refreshPreview(true)"><template #icon><RefreshCw :size="16" /></template>刷新</a-button>
          </a-tooltip>
          <a-button v-if="activeTab === 'policy'" type="primary" @click="savePolicyPreview"><template #icon><Check :size="16" /></template>保存策略</a-button>
          <a-button v-else-if="activeTab === 'bans'" type="primary" @click="activeTab = 'policy'"><template #icon><SlidersHorizontal :size="16" /></template>调整策略</a-button>
          <a-button v-else type="primary" @click="openRuleDrawer(activeTab === 'allowlist' ? 'allowlist' : 'blacklist')"><template #icon><Plus :size="16" /></template>{{ activeTab === 'allowlist' ? '添加白名单' : '添加黑名单' }}</a-button>
        </div>
      </div>

      <template v-if="activeTab === 'overview'">
        <section class="posture-panel uvp-system-panel" aria-label="当前防护状态">
          <div class="posture-main">
            <span class="shield-orbit" aria-hidden="true"><ShieldCheck :size="30" /></span>
            <div><span class="section-label">当前安全态势</span><h2>{{ selectedModeConfig.title }}模式已启用</h2><p>{{ selectedModeConfig.summary }}主机防火墙状态以 Agent 实际回报为准，云厂商边界防护尚未接入。</p></div>
          </div>
          <div class="score-block"><strong>{{ lastRefreshLabel }}</strong><small>接口更新时间</small><span>{{ live ? "每 10 秒自动刷新" : "自动刷新已暂停" }}</span></div>
          <button class="advice-block" type="button" @click="activeTab = 'policy'">
            <span><TriangleAlert :size="18" /></span>
            <span><strong>{{ attentionCount ? `还有 ${attentionCount} 项待处理` : "当前没有待处理项" }}</strong><small>{{ attentionCount ? "查看自动封禁和访问名单，确认是否需要长期处理。" : "自动封禁和访问名单当前没有需要复核的项目。" }}</small></span>
            <ChevronRight :size="18" />
          </button>
        </section>

        <section class="metric-grid" aria-label="安全指标">
          <article><span>已识别安全事件</span><strong>{{ recognizedCount }}</strong><small class="warning"><Activity :size="13" />来自安全事件接口聚合</small></article>
          <article><span>应用层已拦截</span><strong>{{ blockedCount }}</strong><small class="success"><ShieldCheck :size="13" />丢弃与封禁动作累计</small></article>
          <article><span>生效中自动封禁</span><strong>{{ autoBans.length }}</strong><small><Ban :size="13" />主机防火墙已生效 {{ autoBans.filter(item => item.firewallState === '已生效').length }}</small></article>
            <article><span>主机防火墙</span><strong class="status-value">{{ securityAgent.connected ? '在线' : '降级' }}</strong><small :class="securityAgent.connected ? 'success' : 'warning'"><BrickWall :size="13" />{{ securityAgent.appliedRules }} 条动态规则</small></article>
        </section>

        <section class="overview-grid">
          <article class="uvp-system-panel chart-panel">
            <div class="panel-heading">
              <div><span class="section-label">攻击趋势</span><h3>识别与封禁</h3></div>
              <div class="period-switch" aria-label="趋势周期">
                <button v-for="item in trendPeriods" :key="item" type="button" :class="{ active: period === item }" @click="period = item">{{ item }}</button>
              </div>
            </div>
            <div class="chart-legend"><span><i class="detected" />识别</span><span><i class="blocked" />封禁</span><b>安全事件接口</b></div>
            <div v-if="trendData.hasData" ref="chartElement" class="trend-chart" aria-label="攻击识别与封禁趋势图" />
            <div v-else class="empty-state chart-empty" role="status"><span class="empty-state-icon"><Activity :size="22" /></span><strong>暂无趋势数据</strong><small>当前时间范围内还没有安全事件，接口收到数据后会自动更新。</small></div>
          </article>

          <article class="uvp-system-panel defense-panel">
            <div class="panel-heading"><div><span class="section-label">三层防护</span><h3>防护链路</h3></div><a-tag color="blue">{{ enabledDefenseLayers }} / 3 已启用</a-tag></div>
            <div class="defense-chain">
              <div class="defense-item pending"><span><CloudCog :size="19" /></span><div><strong>云边界防护</strong><small>待接入云厂商 API</small></div><a-tag color="orange">规划中</a-tag></div>
              <i class="chain-line" />
              <div class="defense-item" :class="{ active: securityAgent.connected }"><span><BrickWall :size="19" /></span><div><strong>主机防火墙</strong><small>uvp-firewall-agent · nftables</small></div><a-tag :color="securityAgent.connected ? 'green' : 'orange'"><Check :size="12" />{{ securityAgent.connected ? '在线' : '降级' }}</a-tag></div>
              <i class="chain-line active" />
              <div class="defense-item active"><span><Fingerprint :size="19" /></span><div><strong>应用协议防护</strong><small>限速 · 鉴权 · 风险评分</small></div><a-tag color="green"><Check :size="12" />保护</a-tag></div>
            </div>
            <div class="panel-note"><Zap :size="16" /><span>明确恶意来源会自动永久封禁，只有人工解封才会解除。</span></div>
          </article>
        </section>

        <section class="overview-grid lower-grid">
          <article class="uvp-system-panel signal-panel">
            <div class="panel-heading"><div><span class="section-label">实时信号</span><h3>最近高频来源</h3></div><button class="live-toggle" type="button" :aria-pressed="live" @click="live = !live"><Pause v-if="live" :size="14" /><Play v-else :size="14" />{{ live ? '自动刷新' : '已暂停' }}</button></div>
            <div v-if="!events.length" class="empty-state signal-empty" role="status"><span class="empty-state-icon"><Activity :size="22" /></span><strong>暂无高频来源</strong><small>安全事件接口暂未发现重复风险来源，收到数据后会在这里显示。</small></div>
            <button v-for="event in events.slice(0, 3)" v-else :key="event.id" class="signal-row" type="button" @click="activeTab = 'events'">
              <span :class="['risk-dot', event.severity === '高危' ? 'danger' : 'warning']" />
              <span><strong>{{ event.source }}</strong><small>{{ event.method }} · {{ event.rule }}</small></span>
              <b>{{ event.count }} 次</b><ChevronRight :size="16" />
            </button>
          </article>

          <article class="uvp-system-panel attention-panel">
            <div class="panel-heading"><div><span class="section-label">待处理</span><h3>安全建议</h3></div><span class="attention-count">{{ attentionCount }}</span></div>
            <button v-if="autoBans.length" class="attention-row" type="button" @click="activeTab = 'bans'"><span class="attention-icon danger"><Ban :size="18" /></span><span><strong>{{ autoBans.length }} 条自动封禁待复核</strong><small>自动封禁已永久生效，复核后可转为人工黑名单。</small></span><ChevronRight :size="16" /></button>
            <button v-if="allowRules.length" class="attention-row" type="button" @click="activeTab = 'allowlist'"><span class="attention-icon warning"><UserRoundCheck :size="18" /></span><span><strong>{{ allowRules.length }} 条白名单规则</strong><small>可信出口加入白名单后可减少误判。</small></span><ChevronRight :size="16" /></button>
            <div v-if="!attentionCount" class="empty-state attention-empty" role="status"><span class="empty-state-icon"><CheckCircle2 :size="22" /></span><strong>当前没有待处理项</strong><small>安全事件、自动封禁和访问名单会在接口刷新后更新。</small></div>
          </article>
        </section>
      </template>

      <section v-else-if="activeTab === 'events'" class="workspace-panel uvp-system-panel">
        <div class="filter-bar">
          <a-select v-model="eventSeverity" aria-label="风险等级" style="width: 150px"><a-option>全部风险</a-option><a-option>高危</a-option><a-option>中危</a-option><a-option>低危</a-option></a-select>
          <a-input v-model="eventSearch" allow-clear placeholder="搜索 IP、方法或 User-Agent" aria-label="搜索风险事件"><template #prefix><Search :size="15" /></template></a-input>
          <a-button><template #icon><SlidersHorizontal :size="15" /></template>更多筛选</a-button>
        </div>
        <a-table class="security-table" :data="filteredEvents" row-key="id" :pagination="{ pageSize: 5 }" :scroll="{ x: 1050 }">
          <template #columns>
            <a-table-column title="风险" :width="90"><template #cell="{ record }"><span :class="['severity', record.severity === '高危' ? 'high' : record.severity === '中危' ? 'medium' : 'low']"><i />{{ record.severity }}</span></template></a-table-column>
            <a-table-column title="来源" :width="170"><template #cell="{ record }"><strong class="mono">{{ record.source }}</strong><small class="cell-subline">{{ record.location }}</small></template></a-table-column>
            <a-table-column title="方法" data-index="method" :width="100" />
            <a-table-column title="User-Agent" data-index="userAgent" :width="190" ellipsis tooltip />
            <a-table-column title="识别规则" data-index="rule" :width="200" />
            <a-table-column title="处理结果" data-index="action" :width="140" />
            <a-table-column title="次数" data-index="count" :width="80" />
            <a-table-column title="最近发生" data-index="time" :width="110" />
          </template>
        </a-table>
      </section>

      <section v-else-if="activeTab === 'bans'" class="workspace-panel uvp-system-panel">
        <div class="ban-summary">
          <div><span>正在封禁</span><strong>{{ autoBans.length }}</strong></div>
          <div><span>主机防火墙生效</span><strong>{{ autoBans.filter(item => item.firewallState === '已生效').length }}</strong></div>
          <div><span>仅应用层拦截</span><strong>{{ autoBans.filter(item => item.firewallState !== '已生效').length }}</strong></div>
            <div class="ban-summary-note"><ShieldCheck :size="18" /><span><strong>自动封禁与手动黑名单分开管理</strong><small>自动封禁永久生效，只有人工解封才会解除。</small></span></div>
        </div>
        <a-table class="security-table ban-table" :data="autoBans" row-key="id" :pagination="false" :scroll="{ x: 1060 }">
          <template #columns>
            <a-table-column title="来源" :width="160"><template #cell="{ record }"><strong class="mono">{{ record.source }}</strong><small class="cell-subline">{{ record.location }}</small></template></a-table-column>
            <a-table-column title="进入原因" :width="280"><template #cell="{ record }"><strong class="ban-reason">{{ record.reason }}</strong><small class="cell-subline">{{ record.evidence }}</small></template></a-table-column>
            <a-table-column title="触发策略" :width="110"><template #cell="{ record }"><a-tag :color="record.mode === '严格' ? 'red' : 'blue'">{{ record.mode }}模式</a-tag><small class="cell-subline">{{ record.method }}</small></template></a-table-column>
            <a-table-column title="防护位置" :width="170"><template #cell="{ record }"><span class="enforcement-tags"><a-tag color="green">应用层已拦截</a-tag><a-tag :color="record.firewallState === '已生效' ? 'green' : record.firewallState === '待同步' ? 'orange' : 'red'">主机防火墙{{ record.firewallState }}</a-tag></span></template></a-table-column>
            <a-table-column title="封禁时间" :width="110"><template #cell="{ record }"><span>{{ record.createdAt }}</span><small class="cell-subline">{{ record.expires }}</small></template></a-table-column>
            <a-table-column title="封禁后拦截" :width="100"><template #cell="{ record }"><strong>{{ record.blocked }} 次</strong></template></a-table-column>
            <a-table-column title="操作" :width="130"><template #cell="{ record }"><span class="ban-actions"><a-tooltip content="查看关联风险事件"><a-button size="small" aria-label="查看关联风险事件" @click="showBanEvents(record)"><template #icon><Search :size="14" /></template></a-button></a-tooltip><a-tooltip content="转为手动黑名单"><a-button size="small" aria-label="转为手动黑名单" @click="promoteToBlacklist(record)"><template #icon><Ban :size="14" /></template></a-button></a-tooltip><a-tooltip content="解除自动封禁"><a-button size="small" status="danger" aria-label="解除自动封禁" @click="unbanPreview(record)"><template #icon><ShieldOff :size="14" /></template></a-button></a-tooltip></span></template></a-table-column>
          </template>
        </a-table>
      </section>

      <section v-else-if="activeTab === 'blacklist' || activeTab === 'allowlist'" class="workspace-panel uvp-system-panel">
        <div class="rule-summary">
          <div><span>规则总数</span><strong>{{ currentRules.length }}</strong></div>
          <div><span>正在生效</span><strong>{{ currentRules.filter(item => item.enabled).length }}</strong></div>
          <div><span>{{ activeTab === 'blacklist' ? '人工添加' : '永久可信' }}</span><strong>{{ activeTab === 'blacklist' ? currentRules.length : currentRules.filter(item => item.expires === '永久').length }}</strong></div>
          <div class="rule-safety"><ShieldCheck :size="18" /><span><strong>{{ activeTab === 'blacklist' ? '这里只管理手动黑名单' : '白名单不绕过协议校验' }}</strong><small>{{ activeTab === 'blacklist' ? '策略自动生成的临时封禁请到“自动封禁”查看。' : '异常报文仍会被应用层拒绝。' }}</small></span></div>
        </div>
        <a-table class="security-table" :data="currentRules" row-key="id" :pagination="false" :scroll="{ x: 900 }">
          <template #columns>
            <a-table-column title="类型" data-index="type" :width="120" />
            <a-table-column title="匹配内容" :width="210"><template #cell="{ record }"><span class="mono">{{ record.value }}</span></template></a-table-column>
            <a-table-column title="备注" data-index="note" :width="210" />
            <a-table-column title="范围" data-index="scope" :width="160" />
            <a-table-column title="有效期" data-index="expires" :width="140" />
            <a-table-column title="状态" :width="100"><template #cell="{ record }"><a-switch :model-value="record.enabled" size="small" @change="toggleRuleFromEvent(record, $event)" /></template></a-table-column>
            <a-table-column title="操作" :width="80" fixed="right"><template #cell="{ record }"><a-tooltip content="删除规则"><a-button type="text" status="danger" aria-label="删除规则" @click="removeRule(activeTab, record.id)"><template #icon><Trash2 :size="16" /></template></a-button></a-tooltip></template></a-table-column>
          </template>
        </a-table>
      </section>

      <section v-else class="workspace-panel uvp-system-panel policy-workspace">
        <div class="mode-selector" role="radiogroup" aria-label="防护模式">
          <button v-for="mode in protectionModes" :key="mode.key" type="button" :class="['mode-option', `mode-${mode.key}`, { active: selectedMode === mode.key }]" :aria-checked="selectedMode === mode.key" role="radio" @click="selectedMode = mode.key">
            <span class="mode-icon"><component :is="mode.icon" :size="20" /></span>
            <span class="mode-copy"><span class="mode-heading"><strong>{{ mode.title }}</strong><em>{{ mode.badge }}</em></span><small>{{ mode.desc }}</small></span>
            <CheckCircle2 v-if="selectedMode === mode.key" class="mode-check" :size="18" />
          </button>
        </div>
        <section :class="['mode-explainer', `mode-${selectedModeConfig.key}`]" aria-live="polite">
          <div class="mode-explainer-heading">
            <span><component :is="selectedModeConfig.icon" :size="22" /></span>
            <div><small>{{ selectedModeConfig.title }}模式启用后</small><h3>{{ selectedModeConfig.headline }}</h3><p>{{ selectedModeConfig.summary }}</p></div>
          </div>
          <div class="mode-result-grid">
            <article><span>入口与速率</span><strong>{{ selectedModeConfig.entryResult }}</strong></article>
            <article><span>协议鉴权</span><strong>{{ selectedModeConfig.protocolResult }}</strong></article>
            <article><span>风险升级</span><strong>{{ selectedModeConfig.banResult }}</strong></article>
          </div>
        </section>
        <div class="policy-grid">
          <div class="policy-section">
            <div class="policy-title"><span><Activity :size="18" /></span><div><h3>自动防护规则</h3><p>控制异常来源何时被识别并升级处理。</p></div></div>
            <div class="setting-row"><span><strong>重复攻击自动封禁</strong><small>短时间持续攻击达到阈值后，永久封禁来源 IP，直到人工解封</small></span><a-tag :color="selectedMode === 'observe' ? 'orange' : 'green'">{{ selectedMode === 'observe' ? '观察模式不执行' : '永久封禁' }}</a-tag></div>
            <div class="setting-row"><span><strong>扫描器特征识别</strong><small>User-Agent 仅作为辅助信号，不作为可信身份</small></span><a-tag color="green">协议事件接口</a-tag></div>
            <div class="setting-row"><span><strong>入口速率阈值</strong><small>当前策略窗口内允许的 UDP 包数量 · {{ policyWindowLabel }}</small></span><div class="threshold-control"><a-input-number v-model="maxUdpThreshold" :min="1" :max="100000" /><span>包/窗口</span></div></div>
            <div class="setting-row"><span><strong>风险累计封禁阈值</strong><small>INVITE、REGISTER、MESSAGE 等风险按评分累计，达到阈值后加入主机防火墙</small></span><div class="threshold-control"><a-input-number v-model="banThreshold" :min="1" :max="10000" /><span>风险分</span></div></div>
          </div>
          <div class="policy-section">
            <div class="policy-title"><span><BrickWall :size="18" /></span><div><h3>主机防火墙</h3><p>阻止已确认的攻击流量继续进入服务进程。</p></div></div>
            <div class="setting-row"><span><strong>联动主机防火墙</strong><small>应用层确认攻击后，在操作系统网络入口封禁来源 IP</small></span><a-tag :color="securityAgent.connected ? 'green' : 'orange'">{{ securityAgent.connected ? 'Agent 已连接' : 'Agent 未连接' }}</a-tag></div>
            <div class="agent-status"><span :class="['agent-icon', { warning: !securityAgent.connected }]" ><CheckCircle2 v-if="securityAgent.connected" :size="22" /><TriangleAlert v-else :size="22" /></span><span><strong>{{ agentStatus.title }}</strong><small>{{ agentStatus.detail }}</small></span><a-tag :color="agentStatus.color">{{ agentStatus.label }}</a-tag></div>
            <div class="ttl-row"><span><strong>自动封禁有效期</strong><small>风险达到阈值后永久加入防护，除非人工解封</small></span><a-tag color="red">永久</a-tag></div>
            <div class="cloud-roadmap"><CloudCog :size="18" /><span><strong>云厂商防火墙联动</strong><small>当前系统未接入云厂商 API，暂不宣称已生效。</small></span><a-tag color="orange">未接入</a-tag></div>
          </div>
        </div>
      </section>
    </div>

    <a-drawer v-model:visible="ruleDrawerVisible" :width="440" :footer="false" unmount-on-close>
      <template #title>{{ editingRuleKind === 'blacklist' ? '新增黑名单规则' : '新增白名单规则' }}</template>
              <div class="drawer-intro"><span :class="editingRuleKind"><Ban v-if="editingRuleKind === 'blacklist'" :size="20" /><UserRoundCheck v-else :size="20" /></span><div><strong>{{ editingRuleKind === 'blacklist' ? '拒绝风险来源' : '放行可信来源' }}</strong><p>规则会写入安全策略并在应用层即时生效；User-Agent 规则不会写入主机防火墙。</p></div></div>
      <a-form :model="ruleFormModel" layout="vertical" class="rule-form">
        <a-form-item label="规则类型"><a-radio-group v-model="ruleType" type="button"><a-radio value="IP">IP</a-radio><a-radio value="CIDR">CIDR</a-radio><a-radio value="User-Agent">User-Agent</a-radio></a-radio-group></a-form-item>
        <a-form-item label="匹配内容" required><a-input v-model="ruleValue" :placeholder="ruleType === 'IP' ? '例如 203.0.113.12' : ruleType === 'CIDR' ? '例如 203.0.113.0/24' : '例如 friendly-scanner*'" /></a-form-item>
        <a-form-item label="备注"><a-textarea v-model="ruleNote" placeholder="说明规则用途，方便后续复核" :max-length="80" show-word-limit /></a-form-item>
        <a-form-item label="有效期"><a-select v-model="ruleExpiry"><a-option>10 分钟</a-option><a-option>1 小时</a-option><a-option>1 天</a-option><a-option>7 天</a-option><a-option>永久</a-option></a-select></a-form-item>
      </a-form>
      <a-alert v-if="editingRuleKind === 'allowlist'" type="warning">白名单只降低来源风险分，不会绕过 SIP 格式、鉴权和设备身份校验。</a-alert>
      <div class="drawer-actions"><a-button @click="ruleDrawerVisible = false">取消</a-button><a-button type="primary" @click="saveRule">保存规则</a-button></div>
    </a-drawer>
  </main>
</template>

<style scoped>
.security-preview {
  box-sizing: border-box;
  padding: var(--uvp-main-padding);
  color: var(--uvp-text-primary);
}

.security-shell {
  width: 100%;
  max-width: 1540px;
  margin: 0 auto;
}

.security-nav,
.tab-actions,
.posture-main,
.advice-block,
.panel-heading,
.chart-legend,
.chart-legend span,
.defense-item,
.panel-note,
.live-toggle,
.signal-row,
.filter-bar,
.rule-safety,
.policy-title,
.setting-row,
.agent-status,
.ttl-row,
.ttl-row > div,
.cloud-roadmap,
.drawer-intro,
.drawer-actions {
  display: flex;
  align-items: center;
}

.security-nav {
  align-items: flex-end;
  gap: 20px;
  margin-bottom: 16px;
  border-bottom: 1px solid var(--uvp-panel-border);
}

.tab-actions {
  flex: 0 0 auto;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding-bottom: 8px;
}

.live-dot {
  display: inline-block;
  width: 7px;
  height: 7px;
  margin-right: 6px;
  border-radius: 50%;
  background: var(--uvp-brand-cyan);
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--uvp-brand-cyan) 14%, transparent);
}

.security-tabs {
  flex: 1;
  min-width: 0;
  margin-bottom: 0;
}

:deep(.security-tabs .arco-tabs-content) {
  display: none;
}

:deep(.security-tabs .arco-tabs-nav-tab-list) {
  gap: 12px;
}

.uvp-system-panel,
.metric-grid article,
.workspace-panel {
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
  background: var(--uvp-panel-bg);
  box-shadow: var(--uvp-panel-shadow);
}

.posture-panel {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 150px minmax(300px, 0.65fr);
  align-items: stretch;
  overflow: hidden;
  border-color: color-mix(in srgb, var(--uvp-brand) 24%, var(--uvp-panel-border));
}

.posture-main {
  gap: 17px;
  padding: 20px 22px;
}

.shield-orbit {
  position: relative;
  display: grid;
  flex: 0 0 54px;
  width: 54px;
  height: 54px;
  color: var(--uvp-brand);
  border-radius: 50%;
  background: var(--uvp-brand-soft);
  place-items: center;
}

.shield-orbit::after {
  position: absolute;
  inset: -5px;
  content: "";
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 28%, transparent);
  border-radius: 50%;
  animation: shieldPulse 2.2s ease-out infinite;
}

.section-label {
  color: var(--uvp-text-tertiary);
  font-size: 11px;
  font-weight: 650;
}

.posture-main h2 {
  margin: 4px 0 5px;
  font-size: 18px;
}

.posture-main p {
  margin: 0;
  color: var(--uvp-text-secondary);
  font-size: 13px;
  line-height: 1.5;
}

.score-block {
  display: grid;
  align-content: center;
  padding: 16px 22px;
  border-left: 1px solid var(--uvp-panel-border);
  border-right: 1px solid var(--uvp-panel-border);
  text-align: center;
}

.score-block strong {
  color: var(--uvp-brand-strong);
  font-size: 30px;
}

.score-block span,
.score-block small {
  color: var(--uvp-text-tertiary);
}

.score-block small {
  margin-top: 3px;
  font-size: 11px;
}

.advice-block {
  width: 100%;
  gap: 12px;
  padding: 16px 20px;
  border: 0;
  color: inherit;
  background: var(--uvp-warning-soft);
  text-align: left;
  cursor: pointer;
  transition: background-color 0.2s ease;
}

.advice-block:hover {
  background: color-mix(in srgb, var(--uvp-warning-soft) 78%, var(--uvp-warning) 10%);
}

.advice-block > span:first-child {
  display: grid;
  flex: 0 0 34px;
  width: 34px;
  height: 34px;
  color: var(--uvp-warning);
  border-radius: 8px;
  background: color-mix(in srgb, var(--uvp-warning) 12%, transparent);
  place-items: center;
}

.advice-block > span:nth-child(2) {
  display: flex;
  flex: 1;
  min-width: 0;
  flex-direction: column;
  gap: 4px;
}

.advice-block small {
  color: var(--uvp-text-secondary);
  line-height: 1.45;
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin-top: 12px;
}

.metric-grid article {
  min-width: 0;
  padding: 17px 18px;
}

.metric-grid article > span {
  color: var(--uvp-text-tertiary);
  font-size: 12px;
}

.metric-grid strong {
  display: block;
  margin: 7px 0 5px;
  font-size: 25px;
  font-variant-numeric: tabular-nums;
}

.metric-grid small {
  display: flex;
  align-items: center;
  gap: 5px;
  color: var(--uvp-text-secondary);
}

.metric-grid .success,
.metric-grid .status-value {
  color: var(--uvp-brand-cyan);
}

.metric-grid .warning {
  color: var(--uvp-warning);
}

.overview-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.35fr) minmax(340px, 0.65fr);
  gap: 12px;
  margin-top: 12px;
}

.chart-panel,
.defense-panel,
.signal-panel,
.attention-panel {
  min-width: 0;
  padding: 18px;
}

.panel-heading {
  min-height: 40px;
  justify-content: space-between;
  gap: 16px;
}

.panel-heading h3 {
  margin: 4px 0 0;
  font-size: 15px;
}

.period-switch {
  display: grid;
  grid-template-columns: repeat(3, 42px);
  padding: 3px;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 7px;
  background: var(--uvp-table-header-bg);
}

.period-switch button {
  height: 27px;
  border: 0;
  border-radius: 5px;
  color: var(--uvp-text-tertiary);
  background: transparent;
  cursor: pointer;
}

.period-switch button.active {
  color: var(--uvp-brand-strong);
  background: var(--uvp-panel-bg);
  box-shadow: 0 1px 4px rgb(15 23 42 / 10%);
}

.chart-legend {
  gap: 16px;
  margin-top: 12px;
  color: var(--uvp-text-tertiary);
  font-size: 11px;
}

.chart-legend span {
  gap: 6px;
}

.chart-legend i {
  width: 18px;
  height: 2px;
}

.chart-legend .detected {
  background: var(--uvp-brand);
}

.chart-legend .blocked {
  background: var(--uvp-danger);
}

.chart-legend b {
  margin-left: auto;
  font-weight: 500;
}

.trend-chart {
  width: 100%;
  height: 250px;
}

.empty-state {
  display: grid;
  min-height: 180px;
  padding: 24px 18px;
  color: var(--uvp-text-secondary);
  text-align: center;
  place-items: center;
  align-content: center;
  gap: 8px;
}

.empty-state-icon {
  display: grid;
  width: 42px;
  height: 42px;
  color: var(--uvp-text-tertiary);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 50%;
  background: var(--uvp-list-panel-bg);
  place-items: center;
}

.empty-state strong {
  color: var(--uvp-text-primary);
  font-size: 13px;
}

.empty-state small {
  max-width: 310px;
  color: var(--uvp-text-tertiary);
  line-height: 1.55;
}

.chart-empty {
  min-height: 250px;
}

.signal-empty {
  min-height: 190px;
}

.defense-chain {
  padding-top: 13px;
}

.defense-item {
  min-height: 58px;
  gap: 11px;
  padding: 10px 12px;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
  background: var(--uvp-list-panel-bg);
}

.defense-item > span:first-child {
  display: grid;
  flex: 0 0 34px;
  width: 34px;
  height: 34px;
  border-radius: 8px;
  place-items: center;
}

.defense-item.pending > span:first-child {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
}

.defense-item.active > span:first-child {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent);
}

.defense-item > div {
  display: flex;
  flex: 1;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.defense-item small,
.panel-note,
.signal-row small {
  color: var(--uvp-text-tertiary);
  line-height: 1.45;
}

.chain-line {
  display: block;
  width: 1px;
  height: 15px;
  margin-left: 29px;
  background: var(--uvp-warning-border);
}

.chain-line.active {
  background: color-mix(in srgb, var(--uvp-brand-cyan) 42%, var(--uvp-panel-border));
}

.panel-note {
  gap: 8px;
  margin-top: 12px;
  padding: 10px 12px;
  color: var(--uvp-brand-cyan);
  border-radius: 8px;
  background: color-mix(in srgb, var(--uvp-brand-cyan) 7%, var(--uvp-list-panel-bg));
}

.lower-grid {
  grid-template-columns: minmax(0, 1fr) minmax(340px, 0.75fr);
}

.live-toggle {
  gap: 6px;
  padding: 6px 9px;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 7px;
  color: var(--uvp-brand-cyan);
  background: var(--uvp-list-panel-bg);
  cursor: pointer;
}

.signal-row {
  width: 100%;
  min-height: 58px;
  gap: 10px;
  padding: 10px 4px;
  border: 0;
  border-bottom: 1px solid var(--uvp-panel-border);
  color: inherit;
  background: transparent;
  text-align: left;
  cursor: pointer;
}

.signal-row:last-child {
  border-bottom: 0;
}

.signal-row:hover {
  background: var(--uvp-table-row-hover-bg);
}

.risk-dot {
  flex: 0 0 7px;
  width: 7px;
  height: 7px;
  border-radius: 50%;
}

.risk-dot.danger {
  background: var(--uvp-danger);
}

.risk-dot.warning {
  background: var(--uvp-warning);
}

.signal-row > span:nth-child(2) {
  display: flex;
  flex: 1;
  min-width: 0;
  flex-direction: column;
  gap: 4px;
}

.signal-row b {
  color: var(--uvp-text-secondary);
  font-size: 12px;
  white-space: nowrap;
}

.attention-row {
  display: grid;
  grid-template-columns: 36px minmax(0, 1fr) 18px;
  width: 100%;
  min-height: 76px;
  align-items: center;
  gap: 12px;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--uvp-list-panel-border);
  border-radius: 8px;
  color: inherit;
  background: var(--uvp-list-panel-bg);
  text-align: left;
  cursor: pointer;
}

.attention-row:hover {
  border-color: color-mix(in srgb, var(--uvp-brand) 28%, var(--uvp-panel-border));
  background: var(--uvp-table-row-hover-bg);
}

.attention-row > span:nth-child(2) {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 4px;
}

.attention-row strong {
  font-size: 13px;
  line-height: 1.4;
}

.attention-row small {
  color: var(--uvp-text-tertiary);
  line-height: 1.5;
}

.attention-row > svg {
  justify-self: end;
  color: var(--uvp-text-tertiary);
}

.attention-count {
  display: grid;
  min-width: 26px;
  height: 26px;
  color: var(--uvp-warning);
  border-radius: 50%;
  background: var(--uvp-warning-soft);
  place-items: center;
}

.attention-icon,
.agent-icon {
  display: grid;
  flex: 0 0 36px;
  width: 36px;
  height: 36px;
  border-radius: 8px;
  place-items: center;
}

.attention-icon.danger {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
}

.attention-icon.warning {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
}

.workspace-panel {
  min-width: 0;
  padding: 20px;
}

.filter-bar {
  gap: 8px;
  margin-bottom: 14px;
  padding: 12px;
  border: 1px solid var(--uvp-list-panel-border);
  border-radius: 9px;
  background: var(--uvp-list-toolbar-bg);
}

.filter-bar :deep(.arco-input-wrapper) {
  max-width: 440px;
}

.security-table {
  overflow: hidden;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 9px;
}

:deep(.security-table .arco-table-th) {
  background: var(--uvp-table-header-bg);
}

:deep(.security-table .arco-table-tr) {
  background: var(--uvp-table-row-bg);
}

:deep(.security-table .arco-table-tr:hover .arco-table-td) {
  background: var(--uvp-table-row-hover-bg);
}

.mono {
  font-family: "SFMono-Regular", Consolas, monospace;
  font-variant-numeric: tabular-nums;
}

.cell-subline {
  display: block;
  margin-top: 4px;
  color: var(--uvp-text-tertiary);
}

.severity {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 7px;
  border-radius: 6px;
}

.severity i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}

.severity.high {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
}

.severity.high i {
  background: var(--uvp-danger);
}

.severity.medium {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
}

.severity.medium i {
  background: var(--uvp-warning);
}

.severity.low {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
}

.severity.low i {
  background: var(--uvp-brand-cyan);
}

.rule-summary,
.ban-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(110px, 0.55fr)) minmax(280px, 1.45fr);
  margin-bottom: 14px;
  overflow: hidden;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 9px;
  background: var(--uvp-list-panel-bg);
}

.rule-summary > div,
.ban-summary > div {
  padding: 14px 16px;
  border-right: 1px solid var(--uvp-panel-border);
}

.rule-summary > div:last-child,
.ban-summary > div:last-child {
  border-right: 0;
}

.rule-summary span,
.ban-summary > div > span:first-child {
  color: var(--uvp-text-tertiary);
  font-size: 11px;
}

.rule-summary > div > strong,
.ban-summary > div > strong {
  display: block;
  margin-top: 5px;
  font-size: 20px;
}

.rule-summary .rule-safety,
.ban-summary .ban-summary-note {
  gap: 10px;
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 6%, var(--uvp-list-panel-bg));
}

.rule-safety > span,
.ban-summary-note > span {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.rule-safety strong,
.ban-summary-note strong {
  margin: 0;
  color: var(--uvp-text-primary);
  font-size: 12px;
}

.rule-safety small,
.ban-summary-note small {
  color: var(--uvp-text-tertiary);
}

.ban-reason {
  display: block;
  line-height: 1.45;
}

.enforcement-tags,
.ban-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

.ban-summary-note > svg {
  flex: 0 0 auto;
}

.mode-selector {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin-bottom: 14px;
}

.mode-selector button {
  position: relative;
  display: flex;
  min-height: 76px;
  align-items: center;
  gap: 12px;
  padding: 13px 14px;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 9px;
  color: inherit;
  background: var(--uvp-list-panel-bg);
  text-align: left;
  cursor: pointer;
  transition: border-color 0.2s ease, background-color 0.2s ease;
}

.mode-selector button:hover {
  border-color: color-mix(in srgb, var(--uvp-brand) 35%, var(--uvp-panel-border));
}

.mode-selector button.active {
  border-color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
}

.mode-icon {
  display: grid;
  flex: 0 0 40px;
  width: 40px;
  height: 40px;
  color: var(--uvp-text-secondary);
  border-radius: 8px;
  background: var(--uvp-panel-bg);
  place-items: center;
}

.mode-copy {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 5px;
}

.mode-selector button.active .mode-icon,
.mode-selector button.active .mode-check {
  color: var(--uvp-brand-strong);
}

.mode-heading {
  display: flex;
  align-items: center;
  gap: 8px;
}

.mode-heading strong {
  font-size: 14px;
}

.mode-heading em {
  padding: 2px 6px;
  border-radius: 4px;
  color: var(--uvp-text-secondary);
  background: var(--uvp-table-header-bg);
  font-size: 10px;
  font-style: normal;
  font-weight: 500;
}

.mode-selector button.active .mode-heading em {
  color: var(--uvp-brand-strong);
  background: color-mix(in srgb, var(--uvp-brand) 10%, var(--uvp-panel-bg));
}

.mode-copy small {
  min-width: 0;
  color: var(--uvp-text-tertiary);
  line-height: 1.45;
}

.mode-check {
  flex: 0 0 18px;
}

.mode-explainer {
  margin-bottom: 14px;
  overflow: hidden;
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 28%, var(--uvp-panel-border));
  border-radius: 8px;
  background: color-mix(in srgb, var(--uvp-brand) 4%, var(--uvp-list-panel-bg));
}

.mode-explainer-heading {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 15px 16px 14px;
}

.mode-explainer-heading > span {
  display: grid;
  flex: 0 0 40px;
  width: 40px;
  height: 40px;
  color: var(--uvp-brand-strong);
  border-radius: 8px;
  background: var(--uvp-brand-soft);
  place-items: center;
}

.mode-explainer-heading > div {
  min-width: 0;
}

.mode-explainer-heading small {
  color: var(--uvp-brand-strong);
  font-weight: 600;
}

.mode-explainer-heading h3,
.mode-explainer-heading p {
  margin: 0;
}

.mode-explainer-heading h3 {
  margin-top: 3px;
  font-size: 15px;
}

.mode-explainer-heading p {
  margin-top: 5px;
  color: var(--uvp-text-secondary);
  font-size: 12px;
  line-height: 1.55;
}

.mode-result-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  border-top: 1px solid var(--uvp-panel-border);
  background: var(--uvp-list-panel-bg);
}

.mode-result-grid article {
  min-width: 0;
  padding: 12px 16px 13px;
  border-right: 1px solid var(--uvp-panel-border);
}

.mode-result-grid article:last-child {
  border-right: 0;
}

.mode-result-grid span,
.mode-result-grid strong {
  display: block;
}

.mode-result-grid span {
  margin-bottom: 5px;
  color: var(--uvp-text-tertiary);
  font-size: 11px;
}

.mode-result-grid strong {
  color: var(--uvp-text-primary);
  font-size: 12px;
  line-height: 1.5;
}

.mode-explainer.mode-observe {
  border-color: var(--uvp-warning-border);
  background: var(--uvp-warning-soft);
}

.mode-explainer.mode-observe .mode-explainer-heading > span,
.mode-explainer.mode-observe .mode-explainer-heading small {
  color: var(--uvp-warning);
}

.mode-explainer.mode-strict {
  border-color: color-mix(in srgb, var(--uvp-danger) 24%, var(--uvp-panel-border));
  background: color-mix(in srgb, var(--uvp-danger) 4%, var(--uvp-list-panel-bg));
}

.mode-explainer.mode-strict .mode-explainer-heading > span,
.mode-explainer.mode-strict .mode-explainer-heading small {
  color: var(--uvp-danger);
}

.policy-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.policy-section {
  overflow: hidden;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 9px;
  background: var(--uvp-list-panel-bg);
}

.policy-title {
  gap: 11px;
  min-height: 70px;
  padding: 13px 16px;
  border-bottom: 1px solid var(--uvp-panel-border);
  background: var(--uvp-table-header-bg);
}

.policy-title > span {
  display: grid;
  width: 36px;
  height: 36px;
  color: var(--uvp-brand);
  border-radius: 8px;
  background: var(--uvp-brand-soft);
  place-items: center;
}

.policy-title h3,
.policy-title p {
  margin: 0;
}

.policy-title h3 {
  font-size: 14px;
}

.policy-title p {
  margin-top: 4px;
  color: var(--uvp-text-tertiary);
  font-size: 11px;
}

.setting-row,
.ttl-row {
  min-height: 68px;
  gap: 14px;
  padding: 11px 16px;
  border-bottom: 1px solid var(--uvp-panel-border);
}

.setting-row > span,
.ttl-row > span,
.agent-status > span:nth-child(2),
.cloud-roadmap > span:nth-child(2) {
  display: flex;
  flex: 1;
  min-width: 0;
  flex-direction: column;
  gap: 4px;
}

.setting-row small,
.ttl-row small,
.agent-status small,
.cloud-roadmap small {
  color: var(--uvp-text-tertiary);
  line-height: 1.45;
}

.threshold-control {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 8px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}

:deep(.threshold-control .arco-input-number) {
  width: 112px;
}

.agent-status,
.cloud-roadmap {
  min-height: 72px;
  gap: 11px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--uvp-panel-border);
}

.agent-icon {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
}

.agent-icon.warning {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
}

.ttl-row {
  align-items: flex-start;
  flex-direction: column;
}

.ttl-row > div {
  flex-wrap: wrap;
  gap: 5px;
}

.cloud-roadmap {
  color: var(--uvp-warning);
  border-bottom: 0;
  background: var(--uvp-warning-soft);
}

.cloud-roadmap strong {
  color: var(--uvp-text-primary);
}

.drawer-intro {
  gap: 11px;
  margin-bottom: 18px;
  padding: 12px;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 9px;
  background: var(--uvp-list-panel-bg);
}

.drawer-intro > span {
  display: grid;
  flex: 0 0 40px;
  width: 40px;
  height: 40px;
  border-radius: 8px;
  place-items: center;
}

.drawer-intro > span.blacklist {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
}

.drawer-intro > span.allowlist {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
}

.drawer-intro p {
  margin: 4px 0 0;
  color: var(--uvp-text-tertiary);
  line-height: 1.5;
}

.drawer-actions {
  justify-content: flex-end;
  gap: 8px;
  margin-top: 18px;
}

@keyframes shieldPulse {
  from { opacity: 0.72; transform: scale(0.88); }
  to { opacity: 0; transform: scale(1.16); }
}

@media (max-width: 1180px) {
  .posture-panel { grid-template-columns: minmax(0, 1fr) 140px; }
  .advice-block { grid-column: 1 / -1; border-top: 1px solid var(--uvp-warning-border); }
  .metric-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .overview-grid,
  .lower-grid { grid-template-columns: 1fr; }
  .rule-summary,
  .ban-summary { grid-template-columns: repeat(3, 1fr); }
  .rule-summary .rule-safety,
  .ban-summary .ban-summary-note { grid-column: 1 / -1; border-top: 1px solid var(--uvp-panel-border); }
}

@media (max-width: 820px) {
  .security-preview { padding: 16px; }
  .security-nav { align-items: stretch; flex-direction: column-reverse; gap: 0; }
  .tab-actions { width: 100%; justify-content: flex-end; padding-bottom: 6px; }
  .posture-panel { grid-template-columns: 1fr; }
  .score-block { border: 0; border-top: 1px solid var(--uvp-panel-border); border-bottom: 1px solid var(--uvp-panel-border); }
  .advice-block { grid-column: auto; }
  .mode-selector,
  .policy-grid,
  .mode-result-grid { grid-template-columns: 1fr; }
  .mode-result-grid article { border-right: 0; border-bottom: 1px solid var(--uvp-panel-border); }
  .mode-result-grid article:last-child { border-bottom: 0; }
  .filter-bar { align-items: stretch; flex-direction: column; }
  .filter-bar :deep(.arco-select-view),
  .filter-bar :deep(.arco-input-wrapper) { width: 100% !important; max-width: none; }
}

@media (max-width: 520px) {
  .security-preview { padding: 12px; }
  .tab-actions :deep(.arco-tag) { display: none; }
  .tab-actions :deep(.arco-btn) { flex: 1; }
  :deep(.security-tabs .arco-tabs-nav-tab-list) { gap: 0; }
  :deep(.security-tabs .arco-tabs-tab) { padding-right: 10px; padding-left: 10px; }
  .metric-grid { grid-template-columns: 1fr; }
  .posture-main { align-items: flex-start; padding: 17px; }
  .shield-orbit { flex-basis: 46px; width: 46px; height: 46px; }
  .chart-panel,
  .defense-panel,
  .signal-panel,
  .attention-panel,
  .workspace-panel { padding: 14px; }
  .period-switch { grid-template-columns: repeat(3, 36px); }
  .chart-legend b { display: none; }
  .trend-chart { height: 220px; }
  .rule-summary,
  .ban-summary { grid-template-columns: 1fr; }
  .rule-summary > div,
  .ban-summary > div { border-right: 0; border-bottom: 1px solid var(--uvp-panel-border); }
  .rule-summary > div:last-child,
  .ban-summary > div:last-child { border-bottom: 0; }
  .rule-summary .rule-safety,
  .ban-summary .ban-summary-note { grid-column: auto; border-top: 0; }
  .drawer-actions :deep(.arco-btn) { flex: 1; }
  .setting-row { align-items: flex-start; }
  .threshold-control { align-self: flex-end; }
}

@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
  }
}
</style>
