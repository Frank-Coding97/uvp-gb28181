import { describe, expect, it } from "vitest";
import { compareOccurredAt } from "./helpers";

// 会话详情页的时序图依赖这个比较器：报文时间戳是 RFC3339Nano，
// 用 Date（毫秒）重排会把同一毫秒内的请求/应答顺序抹平。
describe("compareOccurredAt", () => {
  it("同毫秒内保持请求先于应答，即使输入是倒序", () => {
    const request = "2026-10-29T03:45:05.950009544Z";
    const response = "2026-10-29T03:45:05.950161522Z";
    expect(compareOccurredAt(request, response)).toBeLessThan(0);
    expect(compareOccurredAt(response, request)).toBeGreaterThan(0);
    // 倒序输入（列表接口按 DESC 返回）排序后必须是请求在前
    const sorted = [response, request].sort(compareOccurredAt);
    expect(sorted).toEqual([request, response]);
  });

  it("跨秒、跨分钟按真实时间比较", () => {
    expect(compareOccurredAt("2026-10-29T03:45:05.999999999Z", "2026-10-29T03:45:06.000000001Z")).toBeLessThan(0);
    expect(compareOccurredAt("2026-10-29T03:45:59.5Z", "2026-10-29T03:46:00.1Z")).toBeLessThan(0);
  });

  it("小数位数不同（含整秒）也能正确比较", () => {
    expect(compareOccurredAt("2026-10-29T03:45:05Z", "2026-10-29T03:45:05.000000001Z")).toBeLessThan(0);
    expect(compareOccurredAt("2026-10-29T03:45:05.95Z", "2026-10-29T03:45:05.950161522Z")).toBeLessThan(0);
  });

  it("同一时刻不同时区写法视为相等（剥小数不能丢掉时区）", () => {
    expect(compareOccurredAt("2026-10-29T03:45:05.950009544Z", "2026-10-29T11:45:05.950009544+08:00")).toBe(0);
  });

  it("无法解析的时间戳按同一秒处理，不产生 NaN 排序", () => {
    expect(compareOccurredAt("not-a-time", "2026-10-29T03:45:05.950009544Z")).toBeLessThan(0);
    expect(Number.isNaN(compareOccurredAt("not-a-time", "not-a-time"))).toBe(false);
  });

  it("现场注册会话（接口 DESC 返回）排序后是 请求→401→请求→200", () => {
    // 取自线上设备 33010025031320300001 的 Call-ID 1199591389，
    // 前两条相差 0.15ms —— 用毫秒重排时 401 会跑到 REGISTER 前面。
    const fromApi = [
      "2026-10-29T03:45:06.055417132Z", // 200 CSeq 2
      "2026-10-29T03:45:06.050262081Z", // REGISTER CSeq 2
      "2026-10-29T03:45:05.950161522Z", // 401 CSeq 1
      "2026-10-29T03:45:05.950009544Z" // REGISTER CSeq 1
    ];
    expect(fromApi.slice().sort(compareOccurredAt)).toEqual([
      "2026-10-29T03:45:05.950009544Z",
      "2026-10-29T03:45:05.950161522Z",
      "2026-10-29T03:45:06.050262081Z",
      "2026-10-29T03:45:06.055417132Z"
    ]);
  });
});
