/**
 * 设备配置家族的「表单键 ↔ 协议块」翻译层（**唯一**允许翻译的地方）。
 *
 * ## 为什么需要这一层，以及为什么只能有一层
 *
 * 两张词汇表的存在是**结构性的**，不是设计失误：
 *
 *   - 协议侧（后端 `manscdp` 的 json tag）：键名 = 标准元素名小驼峰，
 *     一元素一字段，`PictureMask` 是一个 `regions[]` 数组、`VideoRecordPlan`
 *     是 `schedules[].segments[]` 的两层嵌套。
 *   - 表单侧（`deviceConfigGroups.ts`）：字段 id 为**排版**服务 ——
 *     四个 `maskN` 平铺成四行、录像计划摊成 7 天 × 时段、可选数字用空串表示"不下发"。
 *
 * ⛔ 两张表必须一对一地在这里对上，别把任何一格翻译挪到接口层或后端：
 *    挪到后端 = 后端要知道前端的表单长什么样；挪到接口层 = 造第三套词汇表。
 *    本仓已经为"同一件事写两份判定、改一处忘一处"栽过多次。
 *
 * ## 三条贯穿本文件的口径
 *
 *  1. **空值 ≠ 0**。协议里可选元素（`BasicParam.Expiration`、`AlarmRecord.RecordTime`…）
 *     缺席与"写了 0"是两件事，后端用指针承载这件事。表单侧的统一表达是**空串**：
 *     空串 ⇒ 不下发该元素；`"0"` ⇒ 真的下发 0。
 *  2. **严格发，不静默夹取**。取值范围在协议层收口，违规**拒发**。这里先本地拦一道
 *     （同样的规则、双重保险），因为平台自己发出的越界值会被对端静默当 0 处理，
 *     那种错在回读对账里只表现为"设备没照做"，归因成本极高。
 *  3. **计数不独立存**。`SumNum` / `Num` / `RecordScheduleSumNum` 全由数组长度现算，
 *     绝不放进表单 —— 存成独立字段必然出现"计数说 3 个、实际 2 个"的自相矛盾状态。
 */
import { MAX_MASK_REGIONS, MAX_OSD_TEXT_ITEMS, MAX_OSD_TEXT_LENGTH, MASK_AXES, type FieldValue } from "./deviceConfigGroups";

/** 表单值袋：`{ 字段id: 值 }`。 */
export type FormValues = Record<string, FieldValue>;

export interface BuildResult {
  /** 可下发的协议块（键名 = 标准元素名小驼峰）。校验失败时为空对象。 */
  blocks: Record<string, unknown>;
  /** 校验失败的说明；空串 = 通过。 */
  error: string;
  /**
   * 能下发、但有件事必须就地告诉用户。
   *
   * 与 `error` 的分界很清楚：`error` = **没发出去**；`notice` = **发出去了**，
   * 但用户对结果的预期可能与实际不符，需要在操作后立刻说明。
   * 目前只有一处：`On=1` 且一个遮挡区域都没有（见 [buildPicture]）。
   */
  notice?: string;
}

/* ─────────────────────── 基础取值/解析 ─────────────────────── */

/**
 * 解析一个"可选整数"表单值。
 *
 * 返回 `null` = **不下发该元素**。这就是空串（以及纯空白）的全部含义。
 * 非法输入（非数字、越界）返回 `undefined` —— 调用方据此报错拒发。
 */
function optionalInt(value: FieldValue | undefined, min: number, max: number): number | null | undefined {
  if (value === undefined || value === null) return null;
  if (typeof value === "boolean") return undefined;
  const raw = String(value).trim();
  if (raw === "") return null;
  const parsed = Number(raw);
  if (!Number.isFinite(parsed) || !Number.isInteger(parsed)) return undefined;
  if (parsed < min || parsed > max) return undefined;
  return parsed;
}

/** 必填整数：空串同样非法（必选元素缺席会被对端当 0 处理）。 */
function requiredInt(value: FieldValue | undefined, min: number, max: number): number | undefined {
  const parsed = optionalInt(value, min, max);
  return parsed === null || parsed === undefined ? undefined : parsed;
}

