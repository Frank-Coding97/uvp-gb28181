/**
 * 系统内置字典名单（管理页只读）。
 *
 * ⛔ 背景：`sys_dict` 没有「系统级」列，且现存 25 个字典的 `created_by` **全是 1**
 *   （实测确认，无法当判据）⇒ 按台账 §3.2 的方案 B，**只在前端**把这批 code 的
 *   增删改置灰；**后端不拦**。若将来要真拦，得给 `sys_dict` 加 `is_system` 列
 *   （属 schema 改动：`schema.ir.json` + `seeds/` → `generate_sql.py` → 手工迁移）。
 *
 * ⭐ 入名单判据（两条都满足才加）：
 *   ① 值域由协议/代码**写死**（不是业务可自由增补的）；
 *   ② 改坏会导致**功能受损** —— 要么"那一档选不出来、下发不出去"，要么"回读对不上"。
 *   只影响展示文案的一律不加（改名无害）。
 *
 * ⛔ 刻意**不在**名单里（避免误伤）：
 *   - `post`（岗位）等**业务**字典 —— 管理员本就该能增补；
 *   - `device_status` / `media_node_state` / `cascade_register_state` —— 纯展示，
 *     且判定走**派生键**（`deviceStatus.ts` / `mediaNodeState.ts`），改名不影响逻辑；
 *   - `gender` / `status` / `taskStatus` / `playback_*` —— 同为展示态，暂按"可改"处理。
 *   （若日后要一并禁改，追加进下面的数组即可，并同步 `systemDictCodes.test.ts`。）
 */

/** GB/T 28181 附录值域：改了会让设备配置里那一档消失，或回读对不上。 */
const GB_PROTOCOL_DICT_CODES = [
  "ptz_type", // 摄像头类型（PTZType）
  "video_format", // 视频编码格式
  "video_resolution", // 视频分辨率
  "bit_rate_type", // 码率类型
  "channel_room_type", // 通道室内外
  "channel_supply_light_type", // 通道补光方式
  "channel_direction_type", // 通道监视方位
  "channel_position_type", // 通道位置类型
  "channel_use_type", // 通道用途
  "channel_photoelectric_imaging_type" // 通道光电成像类型
] as const;

/** 平台协议值域：值是代码里写死的字面量（见 `playbackProtocol.ts` 的 `isPlaybackProtocol`）。 */
const PLATFORM_PROTOCOL_DICT_CODES = ["gb28181_playback_protocol"] as const;

export const SYSTEM_DICT_CODES: readonly string[] = [...GB_PROTOCOL_DICT_CODES, ...PLATFORM_PROTOCOL_DICT_CODES];

const SYSTEM_DICT_CODE_SET = new Set<string>(SYSTEM_DICT_CODES);

/** 是否系统内置字典（管理页据此置灰增删改）。入参宽松：非字符串一律 `false`。 */
export function isSystemDict(code: unknown): boolean {
  return typeof code === "string" && SYSTEM_DICT_CODE_SET.has(code);
}

/** 置灰原因，用于 tooltip 与详情提示。 */
export const SYSTEM_DICT_READONLY_HINT = "系统内置字典：值域由国标/协议固定，不可增删改";
