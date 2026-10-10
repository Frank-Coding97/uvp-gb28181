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
//   绿色安装包把**所有**端口压进一整段连续高位端口（51000-51064，
//   见 deploy/standalone/PORTS.md），但 SIP 端口**不经过 config.env** ——
//   它是管理员首次登录引导页时录入、存进 gb_sip_config 表的
//   （2026-07-20 起 SIP 明文配置从 YAML 移到了 DB）。
//   ⇒ 若这里写死 5061，绿色包首次打开向导就预填一个**段外**端口，
//     与"整段连续、防火墙一条规则"的规划自相矛盾。
//   ⇒ 出包时由 build-standalone.sh 注入 VITE_DEFAULT_SIP_PORT；
//     其它构建形态不带该变量 ⇒ 回落 5061，行为完全不变（互不影响）。
const FALLBACK_SIP_PORT = 5061;

function defaultSipPort(): number {
  const raw = import.meta.env.VITE_DEFAULT_SIP_PORT as string | undefined;
  const parsed = Number(raw);
  // 非法值（空 / 非数字 / 越界）一律回落：预填一个必然保存失败的端口
  // 会让现场以为是"平台不允许这个端口"，而真正的原因是打包注入写错了。
  return Number.isInteger(parsed) && parsed >= 1 && parsed <= 65535 ? parsed : FALLBACK_SIP_PORT;
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
    port: defaultSipPort(),
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
    if (!next.config) return;
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