/** 0/1 开关（后端是 int，不是 bool —— 传 bool 会反序列化失败）。 */
function switchValue(value: FieldValue | undefined): 0 | 1 {
  return value === true || value === 1 || value === "1" || value === "true" ? 1 : 0;
}

function boolFromSwitch(value: unknown): boolean {
  return Number(value) === 1;
}

function coordArray(value: FieldValue | undefined, length: number): number[] {
  const raw = Array.isArray(value) ? value : [];
  const result: number[] = [];
  for (let index = 0; index < length; index += 1) {
    const item = Number(raw[index]);
    result.push(Number.isFinite(item) ? item : 0);
  }
  return result;
}

/**
 * 把表单袋里的某个值当成普通对象看。
 *
 * 入参刻意是 `unknown` 而不是 `FieldValue`：表单值是个联合类型（字符串/数字/数组/
 * 结构化列表），直接 `as Record<string, unknown>` 会被 TS 判成"两个类型没有足够重叠"。
 * 这里本来就是"运行时才知道形状、由各分支自己收窄"的场景，所以从 unknown 入口进。
 */
function looseRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

/**
 * 表单里的文本行 → 收窄后的中间形状（**带**草稿标记）。
 *
 * ⛔ 这里刻意保留 `placed`，虽然它不是协议字段：`buildOSD` 要靠它判定
 *    「这一行用户到底摆过没有」（见 [ConfigTextItem.placed] 的注释）。
 *    翻译成协议元素是**下一步**的事（`toOsdItems`），别在这一步就把信息丢掉 ——
 *    丢了就只能拿 `x`/`y` 反推，而 `0,0` 是合法坐标，反推必然误判。
 */
function textItems(value: FieldValue | undefined): { text: string; x: number; y: number; placed: boolean }[] {
  if (!Array.isArray(value)) return [];
  return value.map(item => {
    const record = looseRecord(item);
    return {
      text: String(record.text ?? ""),
      x: Number(record.x) || 0,
      y: Number(record.y) || 0,
      // 缺席按**已定位**：回读播种出来的行、以及没有这个键的老草稿，都是摆过的。
      placed: record.placed === undefined ? true : record.placed === true
    };
  });
}

/**
 * 收窄后的文本行 → 协议元素。
 *
 * ⛔ **只带协议认得的三个键**：`placed` 是纯前端草稿标记，不进报文。
 *    与"计数不独立存"（`SumNum` 现算）同源 —— 报文里出现的东西必须都是标准里的东西。
 */
function toOsdItems(rows: { text: string; x: number; y: number }[]): { text: string; x: number; y: number }[] {
  return rows.map(row => ({ text: row.text, x: row.x, y: row.y }));
}

function scheduleDays(value: FieldValue | undefined): { weekDayNum: number; segments: { start: string; stop: string }[] }[] {
  if (!Array.isArray(value)) return [];
  return value.map(item => {
    const record = looseRecord(item);
    const segments = Array.isArray(record.segments) ? record.segments : [];
    return {
      weekDayNum: Number(record.weekDayNum) || 0,
      segments: segments.map(segment => {
        const seg = looseRecord(segment);
        return { start: String(seg.start ?? ""), stop: String(seg.stop ?? "") };
      })
    };
  });
}

/* ─────────────────────── HH:MM[:SS] ─────────────────────── */

export interface DayTime {
  hour: number;
  min: number;
  sec: number;
}

/**
 * 解析 `HH:MM` 或 `HH:MM:SS`。
 *
 * ⛔ **必须接受并保留秒**：协议里 `StartSec`/`StopSec` 都是必选整数，只让用户填到分钟
 *    就等于每次下发都把秒清零 —— 那是静默改写设备上已有的计划，回读对账还会显示一致。
 */
export function parseDayTime(raw: string): DayTime | null {
  const parts = String(raw).trim().split(":");
  if (parts.length !== 2 && parts.length !== 3) return null;
  const [hour, min, sec = "0"] = parts as [string, string, string?];
  const values = [hour, min, sec].map(part => Number(String(part).trim()));
  if (values.some(value => !Number.isInteger(value))) return null;
  const [h, m, s] = values as [number, number, number];
  if (h < 0 || h > 23 || m < 0 || m > 59 || s < 0 || s > 59) return null;
  return { hour: h, min: m, sec: s };
}

