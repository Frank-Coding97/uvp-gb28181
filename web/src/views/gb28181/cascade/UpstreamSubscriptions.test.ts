import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import type { CascadeSubscriptions } from "@/api/gb28181";
import UpstreamSubscriptions from "./UpstreamSubscriptions.vue";

function snapshot(): CascadeSubscriptions {
  return {
    catalog: {
      state: "active",
      sessions: [
        {
          target: "34020000002000000001",
          event: "Catalog;id=1",
          subscribedAt: "2026-10-19T10:00:00+08:00",
          renewedAt: "2026-10-19T10:01:00+08:00",
          expiresAt: "2026-10-19T10:11:00+08:00",
          lastNotifyAt: "2026-10-19T10:02:00+08:00",
          lastNotifyStatus: "success",
          lastNotifyCode: 200
        },
        {
          target: "34020000001320000002",
          event: "Catalog;id=2",
          subscribedAt: "2026-10-19T10:00:00+08:00",
          renewedAt: "2026-10-19T10:00:00+08:00",
          expiresAt: "2026-10-19T10:10:00+08:00",
          lastNotifyStatus: "failed",
          lastNotifyCode: 500
        }
      ]
    },
    mobilePosition: { state: "unsupported", sessions: [] },
    ptzPosition: { state: "unsupported", sessions: [] },
    alarm: { state: "unsupported", sessions: [] }
  };
}

describe("upstream subscription ledger", () => {
  it("renders four types, multiple scopes and actual notify outcomes", () => {
    const wrapper = mount(UpstreamSubscriptions, { props: { subscriptions: snapshot() } });
    const text = wrapper.text();
    for (const label of ["目录", "位置", "PTZ 精准", "告警"]) expect(text).toContain(label);
    expect(text).toContain("已订阅（2）");
    expect(text.match(/尚未接通/g)).toHaveLength(3);
    expect(text).toContain("34020000001320000002");
    expect(text).toContain("Catalog;id=2");
    expect(text).toContain("2026-10-19 10:11:00");
    expect(text).toContain("成功（200）");
    expect(text).toContain("失败（500）");
  });

  it("renders all four active subscription kinds and their own event details", async () => {
    const value = snapshot();
    const catalogSession = value.catalog.sessions?.[0];
    if (!catalogSession) throw new Error("catalog fixture needs an active session");
    for (const [key, event] of [
      ["mobilePosition", "presence;id=position"],
      ["ptzPosition", "PTZPosition;id=ptz"],
      ["alarm", "presence;id=alarm"]
    ] as const) {
      value[key] = {
        state: "active",
        sessions: [{ ...catalogSession, event }]
      };
    }
    const wrapper = mount(UpstreamSubscriptions, { props: { subscriptions: value } });
    expect(wrapper.text().match(/已订阅（/g)).toHaveLength(4);
    expect(wrapper.text()).not.toContain("尚未接通");
    for (const event of ["presence;id=position", "PTZPosition;id=ptz", "presence;id=alarm"]) {
      expect(wrapper.text()).toContain(event);
    }
    await wrapper.setProps({ subscriptions: { ...value, alarm: { state: "inactive", sessions: [] } } });
    expect(wrapper.text().match(/已订阅（/g)).toHaveLength(3);
    expect(wrapper.text()).toContain("未订阅");
    expect(wrapper.text()).not.toContain("presence;id=alarm");
    expect(wrapper.text()).toContain("presence;id=position");
  });

  it("updates after cancellation and distinguishes unavailable, unsupported and old API", async () => {
    const wrapper = mount(UpstreamSubscriptions, { props: { subscriptions: snapshot() } });
    const value = snapshot();
    value.catalog = { state: "inactive", sessions: [] };
    await wrapper.setProps({ subscriptions: value });
    expect(wrapper.text()).toContain("未订阅");
    expect(wrapper.text()).not.toContain("34020000001320000002");
    value.catalog = { state: "unavailable", sessions: [] };
    await wrapper.setProps({ subscriptions: { ...value } });
    expect(wrapper.text()).toContain("运行时不可用");
    await wrapper.setProps({ subscriptions: undefined });
    expect(wrapper.text().match(/状态未知/g)).toHaveLength(4);
    expect(wrapper.text()).not.toContain("未订阅");
  });

  it("hides stale active snapshots after list refresh fails", () => {
    const wrapper = mount(UpstreamSubscriptions, { props: { subscriptions: snapshot(), stale: true } });
    expect(wrapper.text()).toContain("状态未知");
    expect(wrapper.text()).not.toContain("已订阅");
    expect(wrapper.text()).not.toContain("34020000001320000002");
  });
});
