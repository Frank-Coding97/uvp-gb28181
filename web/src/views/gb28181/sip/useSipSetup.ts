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

export interface SipSetupForm {
    deploymentMode: SipDeploymentMode | "";
    listenIp: string;
    advertiseIp: string;
    advertiseIpInferred: boolean;
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
        port: 5061,
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
            advertiseIp: next.config.advertiseIp,
            advertiseIpInferred: next.config.advertiseIpInferred,
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
        const result: SaveSipConfigPayload = {
            deploymentMode: form.deploymentMode as SipDeploymentMode,
            listenIp: form.listenIp,
            advertiseIp: form.advertiseIp,
            advertiseIpInferred: form.advertiseIpInferred,
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
