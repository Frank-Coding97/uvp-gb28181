import { computed, ref } from "vue";

export interface SelectionDevice {
    id: number;
    deviceId: string;
    name: string;
}

export interface OperationResultIds {
    changed?: number[];
    skipped?: number[];
    failed?: number[];
}

export function useCrossPageSelection() {
    const selected = new Map<number, SelectionDevice>();
    const visiblePage = ref<SelectionDevice[]>([]);
    const selectedVersion = ref(0);
    const unavailableIds = ref<number[]>([]);

    const selectedDevices = computed(() => {
        void selectedVersion.value;
        return [...selected.values()];
    });
    const selectedIds = computed(() => selectedDevices.value.map((item) => item.id));
    const selectedCount = computed(() => selectedDevices.value.length);
    const currentPageKeys = computed(() => visiblePage.value.filter((item) => selected.has(item.id)).map((item) => item.id));
    const hiddenCount = computed(() => selectedCount.value - currentPageKeys.value.length);

    const touch = () => {
        selectedVersion.value += 1;
    };

    const add = (device: SelectionDevice) => {
        selected.set(device.id, device);
        touch();
    };

    const remove = (deviceId: number) => {
        if (selected.delete(deviceId)) touch();
    };

    const clear = () => {
        if (!selected.size) return;
        selected.clear();
        touch();
    };

    const setVisiblePage = (devices: SelectionDevice[]) => {
        visiblePage.value = devices;
        for (const device of devices) {
            if (selected.has(device.id)) selected.set(device.id, device);
        }
        touch();
    };

    const applyPageSelection = (devices: SelectionDevice[], checkedIds: number[]) => {
        setVisiblePage(devices);
        const checked = new Set(checkedIds);
        for (const device of devices) {
            if (checked.has(device.id)) selected.set(device.id, device);
            else selected.delete(device.id);
        }
        touch();
    };

    const applyOperationResult = (result: OperationResultIds) => {
        for (const id of [...(result.changed ?? []), ...(result.skipped ?? [])]) selected.delete(id);
        touch();
    };

    const removeUnavailable = (ids: number[]) => {
        const current = new Set(unavailableIds.value);
        for (const id of ids) {
            selected.delete(id);
            current.add(id);
        }
        unavailableIds.value = [...current];
        touch();
    };

    return {
        selectedDevices,
        selectedIds,
        selectedCount,
        currentPageKeys,
        hiddenCount,
        unavailableIds,
        add,
        remove,
        clear,
        setVisiblePage,
        applyPageSelection,
        applyOperationResult,
        removeUnavailable
    };
}
