import { describe, expect, it } from "vitest";
import { reportedCharsetText } from "./reportedCharsetText";

describe("reportedCharsetText", () => {
  it("两侧都空 = 设备还没上报过", () => {
    expect(reportedCharsetText(null, null)).toBe("未上报");
    expect(reportedCharsetText("", "")).toBe("未上报");
  });

  it("声明与实际一致 = 只显示声明值", () => {
    expect(reportedCharsetText("GB2312", "GB2312")).toBe("GB2312");
    expect(reportedCharsetText(" UTF-8 ", "UTF-8")).toBe("UTF-8");
  });

  it("声明与实际不一致 = 两个都展示（设备声明不可信是乱码工单的关键线索）", () => {
    expect(reportedCharsetText("GB18030", "UTF-8")).toBe("GB18030（实测按 UTF-8 解）");
  });

  it("只报了实际判定值 = 报文没写声明，按内容检测出的编码", () => {
    expect(reportedCharsetText(null, "GB18030")).toBe("未声明（实测 GB18030）");
  });
});
