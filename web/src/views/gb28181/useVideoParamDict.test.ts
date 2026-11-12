/**
 * 「视频参数属性」三张码表的**字典注入层** —— 单测 + 收敛防回归。
 *
 * 三件事：
 * 1. **协议值域锁定**：`*_LABEL_FALLBACK` 的值域由 GB/T 28181 固定，这里逐条钉死。
 *    ⚠️ 2026-10-05 起本仓**开发阶段字典数据直接补开发库**，不再维护 `seeds/*.jsonl`
 *    （见技能 `uvp-dict-rollout`）⇒ 原先"兜底 == 种子逐字比"这条锚点失效，
 *    改成"兜底 == 协议值域"的正向锁定：谁动兜底表必然红，必须显式改用例。
 * 2. **字典在 → 用字典**（现场改名界面跟着变）；字典空 → 回落兜底，界面不塌。
 * 3. **收敛**：编码格式值域此前在本仓被定义了三份（其中设备配置抽屉那份漏了 SVAC），
 *    现在三处共用一份字典；且对账判定已与字典文案解耦（见最后一条用例）。
 */
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { createPinia, setActivePinia } from "pinia";
import { ref } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useSystemStore } from "@/store/modules/system";
import { squash } from "@/test/source-assert";
import {
  BIT_RATE_TYPE_LABEL_FALLBACK,
  DICT_CODE_BIT_RATE_TYPE,
  DICT_CODE_VIDEO_FORMAT,
  DICT_CODE_VIDEO_RESOLUTION,
  RESOLUTION_LABEL_FALLBACK,
  VIDEO_FORMAT_LABEL_FALLBACK
} from "./videoParamCodec";
import {
  useBitRateTypeOptions,
  useVideoFormatOptions,
  useVideoParamLabels,
  useVideoResolutionOptions
} from "./useVideoParamDict";

const readSource = (relative: string) => readFileSync(resolve(process.cwd(), relative), "utf8");

describe("协议值域锁定（兜底常量）", () => {
  // ⛔ 这三张表的码值是**协议固定**的（GB/T 28181-2022 附录 G），字典只能改显示名。
  //    改动兜底必须显式改这三条用例 —— 这是防"顺手改掉一个值"的唯一闸门。
  //    ⚠️ 开发库里的字典项与这份兜底的一致性属**人工核对**（单测不连库）。
  it("video_format = 1 MPEG-4 / 2 H.264 / 3 SVAC / 4 3GP / 5 H.265", () => {
    expect(VIDEO_FORMAT_LABEL_FALLBACK).toEqual({
      "1": "MPEG-4",
      "2": "H.264",
      "3": "SVAC",
      "4": "3GP",
      "5": "H.265"
    });
  });

  it("video_resolution = 1 QCIF / 2 CIF / 3 4CIF / 4 D1 / 5 720P / 6 1080P", () => {
    expect(RESOLUTION_LABEL_FALLBACK).toEqual({
      "1": "QCIF",
      "2": "CIF",
      "3": "4CIF",
      "4": "D1",
      "5": "720P",
      "6": "1080P"
    });
  });

  it("bit_rate_type = 1 CBR / 2 VBR", () => {
    expect(BIT_RATE_TYPE_LABEL_FALLBACK).toEqual({ "1": "CBR", "2": "VBR" });
  });
});

