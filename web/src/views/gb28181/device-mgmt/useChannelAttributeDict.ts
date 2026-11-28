/**
 * 通道属性字典的 Vue 注入层 —— 把六个字典读出来喂给 `channelAttributeText.ts` 的纯函数。
 * 纯模块保持不认识 Vue/pinia，单测可以直接跑（同 `useDeviceStatusDict` / `useVideoParamDict` 的分层）。
 *
 * ⛔ 六个值域全部是**只读展示**（设备上报什么就显示什么）。本层只负责取字典，
 *    不承担"未上报哨兵"与"多值拆分"这两条语义 —— 它们留在纯函数里。
 */
import { computed, type ComputedRef } from "vue";
import { useDictLabelMap } from "@/hooks/useDictOptions";
import {
  CHANNEL_DIRECTION_TYPE_LABEL_FALLBACK,
  CHANNEL_PHOTOELECTRIC_IMAGING_TYPE_LABEL_FALLBACK,
  CHANNEL_POSITION_TYPE_LABEL_FALLBACK,
  CHANNEL_ROOM_TYPE_LABEL_FALLBACK,
  CHANNEL_SUPPLY_LIGHT_TYPE_LABEL_FALLBACK,
  CHANNEL_USE_TYPE_LABEL_FALLBACK,
  DICT_CODE_CHANNEL_DIRECTION_TYPE,
  DICT_CODE_CHANNEL_PHOTOELECTRIC_IMAGING_TYPE,
  DICT_CODE_CHANNEL_POSITION_TYPE,
  DICT_CODE_CHANNEL_ROOM_TYPE,
  DICT_CODE_CHANNEL_SUPPLY_LIGHT_TYPE,
  DICT_CODE_CHANNEL_USE_TYPE,
  channelAttributeEntries,
  type ChannelAttributeEntry,
  type ChannelAttributeLabels,
  type ChannelAttributeSnapshot
} from "./channelAttributeText";

/** 六个值域的已合并查表（每个都 `useDictLabelMap(code, 协议值域兜底)`）。 */
export function useChannelAttributeLabels(): ComputedRef<ChannelAttributeLabels> {
  const roomType = useDictLabelMap(DICT_CODE_CHANNEL_ROOM_TYPE, CHANNEL_ROOM_TYPE_LABEL_FALLBACK);
  const supplyLightType = useDictLabelMap(DICT_CODE_CHANNEL_SUPPLY_LIGHT_TYPE, CHANNEL_SUPPLY_LIGHT_TYPE_LABEL_FALLBACK);
  const directionType = useDictLabelMap(DICT_CODE_CHANNEL_DIRECTION_TYPE, CHANNEL_DIRECTION_TYPE_LABEL_FALLBACK);
  const positionType = useDictLabelMap(DICT_CODE_CHANNEL_POSITION_TYPE, CHANNEL_POSITION_TYPE_LABEL_FALLBACK);
  const useType = useDictLabelMap(DICT_CODE_CHANNEL_USE_TYPE, CHANNEL_USE_TYPE_LABEL_FALLBACK);
  const photoelectricImagingType = useDictLabelMap(
    DICT_CODE_CHANNEL_PHOTOELECTRIC_IMAGING_TYPE,
    CHANNEL_PHOTOELECTRIC_IMAGING_TYPE_LABEL_FALLBACK
  );

  return computed<ChannelAttributeLabels>(() => ({
    roomType: roomType.value,
    supplyLightType: supplyLightType.value,
    directionType: directionType.value,
    positionType: positionType.value,
    useType: useType.value,
    photoelectricImagingType: photoelectricImagingType.value
  }));
}

/**
 * 页面里取一次即可：`const channelAttributeRows = computed(() => channelAttributeEntriesOf(channelDetail.value ?? {}));`
 */
export function useChannelAttributeEntries(): (channel: ChannelAttributeSnapshot) => ChannelAttributeEntry[] {
  const labels = useChannelAttributeLabels();
  return (channel: ChannelAttributeSnapshot): ChannelAttributeEntry[] => channelAttributeEntries(channel, labels.value);
}
