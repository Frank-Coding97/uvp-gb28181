import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  getStatus: vi.fn(),
  getPolicy: vi.fn(),
  getMigrationPlan: vi.fn(),
  updatePolicy: vi.fn(),
  check: vi.fn(),
  importPackage: vi.fn(),
  importStatus: vi.fn(),
  createTask: vi.fn(),
  applyTask: vi.fn(),
  cancelTask: vi.fn(),
  rollbackTask: vi.fn()
}));
const modal = vi.hoisted(() => ({ confirm: vi.fn() }));
const permissions = vi.hoisted(() => ({ value: ["system:upgrade:read", "system:upgrade:manage"] as string[] }));
const storage = new Map<string, string>();

vi.mock("@arco-design/web-vue", () => ({ Message: { success: vi.fn(), error: vi.fn() }, Modal: modal }));
vi.mock("@/hooks/usePermission", () => ({
  hasPermission: (permission: string) => permissions.value.includes("*:*:*") || permissions.value.includes(permission)
}));
vi.mock("@/api/service-upgrade", () => ({
  getServiceUpgradeStatus: api.getStatus,
  getServiceUpgradePolicy: api.getPolicy,
  getServiceUpgradeMigrationPlan: api.getMigrationPlan,
  updateServiceUpgradePolicy: api.updatePolicy,
  checkServiceUpgrade: api.check,
  importServiceUpgrade: api.importPackage,
  getServiceUpgradeImport: api.importStatus,
  createServiceUpgradeTask: api.createTask,
  applyServiceUpgradeTask: api.applyTask,
  cancelServiceUpgradeTask: api.cancelTask,
  rollbackServiceUpgradeTask: api.rollbackTask
}));

import ServiceUpgradePage from "./index.vue";
import Upload from "@arco-design/web-vue/es/upload/upload";

const policy = () => ({
  enabled: true,
  policy_version: 1,
  timezone: "Asia/Shanghai",
  window_start: "02:00",
  window_end: "04:00",
  channel: "stable",
  deployment: "systemd",
  os: "linux",
  arch: "amd64",
  patch_only: true,
  allow_restart: false,
  auto_rollback: false,
  notification_target: "本地日志",
  retain_versions: 2
});

const status = () => ({
  current: { version: "1.0.0", build_id: "old" },
  deployment: "systemd",
  releaseSourceConfigured: true,
  capabilities: { onlineCheck: true, offlineImport: true, apply: true, rollback: true, updaterProtocol: 1 },
  tasks: [],
  events: []
});

function mountPage(realUpload = false) {
  return mount(ServiceUpgradePage, {
    global: {
      components: { AUpload: Upload },
      stubs: {
        "a-drawer": { props: ["visible"], template: "<aside v-if='visible'><slot name='title' /><slot /></aside>" },
        "a-card": { template: "<section><slot name='title' /><slot name='extra' /><slot /></section>" },
        "a-button": { props: { disabled: Boolean }, template: "<button :disabled='disabled'><slot /></button>" },
        "a-descriptions": { template: "<div><slot /></div>" },
        "a-descriptions-item": { template: "<div><slot /></div>" },
        "a-space": { template: "<span><slot /></span>" },
        "a-upload": realUpload ? false : { template: "<div><slot name='upload-button' /></div>" },
        "a-switch": {
          props: { modelValue: Boolean, disabled: Boolean },
          emits: ["update:modelValue"],
          template:
            "<input type='checkbox' :checked='modelValue' :disabled='disabled' @change='$emit(\"update:modelValue\", $event.target.checked)' />"
        },
        "a-form": { template: "<form><slot /></form>" },
        "a-form-item": { template: "<label><slot /></label>" },
        "a-alert": { props: ["type"], template: "<div role='alert'><slot /></div>" },
        "a-typography-paragraph": { template: "<p><slot /></p>" },
        "a-table": { template: "<div><slot name='columns' /></div>" },
        "a-table-column": { template: "<div />" },
        "a-link": { template: "<a><slot /></a>" },
        "a-divider": { template: "<hr />" },
        "a-list": { template: "<div><slot name='header' /></div>" },
        "a-list-item": { template: "<div><slot /></div>" },
        "a-input": { props: ["modelValue", "disabled"], template: "<input :value='modelValue' :disabled='disabled' />" },
        "a-input-number": { props: ["modelValue", "disabled"], template: "<input :value='modelValue' :disabled='disabled' />" },
        "a-select": { props: ["modelValue", "disabled"], template: "<select :disabled='disabled'><slot /></select>" },
        "a-option": { props: ["value"], template: "<option :value='value'><slot /></option>" },
        "icon-refresh": true
      }
    }
  });
}