/** 反向：线格式 → 表单串。**恒定补到秒**，这样 read→write 是恒等映射。 */
export function formatDayTime(hour: unknown, min: unknown, sec: unknown): string {
  return [Number(hour) || 0, Number(min) || 0, Number(sec) || 0].map(value => String(value).padStart(2, "0")).join(":");
}

/* ─────────────────────── 读取：协议块 → 表单值 ─────────────────────── */

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

/**
 * 把某一条 `payload` 折成表单值。
 *
 * ⛔ **`payload` 是那一块本身**（`OSDConfig` 这一行存的就是
 *    `{"length":1920,...,"items":[...]}`），**不是**包在 ConfigType 键下的容器。
 *    库里的 `gb_device_config.payload_json` 按 `(device, target, configType)` 一行一块，
 *    所以这里**直接读顶层字段**，不要去取 `payload[configType]` —— 取不到会静默返回
 *    一份空表单，界面上表现为"回读回来一片空白"，而回读其实成功了。
 *    （下发请求里的 `blocks` 才是容器；两者形状不同，别互相套用。）
 *
 * ⛔ 只翻译**这组覆盖的**配置类型：`configType` 参数只用来选分支，
 *    不要拿它去 payload 里找同名的键。
 */
export function readDeviceConfigPayload(configType: string, payload: Record<string, unknown>): FormValues {
  const block = asRecord(payload);
  switch (configType) {
    case "OSDConfig":
      return {
        timeEnable: boolFromSwitch(block.timeEnable),
        // 缺席 ⇒ 空串 ⇒ 下次下发不发送该元素。**不能**补成 "0"：
        // 0 表示「YYYY-MM-DD HH:MM:SS」这个特定格式，与"设备不指定"不是一回事。
        timeType: block.timeType === undefined || block.timeType === null ? "" : String(block.timeType),
        // 滑杆承载的字段统一输出**字符串**：与表单初值、滑杆清空值同型，
        // 否则"刚回读完"就会被脏值统计算成"已改"（数字 vs 字符串）。
        length: String(Number(block.length) || 0),
        width: String(Number(block.width) || 0),
        timeX: String(Number(block.timeX) || 0),
        timeY: String(Number(block.timeY) || 0),
        textEnable: boolFromSwitch(block.textEnable),
        items: (Array.isArray(block.items) ? block.items : []).map(item => {
          const record = asRecord(item);
          return {
            text: String(record.text ?? ""),
            x: Number(record.x) || 0,
            y: Number(record.y) || 0,
            // ⛔ 回读回来的行**一律是已定位**的：设备上就摆在那儿。
            //    不显式播种的话，草稿里这些行是 `undefined`、基准里也是 `undefined`，
            //    看着一致 —— 但一旦用户拖动其中一行，脏值对比就会把"整行"算成新值，
            //    而那些 `undefined` 的行在画布上会落进"未定位"分支，画成琥珀虚线。
            placed: true
          };
        })
      };
    case "FrameMirror":
      // ⛔ 表单键是 `mirror`，协议键是 `value` —— 这一处就是"两张词汇表不对齐"的
      //    典型例子（表单要 `mask1..mask4` 四个槽位、协议只有一个 `value`）。
      //    翻译只在这里发生，别把 `value` 泄进 `deviceConfigGroups.ts`。
      return { mirror: String(Number(block.value) || 0) };
    case "PictureMask": {
      const values: FormValues = { maskOn: boolFromSwitch(block.on) };
      for (let index = 0; index < MAX_MASK_REGIONS; index += 1) values[`mask${index + 1}`] = [0, 0, 0, 0];
      // 按 `Seq` 归位而不是按数组下标：协议里 Seq 是设备自己给的位置（1~4），
      // 按下标归位会在"设备只回了 Seq=3"时把区域错画成第 1 个。
      for (const item of Array.isArray(block.regions) ? block.regions : []) {
        const record = asRecord(item);
        const seq = Number(record.seq);
        if (!Number.isInteger(seq) || seq < 1 || seq > MAX_MASK_REGIONS) continue;
        values[`mask${seq}`] = [
          Number(record.left) || 0,
          Number(record.top) || 0,
          Number(record.right) || 0,
          Number(record.bottom) || 0
        ];
      }
      return values;
    }
    case "VideoRecordPlan":
      return {
        recordEnable: boolFromSwitch(block.recordEnable),
        streamNumber: String(Number(block.streamNumber) || 0),
        schedules: (Array.isArray(block.schedules) ? block.schedules : []).map(item => {
          const record = asRecord(item);
          const segments = Array.isArray(record.segments) ? record.segments : [];
          return {
            weekDayNum: Number(record.weekDayNum) || 0,
            segments: segments.map(segment => {
              const seg = asRecord(segment);
              return {
                start: formatDayTime(seg.startHour, seg.startMin, seg.startSec),
                stop: formatDayTime(seg.stopHour, seg.stopMin, seg.stopSec)
              };
            })
          };
        })
      };
    case "VideoAlarmRecord":
      return {
        recordEnable: boolFromSwitch(block.recordEnable),
        streamNumber: String(Number(block.streamNumber) || 0),
        recordTime: block.recordTime === undefined || block.recordTime === null ? "" : String(block.recordTime),
        preRecordTime: block.preRecordTime === undefined || block.preRecordTime === null ? "" : String(block.preRecordTime)
      };
    case "basicParam":
      return {
        name: block.name === undefined || block.name === null ? "" : String(block.name),
        expiration: block.expiration === undefined || block.expiration === null ? "" : String(block.expiration),
        heartBeatInterval:
          block.heartBeatInterval === undefined || block.heartBeatInterval === null ? "" : String(block.heartBeatInterval),
        heartBeatCount: block.heartBeatCount === undefined || block.heartBeatCount === null ? "" : String(block.heartBeatCount)
      };
    case "AlarmReport":
      return {
        motionDetection: boolFromSwitch(block.motionDetection),
        fieldDetection: boolFromSwitch(block.fieldDetection)
      };
    default:
      return {};
  }
}

