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
  it("switches the displayed port and address while retaining the editable HTTPS port", async () => {
    const w = render();
    await flushPromises();
    const port = w.get('input[type="number"]');
    await port.setValue(51443);
    expect(w.get('[data-testid="access-preview"]').text()).toContain("https://localhost:51443");
    await w.get('input[type="checkbox"]').setValue(false);
    expect((port.element as HTMLInputElement).value).toBe("51001");
    expect(port.attributes("disabled")).toBeDefined();
    expect(w.get('[data-testid="access-preview"]').text()).toContain("http://localhost:51001");
    await w.get('[data-testid="apply"]').trigger("click");
    const confirm = modal.confirm.mock.calls.at(-1)![0];
    expect(confirm.content).toContain("http://localhost:51001");
    await confirm.onOk();
    expect(api.apply).toHaveBeenCalledWith({ enabled: false, port: 51443, syncZlm: false }, undefined, undefined);
    api.status.mockResolvedValue({ code: 0, data: { ...status(), settings: { enabled: false, port: 51443 } } });
    await w.get('[data-testid="refresh"]').trigger("click");
    await flushPromises();
    await w.get('input[type="checkbox"]').setValue(true);
    expect((port.element as HTMLInputElement).value).toBe("51443");
    expect(port.attributes("disabled")).toBeUndefined();
    w.unmount();
  });
  it("shows the configured HTTP port immediately when HTTPS is already disabled", async () => {
    api.status.mockResolvedValue({
      code: 0,
      data: { ...status(), settings: { enabled: false, port: 51443 }, httpPort: 51801 }
    });
    const w = render();
    await flushPromises();
    expect((w.get('input[type="number"]').element as HTMLInputElement).value).toBe("51801");
    expect(w.get('[data-testid="access-preview"]').text()).toContain("http://localhost:51801");
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
  it("accepts dropped files, shows their names and invalidates validation when removed", async () => {
    const w = render();
    await flushPromises();
    const cert = new File(["certificate"], "server.crt");
    const key = new File(["private key"], "server.key");
    await w.get('[data-testid="certificate-dropzone"]').trigger("drop", { dataTransfer: { files: [cert] } });
    await w.get('[data-testid="privateKey-dropzone"]').trigger("drop", { dataTransfer: { files: [key] } });
    await flushPromises();
    expect(api.certificate).toHaveBeenCalledWith(cert, key);
    expect(w.text()).toContain("server.crt");
    expect(w.text()).toContain("server.key");
    await w.get('[data-testid="remove-certificate"]').trigger("click");
    expect(w.text()).not.toContain("server.crt");
    expect(w.text()).not.toContain("新证书：");
    expect(w.get('[data-testid="apply"]').attributes("disabled")).toBeDefined();
    w.unmount();
  });
  it("rejects multiple, oversized and wrong-format drops before validation", async () => {
    const w = render();
    await flushPromises();
    const zone = w.get('[data-testid="certificate-dropzone"]');
    const cert = new File(["pem"], "server.crt");
    await zone.trigger("drop", { dataTransfer: { files: [cert, cert] } });
    expect(w.text()).toContain("每次只能选择一个文件");
    await zone.trigger("drop", { dataTransfer: { files: [new File(["text"], "wrong.txt")] } });
    expect(w.text()).toContain("请选择 PEM、CRT 或 CER 格式的证书");
    await zone.trigger("drop", { dataTransfer: { files: [new File([new Uint8Array(1024 * 1024 + 1)], "large.crt")] } });
    expect(w.text()).toContain("每个证书或私钥文件不能超过 1 MiB");
    expect(api.certificate).not.toHaveBeenCalled();
    w.unmount();
  });
  it("ignores file drops when the installation is unsupported", async () => {
    api.status.mockResolvedValue({ code: 0, data: { supported: false, reason: "仅支持绿色包" } });
    const w = render();
    await flushPromises();
    await w.get('[data-testid="certificate-dropzone"]').trigger("drop", {
      dataTransfer: { files: [new File(["pem"], "server.crt")] }
    });
    expect(w.text()).not.toContain("server.crt");
    expect(api.certificate).not.toHaveBeenCalled();
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
