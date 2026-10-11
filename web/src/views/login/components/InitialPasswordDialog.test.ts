import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { defineComponent, h } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import InitialPasswordDialog from "./InitialPasswordDialog.vue";
import { useSysConfigStore } from "@/store/modules/sys-config";

const api = vi.hoisted(() => ({ changeInitialPasswordAPI: vi.fn(), warning: vi.fn(), success: vi.fn() }));
vi.mock("@/api/user", () => ({ changeInitialPasswordAPI: api.changeInitialPasswordAPI }));
vi.mock("@arco-design/web-vue", () => ({ Message: { warning: api.warning, success: api.success } }));

const source = readFileSync(resolve(process.cwd(), "src/views/login/components/InitialPasswordDialog.vue"), "utf8");

const PasswordInputStub = defineComponent({
  props: { modelValue: { type: String, default: "" } },
  emits: ["update:modelValue"],
  setup(props, { emit }) {
    return () =>
      h("input", {
        type: "password",
        value: props.modelValue,
        onInput: (event: Event) => emit("update:modelValue", (event.target as HTMLInputElement).value)
      });
  }
});

const mountDialog = () =>
  mount(InitialPasswordDialog, {
    global: {
      stubs: {
        "a-modal": {
          props: ["visible", "closable", "maskClosable", "escToClose", "footer"],
          template: "<section data-testid='dialog'><slot /></section>"
        },
        "a-form": {
          emits: ["submit"],
          template: "<form @submit.prevent=\"$emit('submit', { errors: undefined })\"><slot /></form>"
        },
        "a-form-item": { template: "<label><slot /></label>" },
        "a-input-password": PasswordInputStub,
        "a-button": { template: "<button type='submit'><slot /></button>" }
      }
    }
  });

/** 每条规则是否已满足（顺序 = 长度 / 大小写 / 特殊字符）。 */
const passwordRuleItems = (wrapper: VueWrapper) => wrapper.findAll('[data-testid="password-rule-list"] .password-rules__item');
const ruleStates = (wrapper: VueWrapper) => passwordRuleItems(wrapper).map(item => item.classes().includes("is-satisfied"));
/** 确认框那一行（对号反馈）。 */
const confirmRuleItem = (wrapper: VueWrapper) => wrapper.get('[data-testid="confirm-rule-list"] .password-rules__item');