/* ─────────────────────── 下发：表单值 → 协议块 ─────────────────────── */

/**
 * 把一组表单值编成协议块。
 *
 * 返回值里的 `blocks` 可能带**多个** ConfigType（「画面处理」同时给 FrameMirror
 * 与 PictureMask）—— 一次下发多组是标准允许的形态（A.2.3.2 允许兄弟元素并存）。
 */
export function buildDeviceConfigBlocks(groupKey: string, values: FormValues): BuildResult {
  switch (groupKey) {
    case "osd":
      return buildOSD(values);
    case "picture":
      return buildPicture(values);
    case "record-plan":
      return buildRecordPlan(values);
    case "alarm-record":
      return buildAlarmRecord(values);
    case "basic":
      return buildBasicParam(values);
    case "alarm-report":
      return {
        blocks: {
          alarmReport: {
            // 两个都是必选整数：不下发会被对端当 0（= 关掉上报）。
            motionDetection: switchValue(values.motionDetection),
            fieldDetection: switchValue(values.fieldDetection)
          }
        },
        error: ""
      };
    default:
      return { blocks: {}, error: `未知的配置分组: ${groupKey}` };
  }
}

function buildOSD(values: FormValues): BuildResult {
  // ⛔ `length` / `width` 在界面上**已经不是可编辑控件**了（2026-09-20 从两条滑杆改成
  //    只读的「坐标画布」事实块）：设备拒收平台改写，平台只能读它、照它算。
  //    ⇒ 这两个值到不了"用户填错"这条路，只可能"设备没声明"。
  //    所以拒发文案不能再写「OSD 窗口长度必须是 1~3840 的整数」—— 界面上已经没有
  //    这个控件了，用户会看到一条**指向不存在控件的错误**，唯一的动作只能是猜。
  const length = Number(values.length);
  const width = Number(values.width);
  if (!Number.isFinite(length) || length <= 0 || !Number.isFinite(width) || width <= 0) {
    return {
      blocks: {},
      error: "设备未声明坐标画布（Length/Width），无法下发本组配置；请点「读取」重新向设备查询"
    };
  }
  // 设备回了一份不合法的画布（违约报文）。这与"没声明"是两件事，不能共用一句话。
  if (length > 3840 || width > 2160) {
    return { blocks: {}, error: `设备声明的坐标画布超出可用范围（${length}×${width}），无法下发本组配置` };
  }

  const timeX = requiredInt(values.timeX, 0, 8192);
  if (timeX === undefined) return { blocks: {}, error: "OSD 时间 X 坐标必须是 0~8192 的整数" };
  const timeY = requiredInt(values.timeY, 0, 8192);
  if (timeY === undefined) return { blocks: {}, error: "OSD 时间 Y 坐标必须是 0~8192 的整数" };

  const rawTimeType = String(values.timeType ?? "").trim();
  let timeType: number | undefined;
  if (rawTimeType !== "") {
    const parsed = requiredInt(values.timeType, 0, 1);
    if (parsed === undefined) return { blocks: {}, error: "OSD 时间格式只能是 0 / 1，或选择「跟设备走」" };
    timeType = parsed;
  }

  const items = textItems(values.items);
  if (items.length > MAX_OSD_TEXT_ITEMS) {
    return { blocks: {}, error: `OSD 叠加文字最多 ${MAX_OSD_TEXT_ITEMS} 条` };
  }
  for (let index = 0; index < items.length; index += 1) {
    const item = items[index]!;
    // ⛔ **未定位的行不许下发**。`0,0` 是合法坐标（见 [ConfigTextItem.placed]），
    //    从值上判不出"用户还没摆"，所以判据必须来自草稿里的定位标记。
    //    放行的话，设备上会真的多出一行贴在左上角的字，而界面看起来一切正常。
    if (!item.placed) {
      return { blocks: {}, error: `第 ${index + 1} 条叠加文字还没在画面上定位 —— 拖动它的标记再下发` };
    }
    // 按**字符**数判（不是字节）：标准写的是"长度 0~32"，中文一个字占 3 字节，
    // 按字节判会让 11 个汉字就被拒。
    if ([...item.text].length > MAX_OSD_TEXT_LENGTH) {
      return { blocks: {}, error: `第 ${index + 1} 条叠加文字超过 ${MAX_OSD_TEXT_LENGTH} 个字符` };
    }
    if (item.x < 0 || item.y < 0) {
      return { blocks: {}, error: `第 ${index + 1} 条叠加文字的坐标不得为负` };
    }
  }

  return {
    blocks: {
      osdConfig: {
        length,
        width,
        timeX,
        timeY,
        timeEnable: switchValue(values.timeEnable),
        // ⛔ 键整个不出现 = 不下发该元素 = 设备自定格式。
        // 写成 `timeType: 0` 会被对端当成"就要 YYYY-MM-DD HH:MM:SS"。
        ...(timeType === undefined ? {} : { timeType }),
        textEnable: switchValue(values.textEnable),
        items: toOsdItems(items)
      }
    },
    error: ""
  };
}

