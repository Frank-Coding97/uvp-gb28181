import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * 设备详情「上报字符集」的只读契约（2026-11-03）。
 *
 * 数据链路：设备 MESSAGE 报文 → 后端 `manscdp.SniffIncomingCharset`（声明 + 内容嗅探）
 * → `gb_device.reported_charset / wire_charset` → 详情接口 → 这里的「字符集」行。
 * 文案规则在 `../reportedCharsetText.ts`（带行为单测），本文件只钉"详情区确实展示它"。
 */
const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");

describe("device detail reported charset row", () => {
  it("接入信息卡里有一行「字符集」，声明与实际判定值一起喂给文案函数", () => {
    expect(source).toContain('<span class="k">字符集</span>');
    expect(source).toContain("reportedCharsetText(deviceDetail.reportedCharset, deviceDetail.wireCharset)");
    expect(source).toContain("deviceDetail.reportedCharsetAt");
  });

  it("文案函数从独立模块引入（行为单测在 reportedCharsetText.test.ts），不在 SFC 里内联", () => {
    expect(source).toContain('import { reportedCharsetText } from "../reportedCharsetText"');
    expect(source).not.toContain("function reportedCharsetText(");
  });
});