describe("initial password dialog", () => {
  beforeEach(() => {
    api.changeInitialPasswordAPI.mockReset().mockResolvedValue({ code: 0 });
    api.warning.mockReset();
    api.success.mockReset();
    // 策略由「系统配置 → 安全配置」驱动，用例里显式给定以免依赖 store 占位默认值
    const store = useSysConfigStore();
    store.safeConfig = { ...store.safeConfig, minPasswordLength: 8, requireSpecialChar: true };
  });

  it("uses the dedicated API and cannot be dismissed before submission", () => {
    expect(source).toContain("changeInitialPasswordAPI");
    expect(source).toContain(':closable="false"');
    expect(source).toContain(':mask-closable="false"');
    expect(source).toContain(':esc-to-close="false"');
    expect(source).toContain("confirmPassword");
  });

  it("shows a strength indicator driven by the shared password policy", () => {
    expect(source).toContain("buildPasswordPolicyRules");
    expect(source).toContain("passwordRules");
    expect(source).toContain("password-strength__segment");
    expect(source).toContain("符合要求");
  });

  it("turns each rule into a checklist item with a tick icon once satisfied", () => {
    expect(source).toContain("password-rules");
    expect(source).toContain("password-rules__mark");
    // 确认框同样带一条对号行，避免"只有上面三条例外"
    expect(source).toContain('data-rule="confirm-match"');
  });

  it("ticks every rule as the password gets stronger", async () => {
    const wrapper = mountDialog();
    expect(passwordRuleItems(wrapper)).toHaveLength(3);

    await wrapper.findAll("input")[0].setValue("abcdefg!");
    expect(ruleStates(wrapper)).toEqual([true, false, true]);
    // 只有满足的那几条才带对号图标
    expect(passwordRuleItems(wrapper)[0].find("svg").exists()).toBe(true);
    expect(passwordRuleItems(wrapper)[1].find("svg").exists()).toBe(false);

    await wrapper.findAll("input")[0].setValue("Abcdefg!");
    expect(ruleStates(wrapper)).toEqual([true, true, true]);
    expect(passwordRuleItems(wrapper)[1].find("svg").exists()).toBe(true);
  });

  it("keeps the confirmation row below the input instead of beside it", () => {
    // ⛔ 结构防回归：右侧那条对号 = 清单被当成 a-form-item 内容区（flex row）的兄弟元素。
    //    必须包在字段容器里，才会落到输入框下方。
    const wrapper = mountDialog();
    const list = wrapper.get('[data-testid="confirm-rule-list"]');
    expect(list.element.parentElement?.classList.contains("initial-password-dialog__password-field")).toBe(true);
    expect(source).toContain("initial-password-dialog__password-field");
  });

  it("ticks the confirmation row only when both inputs match", async () => {
    const wrapper = mountDialog();
    const inputs = wrapper.findAll("input");

    // 两个框都空时不算一致，否则会给出误导性的对号
    expect(confirmRuleItem(wrapper).classes()).not.toContain("is-satisfied");
    expect(confirmRuleItem(wrapper).text()).toContain("两次输入的密码需一致");
    expect(confirmRuleItem(wrapper).find("svg").exists()).toBe(false);

    await inputs[0].setValue("New-password!");
    await inputs[1].setValue("New-passwor");
    expect(confirmRuleItem(wrapper).classes()).not.toContain("is-satisfied");
    expect(confirmRuleItem(wrapper).text()).toContain("两次输入的密码不一致");

    await inputs[1].setValue("New-password!");
    expect(confirmRuleItem(wrapper).classes()).toContain("is-satisfied");
    expect(confirmRuleItem(wrapper).text()).toContain("两次输入的密码一致");
    expect(confirmRuleItem(wrapper).find("svg").exists()).toBe(true);

    // 清空确认框后对号要收回
    await inputs[1].setValue("");
    expect(confirmRuleItem(wrapper).classes()).not.toContain("is-satisfied");
    expect(confirmRuleItem(wrapper).find("svg").exists()).toBe(false);
  });

  it("blocks submission while a rule is unmet and explains which one", async () => {
    const wrapper = mountDialog();
    const inputs = wrapper.findAll("input");
    await inputs[0].setValue("abcdefg!");
    await inputs[1].setValue("abcdefg!");
    await wrapper.get("form").trigger("submit");

    expect(api.changeInitialPasswordAPI).not.toHaveBeenCalled();
    expect(api.warning).toHaveBeenCalledWith("密码必须同时包含大写和小写字母");
  });

  it("submits matching passwords and emits success only after the API succeeds", async () => {
    const wrapper = mountDialog();
    const inputs = wrapper.findAll("input");
    await inputs[0].setValue("New-password!");
    await inputs[1].setValue("New-password!");
    await wrapper.get("form").trigger("submit");
    await flushPromises();

    expect(api.changeInitialPasswordAPI).toHaveBeenCalledWith({ password: "New-password!", confirmPassword: "New-password!" });
    expect(wrapper.emitted("success")).toHaveLength(1);
  });

  it("does not call the API when the confirmation differs", async () => {
    const wrapper = mountDialog();
    const inputs = wrapper.findAll("input");
    await inputs[0].setValue("New-password!");
    await inputs[1].setValue("Different-password!");
    await wrapper.get("form").trigger("submit");

    expect(api.changeInitialPasswordAPI).not.toHaveBeenCalled();
    expect(wrapper.emitted("success")).toBeUndefined();
  });
});