function buildPicture(values: FormValues): BuildResult {
  const mirror = requiredInt(values.mirror, 0, 3);
  if (mirror === undefined) return { blocks: {}, error: "画面镜像只能是 0~3（标准 enumeration，越界一律拒发）" };

  const regions: { seq: number; left: number; top: number; right: number; bottom: number }[] = [];
  for (let index = 0; index < MAX_MASK_REGIONS; index += 1) {
    const [left, top, right, bottom] = coordArray(values[`mask${index + 1}`], 4) as [number, number, number, number];
    if ([left, top, right, bottom].some(value => !Number.isInteger(value) || value < 0)) {
      return { blocks: {}, error: `遮挡区 ${index + 1} 的四个坐标必须是非负整数` };
    }
    // 四个数全 0 = 这个槽位没启用。**不发**该区域。
    // ⛔ 这里跳过的是"**用户意图**里没有这一槽"，不是"报文里没有这一槽"：报文层由后端
    //    把它补成全量声明（`pictureMaskFullRegions`）。省略 ≠ 删除 —— 设备把 `RegionList`
    //    当按 `Seq` 的增量补丁，**省略一个槽位等于"别碰它"**，那正是"遮挡区删不掉"
    //    的根因（2026-09-20 真机：删掉 Seq4 后下发，设备回读 `Num="4"`、Seq4 原样还在）。
    //    ⛔ 别改成"本地填一条 0,0,0,0"：零面积是**删除**语义，由后端统一补，才能保证
    //    "哪些槽该补"只有一处判据（前端补一份 = 同一个语义两个真源）。
    if (left === 0 && top === 0 && right === 0 && bottom === 0) continue;
    if (left > right || top > bottom) {
      // ⛔ 报错而不是自动交换：自动纠正会让下发值与回读值不一致，
      // 而对账只能看到"设备没照做"。
      return { blocks: {}, error: `遮挡区 ${index + 1} 坐标倒置（${MASK_AXES.join("/")} 要求 左上 ≤ 右下）` };
    }
    regions.push({ seq: index + 1, left, top, right, bottom });
  }

  const maskOn = switchValue(values.maskOn);
  // 「启用遮挡 + 一个区域都没有」**照发**，但必须给出提示（2026-09-19 真机实测 + 产品决定）。
  //
  // 这里原来是一句 `return { error: … }`（本地拒发）。改成"放行 + 提示"的依据：
  //   1. 设备**接受**这个请求。真机实测（海康 IPC）：发 `<On>1</On><SumNum>0</SumNum>`
  //      （不带 `RegionList`），回读 `On=1`，且**不会凭空创建区域** ⇒ "只启用"是合法且
  //      可执行的请求。协议里本来也不存在"独立的启用信令"——`On` 只是 `PictureMask`
  //      里的一个字段，启用只能搭这条 DeviceConfig 报文发出去。
  //   2. 拦住的代价比放行高：操作员点开关/点下发时遇到的是"什么都不发生"，
  //      而归因到"是本地守卫拦的、不是设备不认"的成本极高（2026-09-19 现场反复发生）。
  //   3. 但**效果**必须说清：本次没有携带区域，画面上多半不会有可见的遮挡。
  //      不说，用户下一句必然是"为什么启用了没效果"。
  // ⛔ 别把这条提示当成"既然放行了就可以不留"——它是"启用了却没有遮挡"唯一的事后解释。
  //    设备侧是否还留着旧区域、要不要补一句，由调用方的 `pictureMaskApplyNotice` 拼（那里才有设备事实）。
  const notice = maskOn === 1 && regions.length === 0 ? "已下发「启用」，但本次没有携带任何遮挡区域。" : "";

  return {
    blocks: {
      frameMirror: { value: mirror },
      pictureMask: { on: maskOn, regions }
    },
    error: "",
    notice
  };
}

