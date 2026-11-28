import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { mount } from "@vue/test-utils";
import ArcoVue from "@arco-design/web-vue";
import { describe, expect, it } from "vitest";
import IdentityStep from "./IdentityStep.vue";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/sip/steps/IdentityStep.vue"), "utf8");

const form = {
  deploymentMode: "lan" as const,
  listenIp: "0.0.0.0",
  advertiseIp: "",
  advertiseIpInferred: false,
  hookIp: "",
  sdpIp: "192.168.10.220",
  streamIp: "",
  port: 5062,
  serverId: "34020000002000000002",
  domain: "4401000000",
  password: ""
};

const mountStep = (overrides: Partial<typeof form> = {}, hasExistingPassword = true) =>
  mount(IdentityStep, {
    props: { form: { ...form, ...overrides }, hasExistingPassword },
    global: { plugins: [ArcoVue] }
  });

describe("IdentityStep domain editing", () => {
  it("preserves a custom domain on mount and when the platform ID changes", async () => {
    const wrapper = mount(IdentityStep, {
      props: { form: { ...form }, hasExistingPassword: true },
      global: { plugins: [ArcoVue] }
    });
    expect(wrapper.emitted("update")).toBeUndefined();
    const inputs = wrapper.findAll("input");
    expect(inputs[2].attributes("disabled")).toBeUndefined();
    await inputs[2].setValue("1101000000");
    expect(wrapper.emitted("update")?.at(-1)).toEqual([{ domain: "1101000000" }]);
    await inputs[1].setValue("51010000002000000001");
    expect(wrapper.emitted("update")?.at(-1)).toEqual([{ serverId: "51010000002000000001" }]);
    await wrapper.setProps({ form: { ...form, serverId: "51010000002000000001" } });
    expect(wrapper.emitted("update")).toHaveLength(2);
  });

  it("initializes an empty domain from the platform ID", () => {
    const wrapper = mount(IdentityStep, {
      props: { form: { ...form, domain: "" }, hasExistingPassword: true },
      global: { plugins: [ArcoVue] }
    });
    expect(wrapper.emitted("update")?.[0]).toEqual([{ domain: "3402000000" }]);
  });

  it("shows a digit counter next to the SIP domain and marks it complete at 10 digits", async () => {
    const wrapper = mount(IdentityStep, {
      props: { form: { ...form }, hasExistingPassword: true },
      global: { plugins: [ArcoVue] }
    });
    // 与 SIP 平台 ID 一致:两个字段各有一个位数计数器(平台 ID 20 位 / 域 10 位).
    const counters = wrapper.findAll(".s-counter-suffix");
    expect(counters).toHaveLength(2);
    expect(counters[1].text()).toBe("10/10");
    expect(counters[1].classes()).toContain("is-complete");

    await wrapper.setProps({ form: { ...form, domain: "4401" } });
    const short = wrapper.findAll(".s-counter-suffix")[1];
    expect(short.text()).toBe("4/10");
    expect(short.classes()).not.toContain("is-complete");
  });

  it("reports a domain error only after the field was touched", async () => {
    const wrapper = mount(IdentityStep, {
      props: { form: { ...form, domain: "4401" }, hasExistingPassword: true },
      global: { plugins: [ArcoVue] }
    });
    expect(wrapper.find(".identity-form").text()).not.toContain("必须为 10 位数字");
    await wrapper.findAll("input")[2].trigger("blur");
    expect(wrapper.find(".identity-form").text()).toContain("必须为 10 位数字");
  });
});

// 观感与「修改初始密码」弹窗同一套(共享组件 s-password-rules):强度条 + 逐条规则打对号。
describe("IdentityStep password format checklist", () => {
  const ruleItems = (wrapper: ReturnType<typeof mountStep>) =>
    wrapper.findAll('[data-testid="sip-password-rule-list"] .password-rules__item');
  const ruleStates = (wrapper: ReturnType<typeof mountStep>) =>
    ruleItems(wrapper).map(item => item.classes().includes("is-satisfied"));

  it("lists the four SIP password rules, all unticked while the field is empty", () => {
    const wrapper = mountStep({ password: "" });
    expect(ruleItems(wrapper)).toHaveLength(4);
    expect(ruleStates(wrapper)).toEqual([false, false, false, false]);
    expect(wrapper.get(".password-strength__label").text()).toBe("请输入密码");
    // 空框不该出现"不是弱口令"这类反向规则的绿对号
    expect(ruleItems(wrapper).every(item => !item.find("svg").exists())).toBe(true);
  });

  it("ticks every rule and shows the tick icon for a compliant password", async () => {
    const wrapper = mountStep({ password: "" });

    await wrapper.setProps({ form: { ...form, password: "abcdefgh" } });
    expect(ruleStates(wrapper)).toEqual([true, false, true, false]);
    expect(wrapper.get(".password-strength__label").text()).toBe("继续完善");
    expect(ruleItems(wrapper)[2].find("svg").exists()).toBe(true);
    expect(ruleItems(wrapper)[3].find("svg").exists()).toBe(false);

    await wrapper.setProps({ form: { ...form, password: "Ab1!abcd" } });
    expect(ruleStates(wrapper)).toEqual([true, true, true, true]);
    expect(wrapper.get(".password-strength__label").text()).toBe("符合要求");
    expect(ruleItems(wrapper)[3].find("svg").exists()).toBe(true);
  });

  it("keeps the checklist under the password input instead of beside it", () => {
    // ⛔ 结构防回归:右侧并列 = 清单被当成了 a-form-item 内容区(flex row)的兄弟元素。
    const wrapper = mountStep({ password: "" });
    const list = wrapper.get('[data-testid="sip-password-rule-list"]');
    expect(list.element.closest(".identity-form__password-field")).not.toBeNull();
    expect(source).toContain("identity-form__password-field");
  });
});
