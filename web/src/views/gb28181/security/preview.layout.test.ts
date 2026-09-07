import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/security/preview.vue"), "utf8");
const apiSource = readFileSync(resolve(process.cwd(), "src/api/gb28181-security.ts"), "utf8");

describe("security preview system integration", () => {
  it("uses the UVP workspace shell and theme tokens", () => {
    expect(source).toContain(":class=\"['snow-page', 'security-preview', { 'security-preview--workspace': ['events', 'bans', 'blacklist', 'allowlist'].includes(activeTab) }]\"");
    expect(source).toContain('class="snow-inner uvp-page-shell-flat security-shell"');
    expect(source).toContain("var(--uvp-panel-bg)");
    expect(source).toContain("var(--uvp-brand)");
    expect(source).toContain("var(--uvp-text-primary)");
  });

  it("does not own the application shell or mutate the global theme", () => {
    expect(source).not.toContain('class="topbar"');
    expect(source).not.toContain("security-preview-body");
    expect(source).not.toContain('setAttribute("arco-theme"');
    expect(source).not.toContain("min-height: 100vh");
  });

  it("opens the rule form in the shared system dialog", () => {
    expect(source).toContain(':model="ruleFormModel"');
    expect(source).toContain('<a-modal v-model:visible="ruleDrawerVisible"');
    expect(source).toContain('modal-class="uvp-system-dialog security-rule-dialog"');
    expect(source).not.toContain('<a-drawer v-model:visible="ruleDrawerVisible"');
  });

  it("keeps navigation actions without a duplicate page heading", () => {
    expect(source).not.toContain('class="page-heading"');
    expect(source).not.toContain('class="heading-copy"');
    expect(source).toContain('class="security-nav"');
    expect(source).toContain('class="tab-actions"');
  });

  it("starts tab workspaces with their controls instead of repeated headings", () => {
    expect(source).not.toContain('class="workspace-heading');
    expect(source).not.toContain('class="rule-heading');
    expect(source).not.toContain('class="workspace-panel uvp-system-panel');
    expect(source.match(/class="workspace-panel/g)).toHaveLength(4);
    expect(source).toMatch(/\.workspace-panel\s*{[^}]*padding:\s*0;/s);
    expect(source).toContain("activeTab === 'policy'");
    expect(source).toContain("activeTab === 'allowlist' ? '添加白名单' : '添加黑名单'");
  });

  it("explains the selected mode with concrete security outcomes", () => {
    expect(source).toContain("const protectionModes");
    expect(source).toContain("selectedModeConfig");
    expect(source).toContain("入口与速率");
    expect(source).toContain("协议鉴权");
    expect(source).toContain("风险升级");
    expect(source).toContain("REGISTER、MESSAGE");
    expect(source).toContain("REGISTER 始终校验格式、鉴权和设备身份");
  });

  it("explains registration rejection and the shared source-verification gate", () => {
    expect(source).toContain("formatSecurityAction");
    expect(source).toContain("isHighRiskSecurityReason");
    expect(source).toContain("action: formatSecurityAction(event.action)");
    expect(source).toContain("仍校验协议，仅关闭自动封 IP");
    expect(source).toContain("仍校验 REGISTER，仅关闭自动封 IP");
    expect(source).toContain("无效 REGISTER 仍会拒绝并记录，观察模式只关闭自动封 IP");
    expect(source).toContain("REGISTER 始终校验格式、鉴权和设备身份；无效请求拒绝，修正后可重新注册");
    expect(source).toContain("所有模式都校验 REGISTER 格式、鉴权和设备 ID；密码错误或单一非法 ID 会拒绝并提示检查配置，修正后可重新注册。");
    expect(source).toContain("<a-tag color=\"blue\">可修正重试</a-tag>");
    expect(source).not.toContain("保护/严格模式下，密码错误或单一非法 ID 会拒绝并提示检查配置，修正后可重新注册；观察模式只记录。");
    expect(source).not.toContain("观察模式只记录");
    expect(source).not.toContain("{{ selectedMode === 'observe' ? '仅记录' : '可重试' }}");
    expect(source).toContain("发现非法 ID 枚举只代表高危并拒绝，不直接永久封 IP");
    expect(source).toContain("真实 TCP 或平台回程验证");
    expect(source).toContain("成功认证共享出口");
    expect(source).toContain("单向 UDP 未验证来源只拒绝并记录");
    expect(source).toContain("INVITE 自动 IP 封禁同样适用");
    expect(source).toContain("扫描枚举即使结果为“已拒绝”也属正常");
  });

  it("renders firewall agent health from the live snapshot instead of demo text", () => {
    expect(source).toContain("securityAgent.value.checkedAt");
    expect(source).toContain("agentStatus.detail");
    expect(source).toContain("主机防火墙未连接");
    expect(source).not.toContain("最后心跳 3 秒前 · 已应用 18 条规则");
  });

  it("models platform capability separately from firewall connectivity", () => {
    expect(apiSource).toContain('export type SecurityAgentCapability = "supported" | "unsupported" | "unknown";');
    expect(apiSource).toContain('export type FirewallAgentState = "applied" | "failed" | "unsupported" | "unknown";');
    expect(apiSource).toContain("capability?: SecurityAgentCapability");
    expect(apiSource).toContain("agentState: FirewallAgentState");
  });

  it("distinguishes an unsupported firewall from a disconnected agent", () => {
    expect(source).toContain('securityAgent.value.capability === "unsupported"');
    expect(source).toContain("主机防火墙不支持");
    expect(source).toContain("主机防火墙未连接");
    expect(source).toContain("应用层拦截仍生效");
    expect(source).toContain("系统防火墙不支持");
    expect(source).toContain("Agent 不支持");
  });

  it("keeps application enforcement actions visible for unsupported bans", () => {
    expect(source).toContain('ban.agentState === "unsupported"');
    expect(source).toContain("firewallStateLabel");
    expect(source).toContain("系统防火墙已生效");
    expect(source).toContain("系统防火墙不支持");
    expect(source).toContain("<a-option>不支持</a-option>");
    expect(source).toContain("unbanPreview(record)");
  });

  it("uses a standalone unit label for policy thresholds", () => {
    expect(source).toContain('class="threshold-control"');
    expect(source).toContain("风险累计封禁阈值");
    expect(source).not.toContain("<template #suffix>次/分</template>");
  });

  it("keeps protection mode controls compact and responsive", () => {
    expect(source).toMatch(/\.mode-selector button\s*{[^}]*display:\s*flex;/s);
    expect(source).toMatch(/\.mode-copy\s*{[^}]*min-width:\s*0;/s);
    expect(source).toContain(".mode-result-grid");
  });

  it("aligns security navigation icons with their labels", () => {
    expect(source).toMatch(/\.security-tabs \.arco-tabs-tab\)\s*{[^}]*gap:\s*8px;/s);
    expect(source).toMatch(/\.security-tabs \.arco-tabs-tab svg\)\s*{[^}]*transform:\s*translateY\(1px\);/s);
  });

  it("places protection policy immediately after the overview", () => {
    const overviewIndex = source.indexOf('<a-tab-pane key="overview">');
    const policyIndex = source.indexOf('<a-tab-pane key="policy">');
    const eventsIndex = source.indexOf('<a-tab-pane key="events">');

    expect(overviewIndex).toBeGreaterThan(-1);
    expect(policyIndex).toBeGreaterThan(overviewIndex);
    expect(eventsIndex).toBeGreaterThan(policyIndex);
  });

  it("separates automatic firewall bans from manual blacklist rules", () => {
    expect(source).toContain('<a-tab-pane key="bans"><template #title><Ban :size="14" />自动封禁</template></a-tab-pane>');
    expect(source).toContain('activeTab === \'bans\'');
    expect(source).toContain("进入原因");
    expect(source).toContain("firewallStateLabel(record.firewallState)");
    expect(source).toContain("转为手动黑名单");
    expect(source).toContain("record.location");
  });

  it("uses security events for the trend and provides explicit empty states", () => {
    expect(source).toContain('import { buildSecurityTrend');
    expect(source).toContain("安全事件接口");
    expect(source).not.toContain('const trendData = [');
    expect(source).toContain("暂无趋势数据");
    expect(source).toContain("暂无高频来源");
  });

  it("moves the automatic refresh countdown into the refresh action", () => {
    expect(source).not.toContain('class="score-block"');
    expect(source).toContain("const refreshCountdown = ref(10)");
    expect(source).toContain("refreshCountdown.value -= 1");
    expect(source).toContain("window.setInterval(() => {");
    expect(source).toContain('{{ live ? `刷新 ${refreshCountdown}s` : "刷新" }}');
  });

  it("matches the shared system toolbar sizing for refresh and rule actions", () => {
    expect(source).toContain('class="uvp-page-action-btn uvp-refresh-btn" aria-label="刷新安全数据"');
    expect(source).toContain('<a-button v-else class="uvp-page-action-btn uvp-create-btn" type="primary"');
    expect(source).not.toContain("security-create-action");
  });

  it("keeps ban and access-list workspaces focused on their tables", () => {
    expect(source).not.toContain('class="ban-summary"');
    expect(source).not.toContain('class="rule-summary"');
    expect(source).not.toContain("自动封禁与手动黑名单分开管理");
    expect(source).not.toContain("这里只管理手动黑名单");
    expect(source).not.toContain("白名单不绕过协议校验");
  });

  it("uses the shared search panel for all four security lists", () => {
    expect(source).toContain('<s-layout-search class="security-list-search event-search-panel">');
    expect(source).toContain('<s-layout-search class="security-list-search ban-search-panel">');
    expect(source).toContain('<s-layout-search class="security-list-search rule-search-panel">');
    expect(source).toContain('placeholder="筛选本页来源、原因或策略"');
    expect(source).toContain('placeholder="筛选本页匹配内容或备注"');
    expect(source).toContain(':data="filteredAutoBans"');
    expect(source).toContain(':data="filteredCurrentRules"');
  });

  it("aligns list filters with the online-user search controls", () => {
    expect(source.match(/class="security-list-filter/g)).toHaveLength(7);
    expect(source).toContain('class="security-list-filter security-list-filter--keyword"');
    expect(source).toContain('class="security-list-filter security-list-filter--status"');
    expect(source).not.toMatch(/<a-(?:input|select)[^>]*style="width:/);
    expect(source).toMatch(/\.security-list-filter\s*{[^}]*flex:\s*0 1 176px;[^}]*width:\s*176px;/s);
    expect(source).toMatch(/\.security-list-filter :deep\(\.arco-input-wrapper\),\s*\.security-list-filter :deep\(\.arco-select-view\)\s*{[^}]*width:\s*100%;/s);
  });

  it("uses the existing Arco table pagination style for all security lists", () => {
    expect(source).toContain("const tablePageSizeOptions = [10, 20, 50, 100]");
    expect(source).toContain(':pagination="eventPagination"');
    expect(source).toContain(':pagination="banPagination"');
    expect(source).toContain(':pagination="rulePagination"');
    expect(source).toContain('showTotal: true');
    expect(source).toContain('showJumper: true');
    expect(source).toContain('showPageSize: true');
    expect(source).toContain('pageSizeOptions: tablePageSizeOptions');
    expect(source).toContain('@page-change="handleEventPageChange"');
    expect(source).toContain('@page-change="handleBanPageChange"');
    expect(source).toContain('@page-change="handleRulePageChange"');
  });

  it("keeps list scrolling inside each table workspace", () => {
    expect(source).toContain(":scroll=\"{ x: 1050, y: '100%' }\"");
    expect(source).toContain(":scroll=\"{ x: 1060, y: '100%' }\"");
    expect(source).toContain(":scroll=\"{ x: 900, y: '100%' }\"");
    expect(source).toMatch(/\.security-preview--workspace\s*{[^}]*overflow:\s*hidden;/s);
    expect(source).toMatch(/\.workspace-panel\s*{[^}]*display:\s*flex;[^}]*min-height:\s*0;/s);
    expect(source).toMatch(/\.security-table\s*{[^}]*flex:\s*1;[^}]*min-height:\s*0;/s);
  });

  it("uses the shared data table and pagination treatment", () => {
    expect(source).toContain("const tablePageSizeOptions = [10, 20, 50, 100]");
    expect(source).toContain('class="security-table uvp-data-table"');
    expect(source).toContain('class="security-table uvp-data-table ban-table"');
    expect(source).toMatch(/\.security-preview :deep\(\.uvp-data-table \.arco-table-cell\)\s*{[^}]*font-size:\s*14px;[^}]*line-height:\s*22px;/s);
    expect(source).toMatch(/\.security-preview :deep\(\.uvp-data-table \.arco-pagination-item\)/);
  });

  it("keeps security tables free of an outer frame", () => {
    expect(source).toMatch(/\.security-table\s*{[^}]*border:\s*0;[^}]*border-radius:\s*0;/s);
  });

  it("renders the protection status as a non-action indicator", () => {
    expect(source).toContain("security-live-status");
    expect(source).not.toContain('<a-tag :color="liveStatus.color" bordered>');
  });

  it("keeps later event pages stable during automatic refresh", () => {
    expect(source).toContain("const shouldRefreshEvents = forceLists || showMessage || eventPage.value === 1");
    expect(source).toContain("shouldRefreshEvents ? listSecurityEvents");
  });

  it("uses a compact framed layout for actionable security advice", () => {
    expect(source).toContain('class="attention-row"');
    expect(source).toMatch(/\.attention-row\s*{[^}]*display:\s*grid;/s);
    expect(source).toMatch(/\.attention-row\s*{[^}]*grid-template-columns:\s*36px minmax\(0, 1fr\) 18px;/s);
    expect(source).toMatch(/\.attention-row\s*{[^}]*border:\s*1px solid var\(--uvp-list-panel-border\);/s);
  });

  it("renders operational data from security APIs instead of fixed demo values", () => {
    expect(source).toContain("getSecurityAgentHealth()");
    expect(source).toContain("listSecurityAccessRules(\"blacklist\", { page:");
    expect(source).toContain("updateSecurityAccessRule(rule.id");
    expect(source).toContain("expiryToIso(ruleExpiry.value)");
    expect(source).toContain("自动封禁有效期");
    expect(source).toContain("automaticBanTTLLabel");
    expect(source).toContain("refreshCountdown.value = 10");
    expect(source).not.toContain('<strong>86</strong>');
    expect(source).not.toContain('<span class="attention-count">2</span>');
    expect(source).not.toContain('<a-tag>10 分钟</a-tag><ChevronRight');
  });

  it("sends the rule expiry through the real toggle payload path", () => {
    expect(source).toContain("buildAccessRuleTogglePayload");
    expect(source).toContain("buildAccessRuleTogglePayload(rule, enabled)");
  });

  it("explains the short window, fixed low-frequency INVITE rule and manual permanent release", () => {
    expect(source).toContain("formatShortWindowInviteRule");
    expect(source).toContain("shortWindowInviteRuleLabel");
    expect(source).toContain("10 分钟累计 10 次未授权 INVITE");
    expect(source).toContain("观察模式仅记录");
    expect(source).not.toContain("默认未知 INVITE 示例");
    expect(source).not.toContain("10 秒内 5 次");
    expect(source).not.toContain("有限 TTL");
    expect(source).not.toContain("有限TTL");
  });

  it("uses one Chinese reason formatter for event and ban records", () => {
    expect(source).toContain("formatSecurityReason");
    expect(source).toContain("rule: formatSecurityReason(event.reason)");
    expect(source).toContain("reason: formatSecurityReason(decision.reason)");
  });
});
