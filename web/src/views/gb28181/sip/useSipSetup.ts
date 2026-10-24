import { reactive, ref } from "vue";
import {
  fetchSipNetworkInterfaces,
  fetchSipSetupStatus,
  saveSipSetupConfig,
  skipSipSetup,
  type SaveSipConfigPayload,
  type SipDeploymentMode,
  type SipNetworkInterfaces,
  type SipRuntimeStatus,
  type SipSetupStatus
} from "@/api/gb28181";
import type { BaseResult } from "@/api/types";

// 引导页默认 SIP 端口。
//
// ⛔⛔ 为什么默认值必须允许"按包覆盖"，而不是写死一个数字：
//   绿色安装包把**所有**端口压进一整段连续端口，但 **SIP 端口不经过 config.env** ——
//   它是管理员首次登录引导页时录入、存进 gb_sip_config 表的
//   （2026-07-20 起 SIP 明文配置从 YAML 移到了 DB）。
//   ⇒ 若这里写死 5061，绿色包首次打开向导就预填一个**段外**端口，
//     与"整段连续、防火墙一条规则"的规划自相矛盾。
//   ⇒ 出包时由 build-standalone.sh / build-standalone.ps1 注入 VITE_DEFAULT_SIP_PORT；
//     其它构建形态不带该变量 ⇒ 回落 5061，行为完全不变（互不影响）。
//   ⛔ 段位**按平台不同**，别把其中一套当通用值写进文档或断言：
//     · Linux 绿色包：51000-51064（SIP = 51064，见 deploy/standalone/PORTS.md）
//     · Windows 绿色包：15500-15564（SIP = 15564）—— 两条原因：① Windows 的
//       "动态端口范围"会把落进去的端口整段保留、bind 直接拒绝访问（ZLM 的 UDP 一绑
//       就失败、SIP 永远注册不上），所以避开 50000 那段；② 起步段原选 15000，被
//       深信服终端软件占用（SangforPromoteService 监 127.0.0.1:15000），再上移 500。
//       见 deploy/standalone-windows/README.md 第三节。
//
// ⭐ 三个来源，优先级从高到低（判据实现见 validPort / packedSipPort / applyStatus）：
//   ① 后端 status 下发的 defaultSipPort —— **部署层**给的值（容器版 install.sh 按段基址
//      算出来 → .env → compose → 后端）。段可以平移，所以它才是"当前这套部署的正确答案"。
//   ② 构建期注入的 VITE_DEFAULT_SIP_PORT —— 打镜像/出包那一刻的常量，**不随段平移**。
//      绿色包的段是固定的，所以它够用；容器版换了段就得靠 ①。
//   ③ 5061 —— 前两者都没有时的兜底（普通源码构建）。
const FALLBACK_SIP_PORT = 5061;

// 端口合法性判据只写一处：两个来源（后端下发 / 构建期注入）必须用同一把尺子，
// 否则会出现"后端值被判非法、构建期值被判合法"这种单边漂移。
function validPort(raw: unknown): number | null {
  if (raw === undefined || raw === null || raw === "") return null;
  const parsed = Number(raw);
  return Number.isInteger(parsed) && parsed >= 1 && parsed <= 65535 ? parsed : null;
}

// 构建期兜底：非法值（空 / 非数字 / 越界）一律回落 —— 预填一个必然保存失败的端口
// 会让现场以为是"平台不允许这个端口"，而真正的原因是打包注入写错了。
function packedSipPort(): number {
  const packed = validPort(import.meta.env.VITE_DEFAULT_SIP_PORT);
  return packed === null ? FALLBACK_SIP_PORT : packed;
}

export interface SipSetupForm {
  deploymentMode: SipDeploymentMode | "";
  listenIp: string;
  advertiseIp: string;
  advertiseIpInferred: boolean;
  hookIp: string;
  sdpIp: string;
  streamIp: string;
  port: number;
  domain: string;
  serverId: string;
  password: string;
}

type SaveResult = BaseResult<{
  config: NonNullable<SipSetupStatus["config"]>;
  reloadedOk: boolean;
  reloadError: string;
  mediaDefaultsApplied?: boolean;
  mediaDefaultsError?: string;
  runtime: SipRuntimeStatus;
}>;

export interface SipSetupApi {
  status: () => Promise<BaseResult<SipSetupStatus>>;
  interfaces: () => Promise<BaseResult<SipNetworkInterfaces>>;
  save: (payload: SaveSipConfigPayload) => Promise<SaveResult>;
  skip: () => Promise<BaseResult<{ acknowledged: boolean }>>;
}

const defaultApi: SipSetupApi = {
  status: fetchSipSetupStatus,
  interfaces: fetchSipNetworkInterfaces,
  save: saveSipSetupConfig,
  skip: skipSipSetup
};

