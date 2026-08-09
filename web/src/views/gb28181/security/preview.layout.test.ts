import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/security/preview.vue"), "utf8");

describe("security preview system integration", () => {
  it("uses the UVP workspace shell and theme tokens", () => {
    expect(source).toContain('class="snow-page security-preview"');
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

  it("binds the rule drawer form to a model", () => {
    expect(source).toContain(':model="ruleFormModel"');
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
    expect(source).toContain("Digest、Nonce");
  });

  it("renders firewall agent health from the live snapshot instead of demo text", () => {
    expect(source).toContain("securityAgent.value.checkedAt");
    expect(source).toContain("agentStatus.detail");
    expect(source).toContain("主机防火墙未连接");
    expect(source).not.toContain("最后心跳 3 秒前 · 已应用 18 条规则");
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

  it("separates automatic firewall bans from manual blacklist rules", () => {
    expect(source).toContain('<a-tab-pane key="bans" title="自动封禁" />');
    expect(source).toContain('activeTab === \'bans\'');
    expect(source).toContain("进入原因");
    expect(source).toContain("主机防火墙{{ record.firewallState }}");
    expect(source).toContain("转为手动黑名单");
    expect(source).toContain("这里只管理手动黑名单");
  });

  it("uses security events for the trend and provides explicit empty states", () => {
    expect(source).toContain('import { buildSecurityTrend');
    expect(source).toContain("安全事件接口");
    expect(source).not.toContain('const trendData = [');
    expect(source).toContain("暂无趋势数据");
    expect(source).toContain("暂无高频来源");
    expect(source).toContain("window.setInterval(() => refreshPreview(), 10000)");
  });

  it("uses a compact framed layout for actionable security advice", () => {
    expect(source).toContain('class="attention-row"');
    expect(source).toMatch(/\.attention-row\s*{[^}]*display:\s*grid;/s);
    expect(source).toMatch(/\.attention-row\s*{[^}]*grid-template-columns:\s*36px minmax\(0, 1fr\) 18px;/s);
    expect(source).toMatch(/\.attention-row\s*{[^}]*border:\s*1px solid var\(--uvp-list-panel-border\);/s);
  });

  it("renders operational data from security APIs instead of fixed demo values", () => {
    expect(source).toContain("getSecurityAgentHealth()");
    expect(source).toContain("listSecurityAccessRules(\"blacklist\")");
    expect(source).toContain("updateSecurityAccessRule(rule.id");
    expect(source).toContain("expiryToIso(ruleExpiry.value)");
    expect(source).toContain("自动封禁有效期");
    expect(source).toContain("永久加入防护");
    expect(source).toContain("window.setInterval(() => refreshPreview(), 10000)");
    expect(source).not.toContain('<strong>86</strong>');
    expect(source).not.toContain('<span class="attention-count">2</span>');
    expect(source).not.toContain('<a-tag>10 分钟</a-tag><ChevronRight');
  });
});
