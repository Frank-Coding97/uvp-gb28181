import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { MASK_AXES, MIRROR_OPTIONS, CONFIG_GROUPS, findConfigGroup, type ConfigTextItem } from "./deviceConfigGroups";
import {
  buildDeviceConfigBlocks,
  changedFieldKeys,
  formatDayTime,
  parseDayTime,
  readDeviceConfigPayload,
  type FormValues
} from "./deviceConfigPayload";

/** 取一组的下发块（校验失败时直接抛，免得断言落到一个空对象上）。 */
function blocksFor(groupKey: string, values: FormValues): Record<string, unknown> {
  const result = buildDeviceConfigBlocks(groupKey, values);
  expect(result.error, `${groupKey} 构建应通过，实际报错: ${result.error}`).toBe("");
  return result.blocks;
}

function errorFor(groupKey: string, values: FormValues): string {
  return buildDeviceConfigBlocks(groupKey, values).error;
}

/** 构建结果里"发出去了但你要知道"的那句说明（无话可说时是空串）。 */
function noticeFor(groupKey: string, values: FormValues): string {
  return buildDeviceConfigBlocks(groupKey, values).notice ?? "";
}

describe("deviceConfigPayload 表单键 ↔ 协议块翻译", () => {
  describe("OSDConfig", () => {
    it("timeType 选「不指定」时**整个键不出现**（不是发 0）", () => {
      const blocks = blocksFor("osd", {
        timeEnable: true,
        timeType: "",
        length: "1920",
        width: "1080",
        timeX: "10",
        timeY: "20",
        textEnable: true,
        items: []
      });
      const osd = blocks.osdConfig as Record<string, unknown>;
      // ⛔ 写成 timeType:0 会被对端当成"就要 YYYY-MM-DD HH:MM:SS"，
      //    与"设备自定格式"是两件不同的事。
      expect("timeType" in osd).toBe(false);
      expect(osd.timeEnable).toBe(1);
      expect(osd.textEnable).toBe(1);
      // 计数不独立存：SumNum 由后端按数组长度现算
      expect("SumNum" in osd).toBe(false);
    });

    it("文本按**字符**数限长（32 个汉字合法，33 个拒发）", () => {
      const base: FormValues = {
        timeEnable: true,
        timeType: "",
        length: "1920",
        width: "1080",
        timeX: "0",
        timeY: "0",
        textEnable: true
      };
      const okText = "国".repeat(32);
      expect(errorFor("osd", { ...base, items: [{ text: okText, x: 0, y: 0 }] })).toBe("");
      const badText = "国".repeat(33);
      expect(errorFor("osd", { ...base, items: [{ text: badText, x: 0, y: 0 }] })).toContain("32");
    });

    it("最多 8 条文本，第 9 条拒发", () => {
      const items: ConfigTextItem[] = Array.from({ length: 9 }, (_, index) => ({
        text: `第${index}条`,
        x: 0,
        y: 0
      }));
      const error = errorFor("osd", {
        timeEnable: true,
        timeType: "",
        length: "1920",
        width: "1080",
        timeX: "0",
        timeY: "0",
        textEnable: true,
        items
      });
      expect(error).toContain("8");
    });

    it("窗口尺寸必须为正", () => {
      expect(
        errorFor("osd", {
          timeEnable: true,
          timeType: "",
          length: "0",
          width: "1080",
          timeX: "0",
          timeY: "0",
          textEnable: true,
          items: []
        })
      ).toContain("窗口长度");
    });
  });

  describe("FrameMirror + PictureMask", () => {
    it("一组覆盖两个 ConfigType，两块一起出去", () => {
      // ⛔ 这里必须带一个区域：`maskOn:true` + 空区域是一个**设备不会执行**的组合，
      //    已在下面单独钉住（拒发）。本用例要测的是"两块打包"，与区域空不空无关。
      const blocks = blocksFor("picture", { mirror: "2", maskOn: true, mask1: [10, 20, 30, 40] });
      expect(Object.keys(blocks).sort()).toEqual(["frameMirror", "pictureMask"]);
      // ⛔ 2 = 上下镜像。1/2 写反的后果是"下发上下翻转、画面左右翻"，
      //    而回读对账比的是值、显示"一致"，只能靠人眼发现。
      expect((blocks.frameMirror as { value: number }).value).toBe(2);
      expect((blocks.pictureMask as { on: number }).on).toBe(1);
    });

    it("启用遮挡却没画任何区域 ⇒ **照发**（真机实测设备接受），只把「看不到遮挡」说出来", () => {
      // ⛔ 这里原来是一句"本地拒发"（`return { error }`）。2026-09-19 真机实测推翻了它：
      //    发 `<On>1</On><SumNum>0</SumNum>`（不带 `RegionList`），设备回读 `On=1`，
      //    且**不会凭空创建区域** ⇒ "只启用"是合法且可执行的请求。
      //    协议里也不存在独立的"启用信令"——`On` 只是 `PictureMask` 里的一个字段，
      //    启用只能搭这条 DeviceConfig 发出去。拦住它，操作员只会看到"点了什么都不发生"，
      //    而归因不到"是本地拦的"（2026-09-19 现场为这件事来回问了三轮）。
      const values = { mirror: "0", maskOn: true, mask1: [0, 0, 0, 0] };
      const blocks = blocksFor("picture", values);
      expect((blocks.pictureMask as { on: number }).on).toBe(1);
      expect((blocks.pictureMask as { regions: unknown[] }).regions).toEqual([]);
      // ⛔ 放行 ≠ 沉默：本次没带区域 ⇒ 画面上不会有可见遮挡，必须就地说明。
      //    少了这句，用户下一句必然是"为什么启用了没效果"。
      expect(noticeFor("picture", values)).toContain("没有携带任何遮挡区域");
    });

    it("关掉总闸后的空区域是**允许**的，且不该有提示 —— 那是正常的「移除遮挡」形态", () => {
      // 与上一条互为边界：空区域本身没问题，`On` 才是"要不要遮"的那个开关。
      const values = { mirror: "0", maskOn: false, mask1: [0, 0, 0, 0] };
      const blocks = blocksFor("picture", values);
      expect((blocks.pictureMask as { on: number }).on).toBe(0);
      expect((blocks.pictureMask as { regions: unknown[] }).regions).toEqual([]);
      // 停用是干净路径（后端会把区域铺成零面积删掉），没有任何要提醒的。
      expect(noticeFor("picture", values)).toBe("");
    });

    it("镜像值域是封闭 enumeration（0~3），越界拒发而不是静默夹取", () => {
      expect(errorFor("picture", { mirror: "7", maskOn: false })).toContain("0~3");
    });

    it("四个坐标全 0 的槽位**不发区域**，其余槽位保留自己的 Seq（不重排）", () => {
      const blocks = blocksFor("picture", {
        mirror: "0",
        maskOn: true,
        mask1: [0, 0, 0, 0],
        mask2: [10, 20, 30, 40],
        mask3: [0, 0, 0, 0],
        mask4: [100, 200, 300, 400]
      });
      const regions = (blocks.pictureMask as { regions: Array<Record<string, number>> }).regions;
      // 全零槽位在**意图层**不带：这里是"用户给了哪几块区域"，不是"报文长什么样"。
      // ⛔ 它**不代表**"设备会保留那一槽" —— 报文层由后端把 `Seq 1..4` 补成全量声明，
      //    省略的槽位会被补一条零面积让设备**删掉**（后端 `pictureMaskFullRegions`，
      //    2026-09-20 真机定因：省略 Seq ≠ 删除，那正是"遮挡区删不掉"的根因）。
      expect(regions).toHaveLength(2);
      // ⛔ Seq 跟着**槽位**走，不做 1..N 重排：设备用 Seq 标"这是我的第几个区域"，
      //    重排会让"设备回 seq=4、平台写回 seq=1" —— 区域在设备上被搬了位置，
      //    而回读对账比的是值，会显示成"设备没照做"。Seq 允许有空洞。
      expect(regions[0]).toEqual({ seq: 2, left: 10, top: 20, right: 30, bottom: 40 });
      expect(regions[1]).toEqual({ seq: 4, left: 100, top: 200, right: 300, bottom: 400 });
    });

    it("坐标倒置报错，不自动交换", () => {
      // 自动纠正会让下发值与回读值不一致，而对账只能看到"设备没照做"
      expect(errorFor("picture", { mirror: "0", maskOn: true, mask1: [50, 60, 20, 30] })).toContain("倒置");
    });

    it("遮挡坐标是**两个角点**，轴名不是 X/Y/W/H", () => {
      expect(MASK_AXES).toEqual(["左X", "左Y", "右X", "右Y"]);
      const group = findConfigGroup("picture")!;
      const coords = group.fields.filter(field => field.kind === "coords");
      expect(coords).toHaveLength(4);
      for (const field of coords) expect(field.axes).toEqual(MASK_AXES);
    });
  });

  describe("VideoRecordPlan", () => {
    it("时段保留到秒：HH:MM 补 :00，HH:MM:SS 原样带走", () => {
      const blocks = blocksFor("record-plan", {
        recordEnable: true,
        streamNumber: "0",
        schedules: [
          { weekDayNum: 1, segments: [{ start: "08:00", stop: "12:30:45" }] },
          { weekDayNum: 7, segments: [{ start: "00:00:00", stop: "23:59:59" }] }
        ]
      });
      const plan = blocks.videoRecordPlan as {
        schedules: Array<{ weekDayNum: number; segments: Array<Record<string, number>> }>;
      };
      expect(plan.schedules[0]!.segments[0]).toEqual({
        startHour: 8,
        startMin: 0,
        startSec: 0,
        stopHour: 12,
        stopMin: 30,
        stopSec: 45
      });
      // ⛔ 秒必须原样保留：界面只让填到分钟，就等于每次下发把设备上原有的秒清零 ——
      //    静默改写，而且回读对账还会显示"一致"。
      expect(plan.schedules[1]!.segments[0]!.stopSec).toBe(59);
    });

    it("没有时段的那天**整条不发**（不是发一条空 segments）", () => {
      const blocks = blocksFor("record-plan", {
        recordEnable: true,
        streamNumber: "0",
        schedules: [
          { weekDayNum: 3, segments: [] },
          { weekDayNum: 4, segments: [{ start: "09:00", stop: "10:00" }] }
        ]
      });
      const plan = blocks.videoRecordPlan as { schedules: Array<{ weekDayNum: number }> };
      expect(plan.schedules.map(day => day.weekDayNum)).toEqual([4]);
    });

    it("星期重复 / 越界 / 时段格式错 一律拒发", () => {
      const base = { recordEnable: true, streamNumber: "0" };
      expect(
        errorFor("record-plan", {
          ...base,
          schedules: [
            { weekDayNum: 2, segments: [{ start: "09:00", stop: "10:00" }] },
            { weekDayNum: 2, segments: [{ start: "11:00", stop: "12:00" }] }
          ]
        })
      ).toContain("重复");
      expect(
        errorFor("record-plan", {
          ...base,
          schedules: [{ weekDayNum: 8, segments: [{ start: "09:00", stop: "10:00" }] }]
        })
      ).toContain("1~7");
      expect(
        errorFor("record-plan", {
          ...base,
          schedules: [{ weekDayNum: 1, segments: [{ start: "25:00", stop: "26:00" }] }]
        })
      ).toContain("时刻格式");
    });

    it("counts 由后端现算：下发块里没有 SumNum", () => {
      const blocks = blocksFor("record-plan", {
        recordEnable: true,
        streamNumber: "1",
        schedules: [{ weekDayNum: 1, segments: [{ start: "08:00", stop: "12:00" }] }]
      });
      const plan = blocks.videoRecordPlan as Record<string, unknown>;
      expect("RecordScheduleSumNum" in plan).toBe(false);
      expect("TimeSegmentSumNum" in plan).toBe(false);
      expect(plan.streamNumber).toBe(1);
    });
  });

  describe("VideoAlarmRecord", () => {
    it("留空的延时项**不下发该元素**，填 0 则真的发 0", () => {
      const empty = blocksFor("alarm-record", {
        recordEnable: true,
        streamNumber: "0",
        recordTime: "",
        preRecordTime: ""
      });
      const record = empty.videoAlarmRecord as Record<string, unknown>;
      // nil 与"平台写了 0 秒"是两件事：0 秒在业务上是"报警即停录"
      expect("recordTime" in record).toBe(false);
      expect("preRecordTime" in record).toBe(false);

      const zero = blocksFor("alarm-record", {
        recordEnable: true,
        streamNumber: "0",
        recordTime: "0",
        preRecordTime: "30"
      });
      const zeroRecord = zero.videoAlarmRecord as Record<string, unknown>;
      expect(zeroRecord.recordTime).toBe(0);
      expect(zeroRecord.preRecordTime).toBe(30);
    });

    it("标准里没有 triggerEvent 这个字段（那是私有口径）", () => {
      const group = findConfigGroup("alarm-record")!;
      expect(group.fields.map(field => field.key)).not.toContain("triggerEvent");
      // 标准里必选的是 StreamNumber
      expect(group.fields.map(field => field.key)).toContain("streamNumber");
    });

    it("越界拒发", () => {
      expect(
        errorFor("alarm-record", { recordEnable: true, streamNumber: "0", recordTime: "86401", preRecordTime: "" })
      ).toContain("录像延时");
    });
  });

  describe("basicParam", () => {
    it("四项全空时拒发（空下发没有可落库的内容）", () => {
      expect(errorFor("basic", { name: "", expiration: "", heartBeatInterval: "", heartBeatCount: "" })).toContain("全空");
    });

    it("只填名字时块里只有 name，其余键缺席", () => {
      const blocks = blocksFor("basic", { name: "东门球机", expiration: "", heartBeatInterval: "", heartBeatCount: "" });
      expect(blocks.basicParam).toEqual({ name: "东门球机" });
    });

    it("纯空白的名字被当成没填（不拿空格去撞后端校验）", () => {
      expect(errorFor("basic", { name: "   ", expiration: "", heartBeatInterval: "", heartBeatCount: "" })).toContain("全空");
    });

    it("心跳超时次数上限 1000", () => {
      expect(errorFor("basic", { name: "", expiration: "", heartBeatInterval: "", heartBeatCount: "1001" })).toContain("1000");
    });
  });

  describe("AlarmReport", () => {
    it("字段名就是标准原文的 MotionDetection / FieldDetection", () => {
      const group = findConfigGroup("alarm-report")!;
      expect(group.fields.map(field => field.key)).toEqual(["motionDetection", "fieldDetection"]);
    });

    it("下发的是 **0/1 整数**（后端是 int，传 bool 会反序列化失败）", () => {
      const blocks = blocksFor("alarm-report", { motionDetection: true, fieldDetection: false });
      expect(blocks.alarmReport).toEqual({ motionDetection: 1, fieldDetection: 0 });
      expect(typeof (blocks.alarmReport as { motionDetection: unknown }).motionDetection).toBe("number");
    });
  });

  describe("读取：协议块 → 表单值", () => {
    it("单块 payload 直接读顶层字段（不套 ConfigType 键）", () => {
      const values = readDeviceConfigPayload("OSDConfig", {
        length: 1920,
        width: 1080,
        timeX: 10,
        timeY: 20,
        timeEnable: 1,
        textEnable: 0,
        items: [{ text: "厂区东门", x: 5, y: 6 }]
      });
      // 滑杆承载的字段统一输出字符串，否则"刚回读完"就会被脏值统计算成"已改"
      expect(values.length).toBe("1920");
      expect(values.timeEnable).toBe(true);
      expect(values.textEnable).toBe(false);
      // timeType 缺席 ⇒ 空串 ⇒ 下次下发不发送该元素（不能补成 "0"）
      expect(values.timeType).toBe("");
      expect(values.items).toEqual([{ text: "厂区东门", x: 5, y: 6 }]);
    });

    it("遮挡区按 Seq 归位，不是按数组下标", () => {
      const values = readDeviceConfigPayload("PictureMask", {
        on: 1,
        regions: [{ seq: 3, left: 1, top: 2, right: 3, bottom: 4 }]
      });
      expect(values.mask1).toEqual([0, 0, 0, 0]);
      // 只回了 Seq=3 时，按下标归位会把区域错画成第 1 个
      expect(values.mask3).toEqual([1, 2, 3, 4]);
      expect(values.maskOn).toBe(true);
    });

    it("录像计划的时段读回来是 HH:MM:SS（秒不丢）", () => {
      const values = readDeviceConfigPayload("VideoRecordPlan", {
        recordEnable: 1,
        streamNumber: 1,
        schedules: [
          { weekDayNum: 2, segments: [{ startHour: 8, startMin: 5, startSec: 30, stopHour: 9, stopMin: 0, stopSec: 0 }] }
        ]
      });
      expect(values.schedules).toEqual([{ weekDayNum: 2, segments: [{ start: "08:05:30", stop: "09:00:00" }] }]);
    });

    it("读→写是恒等映射：秒与可选元素都不会被改写", () => {
      const payload = {
        recordEnable: 1,
        streamNumber: 0,
        schedules: [
          { weekDayNum: 5, segments: [{ startHour: 22, startMin: 30, startSec: 15, stopHour: 23, stopMin: 59, stopSec: 59 }] }
        ]
      };
      const values = readDeviceConfigPayload("VideoRecordPlan", payload);
      const blocks = blocksFor("record-plan", values);
      const plan = blocks.videoRecordPlan as { schedules: Array<{ segments: Array<Record<string, number>> }> };
      expect(plan.schedules[0]!.segments[0]).toEqual({
        startHour: 22,
        startMin: 30,
        startSec: 15,
        stopHour: 23,
        stopMin: 59,
        stopSec: 59
      });
    });

    it("可选数字项缺席时读成空串（不是 0）", () => {
      const values = readDeviceConfigPayload("VideoAlarmRecord", { recordEnable: 1, streamNumber: 0 });
      expect(values.recordTime).toBe("");
      expect(values.preRecordTime).toBe("");
      const basic = readDeviceConfigPayload("basicParam", { name: "X" });
      expect(basic.expiration).toBe("");
      expect(basic.heartBeatCount).toBe("");
    });
  });

  describe("脏值统计", () => {
    it("回读值原样回填时不算已改（数字/字符串形态差不能误判）", () => {
      const values = readDeviceConfigPayload("OSDConfig", { length: 1920, width: 1080, timeX: 0, timeY: 0 });
      expect(changedFieldKeys(values, { ...values })).toEqual([]);
    });

    it("只报真的改过的那几个字段", () => {
      const baseline = readDeviceConfigPayload("OSDConfig", { length: 1920, width: 1080, timeX: 0, timeY: 0 });
      const current = { ...baseline, width: "720" };
      expect(changedFieldKeys(current, baseline)).toEqual(["width"]);
    });

    it("嵌套结构（文本行/时段）改动能被发现", () => {
      const baseline: FormValues = { items: [{ text: "A", x: 0, y: 0 }] };
      expect(changedFieldKeys({ items: [{ text: "A", x: 0, y: 0 }] }, baseline)).toEqual([]);
      expect(changedFieldKeys({ items: [{ text: "B", x: 0, y: 0 }] }, baseline)).toEqual(["items"]);
    });
  });

  describe("分组与协议类型的对应", () => {
    it("每个 ready 分组都声明了 ConfigType；只有视频参数属性走独立通道", () => {
      for (const group of CONFIG_GROUPS) {
        if (group.state !== "ready") continue;
        if (group.key === "video-param") {
          expect(group.configTypes).toEqual([]);
          continue;
        }
        expect(group.configTypes.length).toBeGreaterThan(0);
      }
    });

    it("画面处理声明两个 ConfigType，其余 ready 分组各一个", () => {
      const counts = CONFIG_GROUPS.filter(group => group.configTypes.length).map(group => [group.key, group.configTypes.length]);
      expect(counts).toEqual([
        ["osd", 1],
        ["picture", 2],
        ["record-plan", 1],
        ["alarm-record", 1],
        ["basic", 1],
        ["alarm-report", 1]
      ]);
    });

    it("镜像选项人读串与取值一一对应且顺序正确", () => {
      expect(MIRROR_OPTIONS.map(option => option.value)).toEqual(["0", "1", "2", "3"]);
      expect(MIRROR_OPTIONS[1]!.label).toContain("水平");
      expect(MIRROR_OPTIONS[2]!.label).toContain("上下");
    });
  });

  /**
   * 契约锚点：前端 `configTypes` 是**字符串字面量**，后端 `ConfigTypeOrder` 是 Go 常量。
   * 两侧没有任何编译期关联，写错大小写（`basicParam` ≠ `BasicParam`）不会报编译错，
   * 只会在运行时换来一个 HTTP 400 —— 而 400 会被并集放大到**整个家族**读不了。
   *
   * ⛔ 不要改回「拿 CONFIG_GROUPS 和自己比」那种断言：那是恒真的，等于没测。
   *    这里直接读后端源码，让两侧**同源**：后端改常量名、前端改字面量，只要不一致就红。
   */
  describe("ConfigType 契约锚点（与后端 manscdp 同源）", () => {
    function backendManscdpDir(): string {
      const candidates = [
        resolve(process.cwd(), "../server/app/gb28181/manscdp"),
        resolve(process.cwd(), "server/app/gb28181/manscdp")
      ];
      const found = candidates.find(dir => existsSync(dir));
      expect(found, `未能定位后端 manscdp 目录，已尝试: ${candidates.join(" , ")}`).toBeTruthy();
      return found!;
    }

    it("每个 configType 都必须是后端已声明的常量值，且在 device-configs 通道受理范围内", () => {
      const dir = backendManscdpDir();
      const videoParamSource = readFileSync(resolve(dir, "video_param.go"), "utf8");
      const deviceConfigSource = readFileSync(resolve(dir, "device_config.go"), "utf8");

      // 常量名 → 值（定义处：video_param.go 的 const 块）
      const constantValues = new Map(
        [...videoParamSource.matchAll(/(ConfigType[A-Za-z]+)\s*=\s*"([^"]+)"/g)].map(match => [match[1]!, match[2]!])
      );
      expect(constantValues.size, "未能从 video_param.go 解析出任何 ConfigType 常量").toBeGreaterThan(0);

      // device-configs 通用通道受理的类型 = ConfigTypeOrder 列出的那几项
      const orderBlock = deviceConfigSource.match(/var ConfigTypeOrder = \[\]string\{([\s\S]*?)\}/);
      expect(orderBlock, "未能从 device_config.go 解析出 ConfigTypeOrder").toBeTruthy();
      const accepted = new Set(
        [...orderBlock![1]!.matchAll(/ConfigType[A-Za-z]+/g)].map(match => {
          const value = constantValues.get(match[0]!);
          expect(value, `ConfigTypeOrder 引用了未声明的常量 ${match[0]}`).toBeTruthy();
          return value!;
        })
      );
      expect(accepted.size).toBeGreaterThan(0);

      for (const group of CONFIG_GROUPS) {
        for (const configType of group.configTypes) {
          expect(
            accepted.has(configType),
            `分组「${group.key}」声明的 "${configType}" 不在后端 ConfigTypeOrder 里` +
              `（可用值: ${[...accepted].join(" / ")}）—— 大小写写错会在运行时变成 HTTP 400`
          ).toBe(true);
        }
      }
    });
  });

  describe("时刻解析", () => {
    it("接受 HH:MM 与 HH:MM:SS，拒绝越界与乱格式", () => {
      expect(parseDayTime("08:05")).toEqual({ hour: 8, min: 5, sec: 0 });
      expect(parseDayTime("8:5:3")).toEqual({ hour: 8, min: 5, sec: 3 });
      expect(parseDayTime("24:00")).toBeNull();
      expect(parseDayTime("08:60")).toBeNull();
      expect(parseDayTime("08-05")).toBeNull();
      expect(parseDayTime("")).toBeNull();
    });

    it("格式化恒定补到秒（保证 read→write 恒等）", () => {
      expect(formatDayTime(8, 5, 0)).toBe("08:05:00");
      expect(formatDayTime(23, 59, 59)).toBe("23:59:59");
    });
  });
});