function buildRecordPlan(values: FormValues): BuildResult {
  const recordEnable = switchValue(values.recordEnable);
  const streamNumber = requiredInt(values.streamNumber, 0, 3);
  if (streamNumber === undefined) return { blocks: {}, error: "录像码流必须选 0~3" };

  const seen = new Set<number>();
  const schedules: {
    weekDayNum: number;
    segments: {
      startHour: number;
      startMin: number;
      startSec: number;
      stopHour: number;
      stopMin: number;
      stopSec: number;
    }[];
  }[] = [];
  for (const day of scheduleDays(values.schedules)) {
    if (!Number.isInteger(day.weekDayNum) || day.weekDayNum < 1 || day.weekDayNum > 7) {
      return { blocks: {}, error: `录像计划的星期取值越界: ${day.weekDayNum}（合法 1~7）` };
    }
    if (seen.has(day.weekDayNum)) {
      return { blocks: {}, error: `录像计划的星期 ${day.weekDayNum} 重复` };
    }
    seen.add(day.weekDayNum);
    const segments = [];
    for (let index = 0; index < day.segments.length; index += 1) {
      const segment = day.segments[index]!;
      const start = parseDayTime(segment.start);
      const stop = parseDayTime(segment.stop);
      if (!start || !stop) {
        return {
          blocks: {},
          error: `周${day.weekDayNum} 第 ${index + 1} 个时段的时刻格式不对（应为 HH:MM 或 HH:MM:SS）`
        };
      }
      segments.push({
        startHour: start.hour,
        startMin: start.min,
        startSec: start.sec,
        stopHour: stop.hour,
        stopMin: stop.min,
        stopSec: stop.sec
      });
    }
    // 「如当天无录像计划可缺少」⇒ 没有时段的那天**整条不发**，
    // 而不是发一条 segments=[] 的空记录。
    if (!segments.length) continue;
    schedules.push({ weekDayNum: day.weekDayNum, segments });
  }

  return {
    blocks: {
      videoRecordPlan: {
        recordEnable,
        schedules,
        streamNumber
        // RecordScheduleSumNum / TimeSegmentSumNum 不在这里出现：后端由数组长度现算。
      }
    },
    error: ""
  };
}