describe("字典驱动", () => {
  beforeEach(() => {
    // `src/store/modules/system.ts` 的 `ref` 走构建期自动导入，单测环境要显式补上。
    vi.stubGlobal("ref", ref);
    setActivePinia(createPinia());
  });

  const seed = (entries: Array<{ code: string; list: unknown }>) => {
    useSystemStore().dict = entries as never;
  };
  const formatDict = (list: Array<Record<string, unknown>>) => [{ code: DICT_CODE_VIDEO_FORMAT, list }];

  it("字典为空 ⇒ 三个下拉回落兜底值域，且按协议码序", () => {
    seed([]);
    expect(useVideoFormatOptions().value).toEqual([
      { value: "1", label: "MPEG-4" },
      { value: "2", label: "H.264" },
      { value: "3", label: "SVAC" },
      { value: "4", label: "3GP" },
      { value: "5", label: "H.265" }
    ]);
    expect(useVideoResolutionOptions().value.map(option => option.value)).toEqual(["1", "2", "3", "4", "5", "6"]);
    expect(useBitRateTypeOptions().value).toEqual([
      { value: "1", label: "CBR" },
      { value: "2", label: "VBR" }
    ]);
  });

  it("字典在 ⇒ 用字典的文案与顺序（现场改名界面跟着变）", () => {
    seed(
      formatDict([
        { name: "H.265", value: "5", status: 1 },
        { name: "AVC", value: "2", status: 1 }
      ])
    );
    expect(useVideoFormatOptions().value).toEqual([
      { value: "5", label: "H.265" },
      { value: "2", label: "AVC" }
    ]);
  });

  it("分辨率字典在 ⇒ 下拉只出字典里给的那几档", () => {
    seed([{ code: DICT_CODE_VIDEO_RESOLUTION, list: [{ name: "1080P", value: "6", status: 1 }] }]);
    expect(useVideoResolutionOptions().value).toEqual([{ value: "6", label: "1080P" }]);
  });

  it("停用项不进下拉（字典管理页把它关掉即生效）", () => {
    seed(
      formatDict([
        { name: "H.264", value: "2", status: 1 },
        { name: "停用项", value: "3", status: 0 }
      ])
    );
    expect(useVideoFormatOptions().value).toEqual([{ value: "2", label: "H.264" }]);
  });

  it("翻译函数走字典；字典未覆盖的码值由兜底铺底", () => {
    seed(formatDict([{ name: "AVC", value: "2", status: 1 }]));
    const labels = useVideoParamLabels();
    expect(labels.videoFormatText("2")).toBe("AVC");
    // 字典里只给了 `2` ⇒ 其余码值仍是兜底（三个字典各自铺底）
    expect(labels.videoFormatText("5")).toBe("H.265");
    expect(labels.resolutionText("5")).toBe("720P");
    expect(labels.bitRateTypeText("2")).toBe("VBR");
  });

  it("三个字典互不串台（同 value 的 `1` 在三个 code 里含义不同）", () => {
    seed([
      { code: DICT_CODE_VIDEO_FORMAT, list: [{ name: "MPEG-4", value: "1", status: 1 }] },
      { code: DICT_CODE_BIT_RATE_TYPE, list: [{ name: "CBR", value: "1", status: 1 }] }
    ]);
    const labels = useVideoParamLabels();
    expect(labels.videoFormatText("1")).toBe("MPEG-4");
    expect(labels.bitRateTypeText("1")).toBe("CBR");
    // 分辨率字典没给 ⇒ 兜底
    expect(labels.resolutionText("1")).toBe("QCIF");
  });
});

describe("三处重复定义已收敛到字典", () => {
  const drawer = squash(readSource("src/views/gb28181/device-mgmt/DeviceConfigDrawer.vue"));
  const card = squash(readSource("src/views/gb28181/components/play-console/PictureVideoParamCard.vue"));
  const play = squash(readSource("src/views/gb28181/components/PlayConsoleLinked.vue"));

  // ⛔ "MPEG-4" 是编码格式表的指纹：哪个文件里还留着它，就是自己又写了一份值域。
  it("设备配置抽屉改走字典（此前自带 4 项、漏 SVAC）", () => {
    expect(drawer).not.toContain("MPEG-4");
    expect(drawer).toContain("useVideoFormatOptions()");
    expect(drawer).toContain("useVideoResolutionOptions()");
    expect(drawer).toContain("useBitRateTypeOptions()");
  });

  it("播放控制台卡片改走字典", () => {
    expect(card).not.toContain("MPEG-4");
    expect(card).toContain("useVideoFormatOptions()");
    expect(card).toContain("useVideoResolutionOptions()");
    expect(card).toContain("useBitRateTypeOptions()");
  });

  // ⭐ 本批最要紧的一条：字典只改展示名，**不能**成为对账判定的输入。
  it("对账判定认码值，不再从人读串反推", () => {
    expect(play).not.toContain("normalizeCodecToken(videoFormatText");
    expect(play).not.toContain("VIDEO_RESOLUTION_TIERS[");
    expect(play).toContain("videoFormatCodecToken(read.videoFormat)");
    expect(play).toContain("resolutionPixels(read.resolution)");
    expect(play).toContain("resolutionPixels(streamInfo.value.resolution)");
  });

  // ⛔ 边界：值域**校验**是白名单，不是字典。字典化会让"非法值"被翻译成合法文案。
  it("校验白名单没有被字典化", () => {
    const codec = squash(readSource("src/views/gb28181/videoParamCodec.ts"));
    expect(codec).toContain("exportfunctionisValidVideoFormat");
    expect(codec).toContain("exportfunctionisValidBitRateType");
    expect(codec).toContain("exportfunctionisValidResolutionCode");
    expect(codec).toContain("/^[1-5]$/");
  });
});
