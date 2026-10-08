import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { defineComponent, h } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import InitialPasswordDialog from "./InitialPasswordDialog.vue";

const api = vi.hoisted(() => ({ changeInitialPasswordAPI: vi.fn() }));
vi.mock("@/api/user", () => ({ changeInitialPasswordAPI: api.changeInitialPasswordAPI }));
vi.mock("@arco-design/web-vue", () => ({
  Message: { warning: vi.fn(), success: vi.fn() }
}));

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

describe("initial password dialog", () => {
  beforeEach(() => api.changeInitialPasswordAPI.mockReset().mockResolvedValue({ code: 0 }));

  it("uses the dedicated API and cannot be dismissed before submission", () => {
    expect(source).toContain("changeInitialPasswordAPI");
    expect(source).toContain(':closable="false"');
    expect(source).toContain(':mask-closable="false"');
    expect(source).toContain(':esc-to-close="false"');
    expect(source).toContain("confirmPassword");
  });

  it("submits matching passwords and emits success only after the API succeeds", async () => {
    const wrapper = mountDialog();
    const inputs = wrapper.findAll("input");
    await inputs[0].setValue("new-password!");
    await inputs[1].setValue("new-password!");
    await wrapper.get("form").trigger("submit");
    await flushPromises();

    expect(api.changeInitialPasswordAPI).toHaveBeenCalledWith({ password: "new-password!", confirmPassword: "new-password!" });
    expect(wrapper.emitted("success")).toHaveLength(1);
  });

  it("does not call the API when the confirmation differs", async () => {
    const wrapper = mountDialog();
    const inputs = wrapper.findAll("input");
    await inputs[0].setValue("new-password!");
    await inputs[1].setValue("different-password!");
    await wrapper.get("form").trigger("submit");

    expect(api.changeInitialPasswordAPI).not.toHaveBeenCalled();
    expect(wrapper.emitted("success")).toBeUndefined();
  });
});