function initialForm(): SipSetupForm {
  // listenIp 初始留空,NetworkStep 挂载后会根据"推荐网卡"自动填充,
  // 避免出现"0.0.0.0(不可用)"这种误导选项.
  return {
    deploymentMode: "",
    listenIp: "",
    advertiseIp: "",
    advertiseIpInferred: false,
    hookIp: "",
    sdpIp: "",
    streamIp: "",
    port: packedSipPort(),
    domain: "",
    serverId: "",
    password: ""
  };
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : "请求失败";
}

export function useSipSetup(api: SipSetupApi = defaultApi) {
  const loading = ref(false);
  const saving = ref(false);
  const error = ref("");
  const status = ref<SipSetupStatus | null>(null);
  const network = ref<SipNetworkInterfaces | null>(null);
  const hasExistingPassword = ref(false);
  const form = reactive<SipSetupForm>(initialForm());

  function applyStatus(next: SipSetupStatus) {
    status.value = next;
    if (!next.config) {
      // 还没有配置 → 默认端口优先取**部署层**下发的段内值。
      //
      // ⛔ 不能只信构建期常量：安装脚本允许整段平移端口（`--https-port 53000`
      //    ⇒ 规划段 53000-53064），而 VITE_DEFAULT_SIP_PORT 是**打镜像那一刻**烤进去的
      //    51064 —— 平移之后它落在段外，防火墙只放行了段内一段（一条规则），
      //    设备注册必然不通，现场看到的只是"注册不上"，查不到端口段这层原因。
      //    踩过：220 上用 53000 段部署，引导页默认值仍是 51064。
      //
      // 部署层没告知（非容器/绿色包）时字段缺席 ⇒ 保留构建期默认值，行为不变。
      const deployed = validPort(next.defaultSipPort);
      if (deployed !== null) form.port = deployed;
      return;
    }
    Object.assign(form, {
      deploymentMode: next.config.deploymentMode,
      listenIp: next.config.listenIp,
      advertiseIp: next.config.deploymentMode === "lan" && next.config.listenIp === "0.0.0.0" ? "" : next.config.advertiseIp,
      advertiseIpInferred:
        next.config.deploymentMode === "lan" && next.config.listenIp === "0.0.0.0" ? false : next.config.advertiseIpInferred,
      hookIp: next.config.hookIp ?? "",
      sdpIp: next.config.sdpIp ?? "",
      streamIp: next.config.streamIp ?? "",
      port: next.config.port,
      domain: next.config.domain,
      serverId: next.config.serverId,
      password: ""
    });
    hasExistingPassword.value = next.config.hasPassword;
  }

  async function loadStatus() {
    loading.value = true;
    error.value = "";
    try {
      const response = await api.status();
      if (response.code !== 0) throw new Error(response.message || "读取 SIP 配置失败");
      applyStatus(response.data);
      return response.data;
    } catch (cause) {
      error.value = errorText(cause);
      throw cause;
    } finally {
      loading.value = false;
    }
  }

  async function loadNetwork() {
    const response = await api.interfaces();
    if (response.code !== 0) throw new Error(response.message || "读取本机网络接口失败");
    network.value = response.data;
    return response.data;
  }

  function payload(): SaveSipConfigPayload {
    const wildcardLAN = form.deploymentMode === "lan" && form.listenIp === "0.0.0.0";
    const result: SaveSipConfigPayload = {
      deploymentMode: form.deploymentMode as SipDeploymentMode,
      listenIp: form.listenIp,
      advertiseIp: wildcardLAN ? "" : form.advertiseIp,
      advertiseIpInferred: wildcardLAN ? false : form.advertiseIpInferred,
      hookIp: form.hookIp.trim(),
      sdpIp: form.sdpIp.trim(),
      streamIp: form.streamIp.trim(),
      port: form.port,
      domain: form.domain,
      serverId: form.serverId
    };
    if (form.password !== "") result.password = form.password;
    return result;
  }

  async function save() {
    saving.value = true;
    error.value = "";
    try {
      const response = await api.save(payload());
      if (response.code !== 0) throw new Error(response.message || "保存 SIP 配置失败");
      hasExistingPassword.value = response.data.config.hasPassword;
      form.password = "";
      if (status.value) {
        status.value = {
          ...status.value,
          configStatus: "configured",
          config: response.data.config,
          runtime: response.data.runtime
        };
      }
      return response.data;
    } catch (cause) {
      error.value = errorText(cause);
      throw cause;
    } finally {
      saving.value = false;
    }
  }

  async function skip() {
    const response = await api.skip();
    if (response.code !== 0) throw new Error(response.message || "暂缓 SIP 配置失败");
    return response.data;
  }

  return { loading, saving, error, status, network, form, hasExistingPassword, loadStatus, loadNetwork, save, skip, payload };
}
