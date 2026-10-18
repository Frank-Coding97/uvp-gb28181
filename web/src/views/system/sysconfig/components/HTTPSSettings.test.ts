import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
const api = vi.hoisted(() => ({ status: vi.fn(), check: vi.fn(), certificate: vi.fn(), apply: vi.fn() }));
const modal = vi.hoisted(() => ({ confirm: vi.fn() }));
vi.mock("@/api/https-management", () => ({
  getHTTPSStatus: api.status,
  checkHTTPSPort: api.check,
  validateHTTPSCertificate: api.certificate,
  applyHTTPS: api.apply
}));
vi.mock("@arco-design/web-vue", () => ({ Message: { error: vi.fn(), success: vi.fn() }, Modal: modal }));
vi.mock("@/hooks/usePermission", () => ({ hasPermission: () => true }));
import HTTPSSettings from "./HTTPSSettings.vue";
const status = () => ({ supported: true, settings: { enabled: true, port: 51000 }, httpPort: 51001, nginxHealthy: true });
const render = () =>
  mount(HTTPSSettings, {
    global: {
      stubs: {
        "a-switch": {
          props: ["modelValue", "disabled"],
          emits: ["update:modelValue"],
          template:
            '<input type="checkbox" :checked="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\',$event.target.checked)" />'
        }
      }
    }
  });
describe("HTTPS settings", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.status.mockResolvedValue({ code: 0, data: status() });
    api.certificate.mockResolvedValue({ code: 0, data: { subject: "localhost", fingerprint: "abc", notAfter: "2027-01-01" } });
    api.apply.mockResolvedValue({ code: 0, data: { id: "op", state: "running" } });
  });
  it("unsupported installs explain why and disable apply", async () => {
    api.status.mockResolvedValue({ code: 0, data: { supported: false, reason: "仅支持绿色包" } });
    const w = render();
    await flushPromises();
    expect(w.text()).toContain("仅支持绿色包");
    expect(w.get('[data-testid="apply"]').attributes("disabled")).toBeDefined();
    w.unmount();
  });
  it("does not request a ZLM restart without uploaded material", async () => {
    const w = render();
    await flushPromises();
    await w.get('[data-testid="apply"]').trigger("click");
    expect(modal.confirm).toHaveBeenCalled();
    expect(modal.confirm.mock.calls[0][0].content).not.toContain("重启 ZLMediaKit");
    expect(api.apply).not.toHaveBeenCalled();
    w.unmount();
  });
  it("validates uploads and warns about ZLM restart only when selected", async () => {
    const w = render();
    await flushPromises();
    for (const field of ["certificate", "privateKey"]) {
      const input = w.get(`[data-testid="${field}"]`);
      Object.defineProperty(input.element, "files", { value: [new File(["pem"], `${field}.pem`)], configurable: true });
      await input.trigger("change");
    }
    await flushPromises();
    await w.get('[data-testid="apply"]').trigger("click");
    expect(modal.confirm.mock.calls.at(-1)![0].content).toContain("重启 ZLMediaKit");
    await w.get('[data-testid="sync-zlm"]').setValue(false);
    await w.get('[data-testid="apply"]').trigger("click");
    const confirm = modal.confirm.mock.calls.at(-1)![0];
    expect(confirm.content).not.toContain("重启 ZLMediaKit");
    await confirm.onOk();
    expect(api.apply.mock.calls[0][0].syncZlm).toBe(false);
    w.unmount();
  });
  it("shows a new address without declaring a disconnect successful", async () => {
    const w = render();
    await flushPromises();
    api.apply.mockRejectedValue(new Error("Network Error"));
    await w.get('[data-testid="apply"]').trigger("click");
    await modal.confirm.mock.calls[0][0].onOk();
    await flushPromises();
    expect(w.text()).toContain("尚未确认生效");
    expect(w.text()).toContain("https://");
    w.unmount();
  });
  it("submits exactly the files and settings shown at confirmation", async () => {
    const w = render();
    await flushPromises();
    const files: File[] = [];
    for (const field of ["certificate", "privateKey"]) {
      const file = new File(["pem"], `${field}.pem`);
      files.push(file);
      const input = w.get(`[data-testid="${field}"]`);
      Object.defineProperty(input.element, "files", { value: [file], configurable: true });
      await input.trigger("change");
    }
    await flushPromises();
    await w.get('[data-testid="apply"]').trigger("click");
    const confirm = modal.confirm.mock.calls.at(-1)![0];
    await w.get('input[type="checkbox"]').setValue(false);
    const input = w.get('[data-testid="certificate"]');
    Object.defineProperty(input.element, "files", { value: [new File(["new"], "replacement.pem")], configurable: true });
    await input.trigger("change");
    await flushPromises();
    await confirm.onOk();
    expect(api.apply).toHaveBeenCalledWith({ enabled: true, port: 51000, syncZlm: true }, files[0], files[1]);
    w.unmount();
  });
  it("clears submitted uploads after confirmed success so later saves do not restart ZLM", async () => {
    const w = render();
    await flushPromises();
    for (const field of ["certificate", "privateKey"]) {
      const input = w.get(`[data-testid="${field}"]`);
      Object.defineProperty(input.element, "files", { value: [new File(["pem"], `${field}.pem`)], configurable: true });
      await input.trigger("change");
    }
    await flushPromises();
    await w.get('[data-testid="apply"]').trigger("click");
    await modal.confirm.mock.calls.at(-1)![0].onOk();
    api.status.mockResolvedValue({
      code: 0,
      data: { ...status(), operation: { id: "op", state: "succeeded", nginx: "applied", zlm: "applied" } }
    });
    await w.get('[data-testid="refresh"]').trigger("click");
    await flushPromises();
    expect(w.find('[data-testid="sync-zlm"]').exists()).toBe(false);
    expect(w.text()).toContain("Nginx：已应用");
    await w.get('[data-testid="apply"]').trigger("click");
    expect(modal.confirm.mock.calls.at(-1)![0].content).not.toContain("重启 ZLMediaKit");
    w.unmount();
  });
});
