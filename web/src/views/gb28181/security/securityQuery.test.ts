import { describe, expect, it } from "vitest";
import { firewallStateParam, isEmptyQuery, matchTypeParam, normalizeKeyword, severityParam, statusParam } from "./securityQuery";

describe("securityQuery 中文选项到服务端参数", () => {
  it("翻译风险等级，'全部'不算条件", () => {
    expect(severityParam("高危")).toBe("high");
    expect(severityParam("中危")).toBe("medium");
    expect(severityParam("低危")).toBe("low");
    expect(severityParam("全部风险")).toBeUndefined();
  });

  it("翻译防火墙状态与规则类型/状态", () => {
    expect(firewallStateParam("已生效")).toBe("applied");
    expect(firewallStateParam("待同步")).toBe("pending");
    expect(firewallStateParam("同步失败")).toBe("failed");
    expect(firewallStateParam("全部状态")).toBeUndefined();
    expect(matchTypeParam("IP")).toBe("ip");
    expect(matchTypeParam("CIDR")).toBe("cidr");
    expect(matchTypeParam("User-Agent")).toBe("user_agent");
    expect(statusParam("已启用")).toBe("enabled");
    expect(statusParam("已停用")).toBe("disabled");
    expect(statusParam("全部类型")).toBeUndefined();
  });

  it("未知文案不猜，直接不过滤", () => {
    expect(severityParam("紧急")).toBeUndefined();
    expect(matchTypeParam("域名")).toBeUndefined();
    expect(statusParam("归档")).toBeUndefined();
  });

  it("关键字只做 trim，不把中文翻成编码（后端按原始值匹配）", () => {
    expect(normalizeKeyword("  203.0.113.1  ")).toBe("203.0.113.1");
    expect(normalizeKeyword("密码或鉴权配置错误")).toBe("密码或鉴权配置错误");
  });

  it("空判定同时看关键字和下拉条件", () => {
    expect(isEmptyQuery({ keyword: "" })).toBe(true);
    expect(isEmptyQuery({ keyword: "", filter: "high" })).toBe(false);
    expect(isEmptyQuery({ keyword: "1.1.1.1" })).toBe(false);
  });
});
