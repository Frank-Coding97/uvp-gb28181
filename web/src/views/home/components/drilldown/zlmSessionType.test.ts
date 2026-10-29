import { describe, expect, it } from "vitest";
import {
  DICT_CODE_ZLM_SESSION_TYPE,
  ZLM_SESSION_TYPE_EMPTY_TEXT,
  ZLM_SESSION_TYPE_LABEL_FALLBACK,
  zlmSessionTypeLabel
} from "./zlmSessionType";

describe("zlmSessionTypeLabel", () => {
  it("带命名空间的类型直接查表", () => {
    expect(zlmSessionTypeLabel("mediakit::HttpSession")).toBe("HTTP 会话");
    expect(zlmSessionTypeLabel("mediakit::RtpSession")).toBe("RTP 会话");
  });

  it("只给短名时补 mediakit:: 前缀（接口两种形态都要认）", () => {
    expect(zlmSessionTypeLabel("RtpSession")).toBe("RTP 会话");
    expect(zlmSessionTypeLabel("  WebSocketSession  ")).toBe("WebSocket 会话");
  });

  it("空值 → 未知（不渲染空占位）", () => {
    expect(zlmSessionTypeLabel("")).toBe(ZLM_SESSION_TYPE_EMPTY_TEXT);
    expect(zlmSessionTypeLabel("   ")).toBe(ZLM_SESSION_TYPE_EMPTY_TEXT);
    expect(zlmSessionTypeLabel(null)).toBe(ZLM_SESSION_TYPE_EMPTY_TEXT);
    expect(zlmSessionTypeLabel(undefined)).toBe(ZLM_SESSION_TYPE_EMPTY_TEXT);
  });

  it("⛔ 未命中回显原值（排障要看得见 ZLM 到底报了什么）", () => {
    expect(zlmSessionTypeLabel("mediakit::FutureSession")).toBe("mediakit::FutureSession");
    expect(zlmSessionTypeLabel("Weird")).toBe("Weird");
  });

  it("值域锁定：兜底表恰好这 9 档 ZLM 会话类型", () => {
    expect(Object.keys(ZLM_SESSION_TYPE_LABEL_FALLBACK).sort()).toEqual([
      "mediakit::HttpSession",
      "mediakit::RtmpSession",
      "mediakit::RtpSession",
      "mediakit::RtspSession",
      "mediakit::SrtSession",
      "mediakit::TcpSession",
      "mediakit::UdpSession",
      "mediakit::WebRtcSession",
      "mediakit::WebSocketSession"
    ]);
  });

  it("字典只能改名字，改不了「未命中回显原值」这条契约", () => {
    const labels = { ...ZLM_SESSION_TYPE_LABEL_FALLBACK, "mediakit::RtpSession": "RTP 专线" };
    expect(zlmSessionTypeLabel("RtpSession", labels)).toBe("RTP 专线");
    expect(zlmSessionTypeLabel("mediakit::FutureSession", labels)).toBe("mediakit::FutureSession");
  });

  it("字典 code 固定", () => {
    expect(DICT_CODE_ZLM_SESSION_TYPE).toBe("zlm_session_type");
  });
});
