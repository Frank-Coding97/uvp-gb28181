import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  createStandaloneAdmin: vi.fn(),
  forgetBootstrapToken: vi.fn(),
  isBcryptPasswordLengthValid: vi.fn((value: string) => new TextEncoder().encode(value).byteLength <= 72),
  loadStandaloneSetupStatus: vi.fn(),
  readBootstrapTokenOnce: vi.fn(),
  routerReplace: vi.fn(),
  messages: {
    success: vi.fn(),
    info: vi.fn()
  },
  route: { query: {} as Record<string, unknown> }
}));

vi.mock("@/api/standalone-setup", () => ({
  createStandaloneAdmin: mocks.createStandaloneAdmin,
  forgetBootstrapToken: mocks.forgetBootstrapToken,
  isBcryptPasswordLengthValid: mocks.isBcryptPasswordLengthValid,
  loadStandaloneSetupStatus: mocks.loadStandaloneSetupStatus,
  readBootstrapTokenOnce: mocks.readBootstrapTokenOnce
}));
vi.mock("vue-router", () => ({
  useRoute: () => mocks.route,
  useRouter: () => ({ replace: mocks.routerReplace })
}));
vi.mock("@arco-design/web-vue", () => ({ Message: mocks.messages }));

import StandaloneSetup from "./index.vue";

describe("standalone setup page", () => {
  beforeEach(() => {
    mocks.createStandaloneAdmin.mockReset();
    mocks.forgetBootstrapToken.mockReset();
    mocks.loadStandaloneSetupStatus.mockReset();
    mocks.routerReplace.mockReset().mockResolvedValue(undefined);
    mocks.messages.success.mockReset();
    mocks.messages.info.mockReset();
    mocks.route.query = { bootstrap_token: "secret-token" };
    mocks.readBootstrapTokenOnce.mockReset().mockReturnValue("secret-token");
    mocks.loadStandaloneSetupStatus.mockResolvedValue({
      kind: "standalone",
      status: { phase: "pending_admin", standalone: true }
    });
  });

  async function mountReady() {
    const wrapper = mount(StandaloneSetup);
    await flushPromises();
    return wrapper;
  }

  async function fillForm(wrapper: ReturnType<typeof mount>) {
    await wrapper.get('input[name="username"]').setValue("admin");
    await wrapper.get('input[name="password"]').setValue("safe-password");
    await wrapper.get('input[name="passwordConfirmation"]').setValue("safe-password");
  }

  it("submits the form with the in-memory token and sends the user to login after creation", async () => {
    mocks.createStandaloneAdmin.mockResolvedValue({ outcome: "created", phase: "pending_sip" });
    const wrapper = await mountReady();
    await fillForm(wrapper);
    await wrapper.get("form").trigger("submit");
    await flushPromises();

    expect(mocks.createStandaloneAdmin).toHaveBeenCalledWith({ username: "admin", password: "safe-password" }, "secret-token");
    expect(mocks.forgetBootstrapToken).toHaveBeenCalledTimes(1);
    expect(mocks.messages.success).toHaveBeenCalledWith("管理员已创建，请登录并配置 SIP");
    expect(mocks.routerReplace).toHaveBeenCalledWith("/login");
  });

  it("does not reveal credential details for unauthorized and repeat submissions", async () => {
    mocks.createStandaloneAdmin.mockResolvedValue({ outcome: "unauthorized", status: 403 });
    const unauthorized = await mountReady();
    await fillForm(unauthorized);
    await unauthorized.get("form").trigger("submit");
    await flushPromises();
    expect(unauthorized.text()).toContain("请检查初始化凭据后重试");
    expect(unauthorized.text()).not.toContain("credential detail");
    expect(mocks.routerReplace).not.toHaveBeenCalled();

    mocks.createStandaloneAdmin.mockResolvedValue({ outcome: "conflict", status: 409 });
    const repeat = await mountReady();
    await fillForm(repeat);
    await repeat.get("form").trigger("submit");
    await flushPromises();
    expect(mocks.messages.info).toHaveBeenCalledWith("管理员已创建，请直接登录");
    expect(mocks.routerReplace).toHaveBeenCalledWith("/login");
  });

  it("blocks empty, mismatched, and bcrypt-overlong passwords before the API", async () => {
    const wrapper = await mountReady();
    await wrapper.get("form").trigger("submit");
    expect(mocks.createStandaloneAdmin).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("请输入管理员用户名");

    await wrapper.get('input[name="username"]').setValue("admin");
    await wrapper.get('input[name="password"]').setValue("safe-password");
    await wrapper.get('input[name="passwordConfirmation"]').setValue("different-password");
    await wrapper.get("form").trigger("submit");
    expect(mocks.createStandaloneAdmin).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("两次输入的密码不一致");

    await wrapper.get('input[name="password"]').setValue("a".repeat(73));
    await wrapper.get('input[name="passwordConfirmation"]').setValue("a".repeat(73));
    await wrapper.get("form").trigger("submit");
    expect(mocks.createStandaloneAdmin).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("不能超过 72 字节");
  });

  it("shows the launcher prompt and offers no way to obtain a token when absent", async () => {
    mocks.route.query = {};
    mocks.readBootstrapTokenOnce.mockReturnValue(null);
    const wrapper = await mountReady();

    expect(wrapper.text()).toContain("请从本机启动器重新打开首装页面");
    expect(wrapper.text()).not.toContain("获取初始化凭据");
    expect(wrapper.get('button[type="submit"]').attributes("disabled")).toBeDefined();
  });
});