function buildAlarmRecord(values: FormValues): BuildResult {
  const recordEnable = switchValue(values.recordEnable);
  const streamNumber = requiredInt(values.streamNumber, 0, 3);
  if (streamNumber === undefined) return { blocks: {}, error: "录像码流必须选 0~3" };

  const recordTime = optionalInt(values.recordTime, 0, 86400);
  if (recordTime === undefined) return { blocks: {}, error: "录像延时必须是 0~86400 的整数（留空 = 不下发）" };
  const preRecordTime = optionalInt(values.preRecordTime, 0, 86400);
  if (preRecordTime === undefined) {
    return { blocks: {}, error: "预录时长必须是 0~86400 的整数（留空 = 不下发）" };
  }

  return {
    blocks: {
      videoAlarmRecord: {
        recordEnable,
        ...(recordTime === null ? {} : { recordTime }),
        ...(preRecordTime === null ? {} : { preRecordTime }),
        streamNumber
      }
    },
    error: ""
  };
}

function buildBasicParam(values: FormValues): BuildResult {
  const name = String(values.name ?? "").trim();
  // ⛔ 空串 = 不下发该元素；但**非空串里的纯空白**也是非法的（后端会拒）。
  // 这里静默丢掉空白后为空就直接省略，与"用户没填"同解，避免拿空格去撞后端校验。
  const expiration = optionalInt(values.expiration, 1, 86400);
  if (expiration === undefined) return { blocks: {}, error: "注册有效期必须是 1~86400 的整数（留空 = 不下发）" };
  const heartBeatInterval = optionalInt(values.heartBeatInterval, 1, 86400);
  if (heartBeatInterval === undefined) {
    return { blocks: {}, error: "心跳间隔必须是 1~86400 的整数（留空 = 不下发）" };
  }
  const heartBeatCount = optionalInt(values.heartBeatCount, 1, 1000);
  if (heartBeatCount === undefined) return { blocks: {}, error: "心跳超时次数必须是 1~1000 的整数（留空 = 不下发）" };

  const basicParam: Record<string, unknown> = {};
  if (name !== "") basicParam.name = name;
  if (expiration !== null) basicParam.expiration = expiration;
  if (heartBeatInterval !== null) basicParam.heartBeatInterval = heartBeatInterval;
  if (heartBeatCount !== null) basicParam.heartBeatCount = heartBeatCount;

  if (!Object.keys(basicParam).length) {
    // 后端口径一致："四项配置全缺（空下发没有可落库的内容）"。
    return { blocks: {}, error: "基本参数四项全空：请至少填一项，或整组跳过" };
  }
  return { blocks: { basicParam }, error: "" };
}

/* ─────────────────────── 脏值统计 ─────────────────────── */

function stableValue(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(stableValue).join(",")}]`;
  if (value && typeof value === "object") {
    const record = value as Record<string, unknown>;
    return `{${Object.keys(record)
      .sort()
      .map(key => `${key}:${stableValue(record[key])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value ?? null);
}

/**
 * 相对**最近一次回读值**，本组有哪些字段被改过。
 *
 * ⛔ 基准是回读值而不是"打开面板时的值"：下发后设备可能改回别人的值，
 *    拿旧快照当基准会让"设备已经不同了"这件事被算成"用户没改"。
 */
export function changedFieldKeys(current: FormValues, baseline: FormValues): string[] {
  const keys = new Set([...Object.keys(baseline), ...Object.keys(current)]);
  const changed: string[] = [];
  for (const key of keys) {
    if (stableValue(current[key]) !== stableValue(baseline[key])) changed.push(key);
  }
  return changed.sort();
}
