import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import ConfirmStep from "./ConfirmStep.vue";
import { writeTextToClipboard } from "@/utils/app";

vi.mock("@/utils/app", () => ({ writeTextToClipboard: vi.fn().mockResolvedValue(true) }));

describe("ConfirmStep SIP domain", () => {
  it("displays and copies the custom domain from the current form", async () => {
    const wrapper = mount(ConfirmStep, {
      props: {
        form: {
          deploymentMode: "lan",
          listenIp: "192.168.1.10",
          advertiseIp: "",
          advertiseIpInferred: false,
          port: 5062,
          hookIp: "192.0.2.12",
          streamIp: "media.example.com",
          serverId: "34020000002000000002",
          domain: "4401000000",
          password: ""
        },
        hasPassword: true,
        network: null
      }
    });
    const row = wrapper.findAll(".confirm-row").find(item => item.find(".confirm-row__label").text() === "SIP 域");
    expect(row).toBeDefined();
    expect(row?.find("code").text()).toBe("4401000000");
    await row?.find("button").trigger("click");
    expect(writeTextToClipboard).toHaveBeenCalledWith("4401000000");
    wrapper.unmount();
  });

  it("shows and copies the platform media address defaults", async () => {
    const wrapper = mount(ConfirmStep, {
      props: {
        form: {
          deploymentMode: "lan",
          listenIp: "192.168.1.10",
          advertiseIp: "",
          advertiseIpInferred: false,
          hookIp: "192.0.2.12",
          streamIp: "media.example.com",
          port: 5062,
          serverId: "34020000002000000002",
          domain: "3402000000",
          password: ""
        },
        hasPassword: true,
        network: null
      }
    });
    const rows = wrapper.findAll(".confirm-row");
    const hookRow = rows.find(item => item.find(".confirm-row__label").text() === "Hook IP（平台默认）");
    const streamRow = rows.find(item => item.find(".confirm-row__label").text() === "Stream IP（平台默认）");
    expect(hookRow?.find("code").text()).toBe("192.0.2.12");
    expect(streamRow?.find("code").text()).toBe("media.example.com");
    await streamRow?.find("button").trigger("click");
    expect(writeTextToClipboard).toHaveBeenCalledWith("media.example.com");
    wrapper.unmount();
  });
});