describe("service upgrade page", () => {
  beforeEach(() => {
    storage.clear();
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: {
        getItem: (key: string) => storage.get(key) ?? null,
        setItem: (key: string, value: string) => storage.set(key, String(value)),
        removeItem: (key: string) => storage.delete(key)
      }
    });
    permissions.value = ["system:upgrade:read", "system:upgrade:manage"];
    api.getStatus.mockReset().mockResolvedValue({ code: 0, data: status() });
    api.getPolicy.mockReset().mockResolvedValue({ code: 0, data: policy() });
    api.getMigrationPlan.mockReset().mockResolvedValue({
      code: 0,
      data: { can_apply: true, pending: [], applied: [], unknown: [], destructive: [], dialect: "sqlite" }
    });
    api.updatePolicy.mockReset();
    api.check.mockReset();
    api.importPackage.mockReset();
    api.importStatus.mockReset();
    api.createTask.mockReset();
    api.applyTask.mockReset();
    api.cancelTask.mockReset();
    api.rollbackTask.mockReset();
  });

  afterEach(() => vi.useRealTimers());

  it("点击导入打开文件选择，上传后轮询异步任务并显示失败原因", async () => {
    api.importPackage.mockResolvedValue({ code: 0, data: { upload_id: "upload-1", job_id: "job-1", phase: "queued" } });
    api.importStatus.mockResolvedValue({
      code: 0,
      data: { upload_id: "upload-1", job_id: "job-1", phase: "failed", error: "升级包签名无效" }
    });
    const wrapper = mountPage(true);
    await flushPromises();
    const input = wrapper.get('input[type="file"]');
    const click = vi.spyOn(input.element as HTMLInputElement, "click").mockImplementation(() => {});
    const button = wrapper.findAll("button").find(button => button.text() === "导入离线升级包")!;
    await button.trigger("click");
    expect(click).toHaveBeenCalledOnce();
    const file = new File(["package"], "release.upgrade.tar", { type: "application/x-tar" });
    Object.defineProperty(input.element, "files", { configurable: true, value: [file] });
    await input.trigger("change");
    await flushPromises();
    expect(api.importPackage).toHaveBeenCalledWith(file, expect.any(Function));
    expect(api.importStatus).toHaveBeenCalledWith("job-1");
    const reason = wrapper.get('[data-testid="import-progress"] details');
    (reason.element as HTMLDetailsElement).open = true;
    await reason.trigger("toggle");
    expect(wrapper.text()).toContain("升级包签名无效");
    await input.trigger("change");
    await flushPromises();
    expect(api.importPackage).toHaveBeenCalledTimes(2);
    expect((input.element as HTMLInputElement).value).toBe("");
    wrapper.unmount();
  });

  it("刷新页面后恢复未完成的导入任务并展示已校验版本", async () => {
    localStorage.setItem("uvp.service-upgrade.import-job", "job-restore");
    api.importStatus.mockResolvedValue({
      code: 0,
      data: {
        upload_id: "upload-restore",
        job_id: "job-restore",
        phase: "succeeded",
        filename: "release.tar",
        size: 12,
        sha256: "a".repeat(64),
        release_id: "release-new",
        version: "1.2.0",
        build_id: "build-new",
        deployment: "systemd",
        os: "linux",
        arch: "amd64",
        release_notes: "异步导入完成"
      }
    });
    const wrapper = mountPage();
    await flushPromises();
    expect(api.importStatus).toHaveBeenCalledWith("job-restore");
    expect((wrapper.vm as any).update.manifest.version).toBe("1.2.0");
    expect(wrapper.text()).toContain("异步导入完成");
    expect(window.localStorage.getItem("uvp.service-upgrade.import-job")).toBe("job-restore");
    wrapper.unmount();
  });

  it("校验处理中持续轮询，完成后才启用升级版本信息", async () => {
    vi.useFakeTimers();
    api.importPackage.mockResolvedValue({
      code: 0,
      data: { upload_id: "upload-progress", job_id: "job-progress", phase: "queued" }
    });
    api.importStatus
      .mockResolvedValueOnce({ code: 0, data: { upload_id: "upload-progress", job_id: "job-progress", phase: "verifying" } })
      .mockResolvedValueOnce({
        code: 0,
        data: {
          upload_id: "upload-progress",
          job_id: "job-progress",
          phase: "succeeded",
          filename: "release.tar",
          size: 12,
          sha256: "a".repeat(64),
          release_id: "release-progress",
          version: "1.2.0",
          build_id: "build-progress",
          deployment: "systemd",
          os: "linux",
          arch: "amd64"
        }
      });
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    const importPromise = vm.importOffline(new File(["package"], "release.tar"));
    await flushPromises();
    expect(vm.importing).toBe(true);
    expect(vm.update).toBeUndefined();
    await vi.advanceTimersByTimeAsync(2500);
    await flushPromises();
    await importPromise;
    expect(api.importStatus).toHaveBeenCalledTimes(2);
    expect(vm.importing).toBe(false);
    expect(vm.update.manifest.version).toBe("1.2.0");
    wrapper.unmount();
  });

  it("离线导入不可用时不能打开文件选择或导入文件", async () => {
    api.getStatus.mockResolvedValue({ code: 0, data: { ...status(), capabilities: { offlineImport: false } } });
    const wrapper = mountPage(true);
    await flushPromises();
    const input = wrapper.get('input[type="file"]');
    const click = vi.spyOn(input.element as HTMLInputElement, "click").mockImplementation(() => {});
    const button = wrapper.findAll("button").find(button => button.text() === "导入离线升级包")!;
    expect(button.attributes("disabled")).toBeDefined();
    await button.trigger("click");
    expect(click).not.toHaveBeenCalled();
    expect(api.importPackage).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("上传展示实际字节，100% 后等待受理且不提前允许升级", async () => {
    let finish!: (value: unknown) => void;
    api.importPackage.mockReturnValue(new Promise(resolve => (finish = resolve)));
    api.importStatus.mockResolvedValue({ data: { upload_id: "new-import", phase: "verifying" } });
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    vm.update = { manifest: { version: "1.9.0" }, artifact: { sha256: "old" }, hasUpdate: true };
    const pending = vm.importOffline(new File(["archive"], "upgrade.tar"));
    await flushPromises();
    expect(vm.update).toBeUndefined();
    const progress = wrapper.get('[data-testid="import-progress"]');
    expect(progress.text()).toContain("upgrade.tar");
    const onProgress = api.importPackage.mock.calls.at(-1)![1];
    onProgress({ loaded: 4 * 1024 * 1024, total: 10 * 1024 * 1024, lengthComputable: true });
    await flushPromises();
    expect(progress.text()).toContain("4 MiB / 10 MiB");
    expect(progress.get('[role="progressbar"]').attributes("aria-valuenow")).toBe("40");
    onProgress({ loaded: 10, total: 10, lengthComputable: true });
    await flushPromises();
    expect(progress.text()).toContain("等待服务端受理");
    expect(api.createTask).not.toHaveBeenCalled();
    expect(api.applyTask).not.toHaveBeenCalled();
    finish({ data: { upload_id: "new-import", phase: "queued" } });
    await pending;
    await flushPromises();
    expect(progress.text()).toContain("正在校验签名与完整性");
    expect(progress.find('[role="progressbar"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("缺少可靠总大小不编造百分比，上传失败后重新选包清除旧状态", async () => {
    let reject!: (error: Error) => void;
    api.importPackage.mockReturnValueOnce(new Promise((_resolve, fail) => (reject = fail)));
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    const pending = vm.importOffline(new File(["a"], "first.tar"));
    const onProgress = api.importPackage.mock.calls.at(-1)![1];
    expect(onProgress).toBeTypeOf("function");
    onProgress({ loaded: 2048, lengthComputable: false });
    await flushPromises();
    const progress = wrapper.get('[data-testid="import-progress"]');
    expect(progress.text()).toContain("已传 2 KiB");
    expect(progress.get('[role="progressbar"]').attributes("aria-valuenow")).toBeUndefined();
    reject(new Error("上传连接中断"));
    await pending;
    const reason = progress.get("details");
    (reason.element as HTMLDetailsElement).open = true;
    await reason.trigger("toggle");
    expect(progress.text()).toContain("上传连接中断");
    api.importPackage.mockReturnValueOnce(new Promise(vi.fn()));
    void vm.importOffline(new File(["b"], "second.tar"));
    await flushPromises();
    expect(progress.text()).toContain("second.tar");
    expect(progress.text()).not.toContain("first.tar");
    expect(progress.text()).not.toContain("上传连接中断");
    wrapper.unmount();
  });

  it("重新上传时忽略旧导入查询的迟到结果", async () => {
    storage.set("uvp.service-upgrade.import-job", "old-job");
    let finishOld!: (value: unknown) => void;
    api.importStatus.mockReturnValueOnce(new Promise(resolve => (finishOld = resolve)));
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    // A completed old job can still have a manual status refresh in flight.
    vm.importing = false;
    api.importPackage.mockReturnValueOnce(new Promise(vi.fn()));
    void vm.importOffline(new File(["new"], "new.tar"));
    finishOld({ data: { upload_id: "old-job", phase: "failed", error: "旧校验失败" } });
    await flushPromises();
    expect(wrapper.get('[data-testid="import-progress"]').text()).toContain("new.tar");
    expect(wrapper.text()).not.toContain("旧校验失败");
    expect(vm.importing).toBe(true);
    expect(storage.has("uvp.service-upgrade.import-job")).toBe(false);
    wrapper.unmount();
  });

  it("导入查询断线保留阶段，刷新恢复后不虚构上传进度", async () => {
    storage.set("uvp.service-upgrade.import-job", "restore-job");
    api.importStatus.mockRejectedValueOnce(new Error("网络中断"));
    const wrapper = mountPage();
    await flushPromises();
    const progress = wrapper.get('[data-testid="import-progress"]');
    expect(progress.text()).toContain("正在重连");
    expect(progress.find('[role="progressbar"]').exists()).toBe(false);
    api.importStatus.mockResolvedValueOnce({ data: { upload_id: "restore-job", phase: "verifying" } });
    await (wrapper.vm as any).refreshImportJob();
    expect(progress.text()).toContain("正在校验签名与完整性");
    expect(progress.text()).not.toContain("正在重连");
    expect(api.applyTask).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it.each([
    ["fetching", "下载中", 0],
    ["verifying", "校验中", 0],
    ["preparing", "准备与预检", 0],
    ["awaiting_confirmation", "等待应用授权", 0],
    ["stopping", "停止服务", 0],
    ["backing_up", "备份数据", 1],
    ["migrating", "执行迁移", 2],
    ["activating", "切换版本", 3],
    ["validating", "验证启动", 3],
    ["committing", "提交升级结果", 3],
    ["succeeded", "升级成功", 4],
    ["rolled_back", "已恢复旧版本", 3],
    ["recovery_required", "需要恢复处理", 3],
    ["failed", "任务失败或已取消", 3],
    ["future-phase", "未知阶段（future-phase）", -1]
  ])("升级阶段 %s 如实显示且不会自动授权", async (phase, label, index) => {
    const task = { task_id: "current", phase, version: "1.2.0", failure_phase: "validating", committed: phase === "succeeded" };
    api.getStatus.mockResolvedValue({ data: { ...status(), activeTask: task, tasks: [task] } });
    const wrapper = mountPage();
    await flushPromises();
    const progress = wrapper.get('[data-testid="task-progress"]');
    expect(progress.text()).toContain(label);
    const current = progress.find('[aria-current="step"]');
    if (index < 0) expect(current.exists()).toBe(false);
    else expect(current.attributes("data-step")).toBe(String(index));
    expect(progress.find('[role="progressbar"]').exists()).toBe(false);
    if (phase !== "succeeded") expect(progress.text()).not.toContain("升级成功");
    expect(api.applyTask).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("活动任务断线后保留进度，同任务恢复后显示终态，详情独立显示历史任务", async () => {
    const old = { task_id: "old", version: "1.0.0", phase: "succeeded", committed: true };
    const current = { task_id: "current", version: "1.2.0", phase: "backing_up", committed: false };
    api.getStatus.mockResolvedValueOnce({ data: { ...status(), activeTask: current, tasks: [old, current] } });
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    expect(wrapper.get('[data-testid="task-progress"]').text()).toContain("备份数据");
    api.getStatus.mockRejectedValueOnce(new Error("服务重启中"));
    await vm.loadStatus();
    expect(wrapper.get('[data-testid="task-progress"]').text()).toContain("正在重连");
    vm.showTaskDetails("old");
    await flushPromises();
    expect(wrapper.get('[data-testid="task-detail-progress"]').text()).toContain("升级成功");
    expect(wrapper.get('[data-testid="task-progress"]').text()).not.toContain("升级成功");
    api.getStatus.mockResolvedValueOnce({
      data: { ...status(), tasks: [old, { ...current, phase: "succeeded", committed: true, updated_at: "2026-10-19T01:00:00Z" }] }
    });
    await vm.loadStatus();
    expect(wrapper.get('[data-testid="task-progress"]').text()).toContain("升级成功");
    expect(wrapper.get('[data-testid="task-progress"]').text()).not.toContain("正在重连");
    wrapper.unmount();
  });

  it("空闲页面不增加进度占位", async () => {
    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.find('[data-testid="import-progress"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="task-progress"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("旧包重新查询失败后清除可应用的旧制品，并展示真实原因", async () => {
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    vm.update = { manifest: { version: "1.2.0" }, artifact: { sha256: "old" }, hasUpdate: true };
    vm.importJob = { upload_id: "job", phase: "verifying" };
    api.importStatus.mockResolvedValueOnce({ data: { upload_id: "job", phase: "succeeded", version: "1.2.0" } });
    await vm.refreshImportJob();
    expect(vm.update).toBeUndefined();
    expect(wrapper.get('[data-testid="import-progress"]').text()).not.toContain("升级包已就绪 · v1.2.0");
    wrapper.unmount();
  });

  it("默认展示升级记录，自动升级配置按需进入", async () => {
    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.find('[data-testid="upgrade-history"]').isVisible()).toBe(true);
    expect(wrapper.find('[data-testid="upgrade-settings"]').attributes("style")).toContain("display: none");
    await wrapper.get('[data-testid="settings-tab"]').trigger("click");
    expect(wrapper.find('[data-testid="upgrade-settings"]').isVisible()).toBe(true);
    wrapper.unmount();
  });

  it("不显示页面标题说明，刷新状态与升级包操作保持同一行", async () => {
    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.find(".upgrade-heading").exists()).toBe(false);
    expect(wrapper.find(".version-actions").text()).toContain("刷新状态");
    wrapper.unmount();
  });

  it("离线包说明可在应用前查看，缺失说明不编造", async () => {
    api.importPackage.mockResolvedValue({ code: 0, data: { upload_id: "upload-notes", job_id: "job-notes", phase: "queued" } });
    api.importStatus.mockResolvedValue({
      code: 0,
      data: {
        filename: "release.tar",
        size: 12,
        sha256: "a".repeat(64),
        release_id: "release-new",
        version: "1.2.0",
        build_id: "new",
        deployment: "standalone",
        os: "linux",
        arch: "amd64",
        release_notes: "新增：手动升级\n修复：启动异常",
        published_at: "2026-10-18T07:00:00Z"
      }
    });
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    await vm.importOffline(new File(["package"], "release.tar"));
    expect(vm.update.manifest.release_notes).toContain("新增：手动升级");
    expect(wrapper.text()).toContain("新增：手动升级");
    vm.showReleaseNotes("1.2.0", "");
    await flushPromises();
    expect(wrapper.text()).toContain("该版本未提供更新说明");
    wrapper.unmount();
  });

  it("任务详情沿用系统抽屉和详情表格样式", async () => {
    const task = { task_id: "detail-style", version: "1.1.6", phase: "backing_up", committed: false };
    api.getStatus.mockResolvedValue({ data: { ...status(), tasks: [task] } });
    const wrapper = mountPage();
    await flushPromises();
    (wrapper.vm as any).showTaskDetails(task.task_id);
    await flushPromises();
    expect(wrapper.get("aside").classes()).toContain("uvp-system-drawer");
    const details = wrapper.get(".task-descriptions");
    expect(details.classes()).toContain("uvp-system-description");
    expect(details.attributes()).toHaveProperty("bordered");
    wrapper.unmount();
  });

  it("历史日志默认不展开，详情可查看且说明按纯文本显示", async () => {
    const task = {
      task_id: "task-history",
      actor: "admin",
      target_digest: "a",
      version: "1.0.0",
      build_id: "old",
      phase: "succeeded",
      committed: true,
      error: "LONG_DIAGNOSTIC",
      release_notes: "<img src=x onerror=alert(1)>"
    };
    api.getStatus.mockResolvedValue({ code: 0, data: { ...status(), tasks: [task] } });
    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.text()).not.toContain("LONG_DIAGNOSTIC");
    const vm = wrapper.vm as any;
    vm.showTaskDetails(task.task_id);
    await flushPromises();
    expect(wrapper.text()).toContain("LONG_DIAGNOSTIC");
    vm.showReleaseNotes(task.version, task.release_notes);
    await flushPromises();
    expect(wrapper.text()).toContain("<img src=x onerror=alert(1)>");
    expect(wrapper.find("img").exists()).toBe(false);
    wrapper.unmount();
  });

  it("保存策略失败时保留用户当前策略值", async () => {
    api.updatePolicy.mockRejectedValueOnce(new Error("发布站点不可用"));
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    vm.policy.enabled = true;
    await vm.savePolicy();
    expect(vm.policy.enabled).toBe(true);
    expect(vm.errorMessage).toContain("发布站点不可用");
    wrapper.unmount();
  });

  it("只读账号隐藏管理操作并禁用策略编辑", async () => {
    permissions.value = ["system:upgrade:read"];
    const wrapper = mountPage();
    await flushPromises();
    expect(wrapper.text()).not.toContain("导入离线包");
    expect(wrapper.text()).not.toContain("保存策略");
    expect((wrapper.vm as any).canManage).toBe(false);
    expect(wrapper.findAll("input:disabled").length).toBeGreaterThan(0);
    wrapper.unmount();
  });

  it("未配置可信在线发布源时禁用在线检查", async () => {
    api.getStatus.mockResolvedValueOnce({
      code: 0,
      data: { ...status(), releaseSourceConfigured: false, capabilities: { ...status().capabilities, onlineCheck: false } }
    });
    const wrapper = mountPage();
    await flushPromises();
    const checkButton = wrapper.findAll("button").find(button => button.text().includes("检查在线更新"));
    expect(checkButton?.attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("按固定周期刷新任务状态", async () => {
    vi.useFakeTimers();
    const wrapper = mountPage();
    await flushPromises();
    expect(api.getStatus).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(5000);
    expect(api.getStatus).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("轮询刷新时保留未保存的策略编辑", async () => {
    vi.useFakeTimers();
    const wrapper = mountPage();
    await flushPromises();

    const vm = wrapper.vm as any;
    vm.policy.channel = "beta";
    vm.policy.window_start = "03:00";
    api.getPolicy.mockResolvedValueOnce({
      code: 0,
      data: { ...policy(), channel: "stable", window_start: "04:00" }
    });

    await vi.advanceTimersByTimeAsync(5000);
    await flushPromises();

    expect(vm.policy.channel).toBe("beta");
    expect(vm.policy.window_start).toBe("03:00");
    expect(vm.policyDirty).toBe(true);
    expect(wrapper.text()).toContain("有未保存修改");
    wrapper.unmount();
  });

  it("默认关闭自动回退并准确说明补丁和小版本范围", async () => {
    const wrapper = mountPage();
    await flushPromises();

    const vm = wrapper.vm as any;
    expect(vm.policy.auto_rollback).toBe(false);
    expect(wrapper.text()).toContain("补丁/小版本");
    expect(wrapper.text()).not.toContain("仅补丁");
    wrapper.unmount();
  });

  it("通知设置只展示首版页面和本地日志事件能力", async () => {
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("页面和本地日志");
    expect(wrapper.text()).toContain("外部通知渠道暂未接入");
    expect(wrapper.find('input[placeholder="本地日志/页面"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("保存策略时清除未提供界面的外部通知目标", async () => {
    api.updatePolicy.mockResolvedValueOnce({ code: 0, data: policy() });
    const wrapper = mountPage();
    await flushPromises();

    await (wrapper.vm as any).savePolicy();

    expect(api.updatePolicy).toHaveBeenCalledWith(expect.objectContaining({ notification_target: "" }));
    wrapper.unmount();
  });

  it("导入同版本或降级离线包时不显示普通升级入口", async () => {
    api.importPackage.mockResolvedValueOnce({
      code: 0,
      data: {
        path: "/tmp/release.tar",
        filename: "release.tar",
        size: 12,
        sha256: "a".repeat(64),
        release_id: "release-same",
        version: "1.0.0",
        build_id: "same-build",
        deployment: "systemd",
        os: "linux",
        arch: "amd64"
      }
    });
    const wrapper = mountPage();
    await flushPromises();

    const vm = wrapper.vm as any;
    await vm.importOffline(new File(["release"], "release.tar"));
    expect(vm.update.hasUpdate).toBe(false);

    api.importPackage.mockResolvedValueOnce({
      code: 0,
      data: {
        path: "/tmp/release.tar",
        filename: "release.tar",
        size: 12,
        sha256: "b".repeat(64),
        release_id: "release-old",
        version: "0.9.9",
        build_id: "old-build",
        deployment: "systemd",
        os: "linux",
        arch: "amd64"
      }
    });
    await vm.importOffline(new File(["release"], "release.tar"));
    expect(vm.update.hasUpdate).toBe(false);
    expect(wrapper.text()).toContain("当前已是该版本，不能重复创建普通升级任务");
    wrapper.unmount();
  });

  it("导入 RC 离线包时按语义版本判断是否高于当前版本", async () => {
    api.importPackage.mockResolvedValueOnce({
      code: 0,
      data: {
        path: "/tmp/release.tar",
        filename: "release.tar",
        size: 12,
        sha256: "c".repeat(64),
        release_id: "release-rc",
        version: "1.1.0-rc.1",
        build_id: "rc-build",
        deployment: "systemd",
        os: "linux",
        arch: "amd64"
      }
    });
    const wrapper = mountPage();
    await flushPromises();
    await (wrapper.vm as any).importOffline(new File(["release"], "release.tar"));
    expect((wrapper.vm as any).update.hasUpdate).toBe(true);
    wrapper.unmount();
  });

  it("应用前要求管理员确认停机和数据恢复限制", async () => {
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;

    vm.update = {
      manifest: { version: "1.2.0", build_id: "build-b" },
      artifact: { sha256: "a".repeat(64) },
      hasUpdate: true
    };
    vm.status.tasks = [{ task_id: "task-1", version: "1.2.0", build_id: "build-b", phase: "awaiting_confirmation" }];
    vm.confirmApply("task-1");

    const confirmation = modal.confirm.mock.calls.at(-1)?.[0];
    expect(confirmation.title).toContain("确认");
    expect(confirmation.content).toContain("短暂中断");
    expect(confirmation.content).toContain("不能撤销");
    expect(api.applyTask).not.toHaveBeenCalled();

    await confirmation.onOk();
    await flushPromises();
    expect(api.applyTask).toHaveBeenCalledWith("task-1");
    wrapper.unmount();
  });

  it("回退前明确说明迁移不可由程序回切自动撤销", async () => {
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    vm.status.tasks = [{ task_id: "task-1", phase: "recovery_required", failure_phase: "validating", committed: false }];

    vm.confirmRollback("task-1");

    const confirmation = modal.confirm.mock.calls.at(-1)?.[0];
    expect(confirmation.title).toContain("回退");
    expect(confirmation.content).toContain("不能自动撤销数据库迁移");
    expect(api.rollbackTask).not.toHaveBeenCalled();

    await confirmation.onOk();
    await flushPromises();
    expect(api.rollbackTask).toHaveBeenCalledWith("task-1");
    wrapper.unmount();
  });

  it("刷新后按所选持久任务确认目标，不使用后来检查到的更新", async () => {
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    vm.status.tasks = [{ task_id: "task-a", version: "1.2.0", build_id: "build-a", phase: "awaiting_confirmation" }];
    vm.update = { manifest: { version: "2.0.0", build_id: "build-b" }, artifact: { filename: "new.tar" } };
    vm.confirmApply("task-a");
    const confirmation = modal.confirm.mock.calls.at(-1)?.[0];
    expect(confirmation.content).toContain("1.2.0");
    expect(confirmation.content).toContain("build-a");
    expect(confirmation.content).not.toContain("2.0.0");
    wrapper.unmount();
  });

  it("迁移查询失败仍刷新任务，并单独说明迁移信息不可用", async () => {
    api.getMigrationPlan.mockRejectedValueOnce(new Error("迁移计划暂不可用"));
    api.getStatus.mockResolvedValueOnce({ code: 0, data: { ...status(), current: { version: "1.2.0", build_id: "current-b" } } });
    const wrapper = mountPage();
    await flushPromises();
    expect((wrapper.vm as any).status?.current.version).toBe("1.2.0");
    expect(wrapper.text()).toContain("迁移计划暂不可用");
    wrapper.unmount();
  });

  it("慢请求期间合并轮询，避免旧状态覆盖较新状态", async () => {
    vi.useFakeTimers();
    let resolveStatus!: (value: unknown) => void;
    api.getStatus.mockReturnValueOnce(
      new Promise(resolve => {
        resolveStatus = resolve;
      })
    );
    const wrapper = mountPage();
    await flushPromises();
    await (wrapper.vm as any).loadStatus();
    expect(api.getStatus).toHaveBeenCalledTimes(1);
    resolveStatus({ code: 0, data: status() });
    await flushPromises();
    wrapper.unmount();
  });

  it("权限或助手能力不足时不能发起应用请求", async () => {
    permissions.value = ["system:upgrade:read"];
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    vm.status.tasks = [{ task_id: "task-a", phase: "awaiting_confirmation" }];
    await vm.apply("task-a");
    expect(api.applyTask).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("重复创建点击在首次请求完成前只提交一次", async () => {
    let resolveCreate!: (value: unknown) => void;
    api.createTask.mockReturnValueOnce(
      new Promise(resolve => {
        resolveCreate = resolve;
      })
    );
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    vm.update = { manifest: { version: "1.2.0" }, artifact: { sha256: "a".repeat(64) }, hasUpdate: true };
    const first = vm.submitTask();
    const second = vm.submitTask();
    expect(api.createTask).toHaveBeenCalledTimes(1);
    resolveCreate({ code: 0, data: {} });
    await Promise.all([first, second]);
    wrapper.unmount();
  });

  it("保留版本输入超限时显示原值和错误，不静默改成边界或提交", async () => {
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    vm.policy.retain_versions = "51";
    await vm.savePolicy();
    expect(vm.policy.retain_versions).toBe("51");
    expect(wrapper.text()).toContain("保留版本数必须是 0–50 的整数");
    expect(api.updatePolicy).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("策略输入在提交时验证时区与维护窗口", async () => {
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    vm.policy.timezone = "Mars/Base";
    vm.policy.window_start = "25:00";
    await vm.savePolicy();
    expect(wrapper.text()).toContain("有效的 IANA 时区");
    expect(wrapper.text()).toContain("HH:MM");
    expect(api.updatePolicy).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("操作错误不会被无关的状态轮询清除", async () => {
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    vm.errorMessage = "升级助手拒绝应用：配置已变化";
    await vm.loadStatus();
    expect(wrapper.text()).toContain("配置已变化");
    wrapper.unmount();
  });

  it("失败与回退展示真实阶段、原因和不同结果提示", async () => {
    const wrapper = mountPage();
    await flushPromises();
    const vm = wrapper.vm as any;
    expect(vm.phaseLabel("validating")).toBe("验证启动");
    expect(vm.taskHint({ phase: "rolled_back" })).toContain("升级未成功");
    expect(vm.taskHint({ phase: "recovery_required" })).toContain("不可视为已回退");
    expect(vm.taskHint({ phase: "awaiting_confirmation", policy_version: 3 })).toContain("维护窗口");
    wrapper.unmount();
  });
});
