import { reactive } from "vue";
import { listFirmwareUpgrades, listMaintenanceOperations, type DeviceOperationResult, type MaintenanceOperation, type UpgradeOperation } from "./api";

type Action = "reboot" | "upgrade";
type Reboot = DeviceOperationResult | MaintenanceOperation;
interface Activity {
    reboot?: Reboot;
    upgrade?: UpgradeOperation;
    uncertain?: Action;
    loading: boolean;
    error: boolean;
    revision: number;
}

export function isRebootPending(operation?: Reboot) {
    return !!operation && (["queued", "pending", "accepted"].includes(operation.status || "") || (operation.status === "sent" && operation.responseRequired === true));
}
export function isUpgradeUnresolved(operation?: UpgradeOperation) {
    return !!operation && ["queued", "sent", "accepted", "unknown"].includes(operation.status);
}

// Page-owned state: closing a dialog does not change an operation's lifetime.
export function createMaintenanceActivity(api = { listFirmwareUpgrades, listMaintenanceOperations }) {
    const devices = reactive<Record<number, Activity>>({});
    const get = (id: number) => devices[id] ?? (devices[id] = { loading: false, error: false, revision: 0 });
    const rememberUpgrade = (id: number, operation: UpgradeOperation) => {
        const state = get(id);
        state.revision += 1;
        state.upgrade = operation;
        if (state.uncertain === "upgrade") state.uncertain = undefined;
    };
    const rememberReboot = (id: number, operation: Reboot) => {
        const state = get(id);
        state.revision += 1;
        state.reboot = operation;
        if (state.uncertain === "reboot") state.uncertain = undefined;
    };
    const markUncertain = (id: number, action: Action) => {
        const state = get(id);
        state.revision += 1;
        state.uncertain = action;
    };
    async function refresh(id: number) {
        const state = get(id);
        if (state.loading) return false;
        state.loading = true;
        const revision = state.revision;
        try {
            const [reboots, upgrades] = await Promise.all([
                api.listMaintenanceOperations(id, { page: 1, pageSize: 100 }),
                api.listFirmwareUpgrades(id, { page: 1, pageSize: 100 })
            ]);
            if (reboots.code !== 0 || upgrades.code !== 0 || !reboots.data || !upgrades.data) throw new Error("Maintenance query failed");
            if (revision !== state.revision) return false;
            const rebootRows = reboots.data.list || [];
            const upgradeRows = upgrades.data.list || [];
            state.reboot = rebootRows.find(row => row.operationId === state.reboot?.operationId) ?? state.reboot;
            state.upgrade = upgradeRows.find(row => row.operationId === state.upgrade?.operationId) ?? state.upgrade;
            state.reboot = rebootRows.find(isRebootPending) ?? state.reboot;
            state.upgrade = upgradeRows.find(isUpgradeUnresolved) ?? state.upgrade;
            state.error = false;
            return true;
        } catch {
            if (revision === state.revision) state.error = true;
            return false;
        } finally {
            state.loading = false;
        }
    }
    function reason(id: number, action: Action) {
        const state = devices[id];
        if (!state) return "";
        if (state.loading) return "正在核查设备维护状态，请稍候。";
        if (state.error) return "未能核查维护状态，请关闭后重新打开重试。";
        if (state.uncertain) return "上一次请求结果未知，请先核对维护记录，暂不要重复操作。";
        if (action === "reboot" && isUpgradeUnresolved(state.upgrade)) return "升级任务尚未确认完成，暂不能重启设备。";
        if (isRebootPending(state.reboot)) return "设备重启请求处理中，暂不能发起新的维护操作。";
        if (action === "upgrade" && isUpgradeUnresolved(state.upgrade)) return "已有升级任务尚未确认完成，请查看当前任务或维护记录。";
        return "";
    }
    function label(id: number) {
        const state = devices[id];
        if (!state) return "";
        if (state.uncertain || state.upgrade?.status === "unknown") return "维护结果待确认";
        if (isUpgradeUnresolved(state.upgrade)) return "升级处理中";
        if (isRebootPending(state.reboot)) return "重启请求处理中";
        return "";
    }
    return { devices, get, refresh, rememberUpgrade, rememberReboot, markUncertain, reason, label };
}
