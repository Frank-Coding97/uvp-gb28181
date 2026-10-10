import { describe, expect, it } from "vitest";
import {
  BIT_RATE_TYPE_LABEL_FALLBACK,
  RESOLUTION_LABEL_FALLBACK,
  VIDEO_FORMAT_LABEL_FALLBACK,
  bitRateTypeText,
  canEditVideoParams,
  frameRateText,
  isValidFrameRate,
  isValidResolutionCode,
  isValidVideoBitRate,
  normalizeCodecToken,
  parseStreamNumberList,
  pixelsOf,
  resolutionPixels,
  resolutionText,
  validateVideoParamItem,
  validateVideoParamItems,
  videoBitRateRequired,
  videoBitRateText,
  videoFormatCodecToken,
  videoParamEmptyText,
  videoFormatText,
  videoParamReconcileText,
  videoParamReconcileTone,
  type VideoParamCodecItem
} from "./videoParamCodec";

function item(patch: Partial<VideoParamCodecItem> = {}): VideoParamCodecItem {
  return {
    streamNumber: 0,
    videoFormat: "2",
    resolution: "5",
    frameRate: "25",
    bitRateType: "1",
    videoBitRate: "2048",
    ...patch
  };
}

describe("附录 G 码值 → 人读串", () => {
  it("映射视频编码格式的 1-5", () => {
    expect(videoFormatText("1")).toBe("MPEG-4");
    expect(videoFormatText("2")).toBe("H.264");
    expect(videoFormatText("3")).toBe("SVAC");
    expect(videoFormatText("4")).toBe("3GP");
    expect(videoFormatText("5")).toBe("H.265");
  });

  it("映射分辨率的六个码值，其余原样带出", () => {
    expect(resolutionText("1")).toBe("QCIF");
    expect(resolutionText("2")).toBe("CIF");
    expect(resolutionText("3")).toBe("4CIF");
    expect(resolutionText("4")).toBe("D1");
    expect(resolutionText("5")).toBe("720P");
    expect(resolutionText("6")).toBe("1080P");
    // ⛔ 设备回 `WxH` 是标准允许的形态，必须原样显示而不是"未知"
    expect(resolutionText("1920x1080")).toBe("1920x1080");
    // ⛔ 不认识的码值也要原样带出：排障时要能区分"设备给了怪值"与"我们没解析"
    expect(resolutionText("9")).toBe("9");
  });

  it("映射码率类型", () => {
    expect(bitRateTypeText("1")).toBe("CBR");
    expect(bitRateTypeText("2")).toBe("VBR");
  });

  it("帧率与码率带上单位", () => {
    expect(frameRateText("25")).toBe("25 fps");
    expect(videoBitRateText("2048")).toBe("2048 kb/s");
  });

  it("空值一律显示成未上报/未提供", () => {
    expect(videoFormatText("")).toBe("未上报");
    expect(resolutionText(null)).toBe("未上报");
    expect(bitRateTypeText(undefined)).toBe("未上报");
    expect(frameRateText("")).toBe("未上报");
    expect(videoBitRateText("")).toBe("未提供");
    expect(videoBitRateText(null)).toBe("未提供");
  });

  // ⛔ "缺席"与"值为 0"是两件事：VBR 下码率本就该缺席，而 0 是设备真的报了个 0。
  // 两者都渲染成 `0 kb/s` 会把"条件必选字段没给"这件事掩盖掉。
  it("码率缺席与码率 0 必须渲染成不同文本", () => {
    expect(videoBitRateText("0")).toBe("0 kb/s");
    expect(videoBitRateText(null)).not.toBe(videoBitRateText("0"));
  });
});

describe("分辨率合法性（码值或 WxH）", () => {
  it("接受六个码值", () => {
    for (const code of ["1", "2", "3", "4", "5", "6"]) {
      expect(isValidResolutionCode(code)).toBe(true);
    }
  });

  it("接受宽高形式", () => {
    expect(isValidResolutionCode("1920x1080")).toBe(true);
    expect(isValidResolutionCode("704x576")).toBe(true);
  });

  it("拒绝码值 0 / 7、0 开头的宽高、大写 X 与乘号分隔", () => {
    expect(isValidResolutionCode("0")).toBe(false);
    expect(isValidResolutionCode("7")).toBe(false);
    expect(isValidResolutionCode("01920x1080")).toBe(false);
    // ⛔ 目录上报用的是 `*`（CatalogNode.resolution），而本字段标准要求 `x`。
    // 这里刻意不兼容 `*`：两种写法混用会让"我们到底发了什么"不再可读。
    expect(isValidResolutionCode("1920*1080")).toBe(false);
    expect(isValidResolutionCode("1920X1080")).toBe(false);
    expect(isValidResolutionCode("1920x0")).toBe(false);
    expect(isValidResolutionCode("")).toBe(false);
  });
});

describe("帧率与码率的范围", () => {
  it("帧率 0-99，两端含", () => {
    expect(isValidFrameRate("0")).toBe(true);
    expect(isValidFrameRate("99")).toBe(true);
    expect(isValidFrameRate("100")).toBe(false);
    expect(isValidFrameRate("-1")).toBe(false);
    expect(isValidFrameRate("25.5")).toBe(false);
    expect(isValidFrameRate("abc")).toBe(false);
  });

  it("码率 0-100000，两端含", () => {
    expect(isValidVideoBitRate("0")).toBe(true);
    expect(isValidVideoBitRate("100000")).toBe(true);
    expect(isValidVideoBitRate("100001")).toBe(false);
    expect(isValidVideoBitRate("2048.5")).toBe(false);
  });
});

describe("VideoBitRate 的条件必选（仅 CBR）", () => {
  it("只有 CBR(1) 时必填", () => {
    expect(videoBitRateRequired("1")).toBe(true);
    expect(videoBitRateRequired("2")).toBe(false);
    expect(videoBitRateRequired("")).toBe(false);
  });

  it("CBR 缺码率报错", () => {
    expect(validateVideoParamItem(item({ bitRateType: "1", videoBitRate: "" }))).toContain("必须填码率");
    expect(validateVideoParamItem(item({ bitRateType: "1", videoBitRate: null }))).toContain("必须填码率");
  });

  // ⛔ VBR 带码率必须报错而不是静默丢弃：丢弃会让用户以为填了生效了，
  // 而报文里其实没有这个元素（后端同样拒发）。
  it("VBR 带码率报错，且说明该元素不会出现", () => {
    const failure = validateVideoParamItem(item({ bitRateType: "2", videoBitRate: "2048" }));
    expect(failure).toContain("VBR");
    expect(failure).toContain("不应填码率");
  });

  it("VBR 不带码率通过", () => {
    expect(validateVideoParamItem(item({ bitRateType: "2", videoBitRate: null }))).toBeNull();
    expect(validateVideoParamItem(item({ bitRateType: "2", videoBitRate: "" }))).toBeNull();
  });
});

describe("逐格校验的报错定位", () => {
  it("逐项报错并指明是哪个码流哪一格", () => {
    expect(validateVideoParamItem(item({ videoFormat: "9" }))).toContain("码流 0");
    expect(validateVideoParamItem(item({ videoFormat: "9" }))).toContain("视频编码格式");
    expect(validateVideoParamItem(item({ streamNumber: 2, resolution: "0" }))).toContain("码流 2");
    expect(validateVideoParamItem(item({ streamNumber: 2, resolution: "0" }))).toContain("分辨率");
    expect(validateVideoParamItem(item({ frameRate: "120" }))).toContain("帧率");
    expect(validateVideoParamItem(item({ bitRateType: "7" }))).toContain("码率类型");
  });

  it("整批校验拒绝重复码流编号", () => {
    expect(validateVideoParamItems([item({ streamNumber: 0 }), item({ streamNumber: 0 })])).toContain("重复");
    expect(validateVideoParamItems([item({ streamNumber: 0 }), item({ streamNumber: 1 })])).toBeNull();
  });

  it("整批校验拒绝负数或非整数码流编号", () => {
    expect(validateVideoParamItems([item({ streamNumber: -1 })])).toContain("不小于 0");
    expect(validateVideoParamItems([item({ streamNumber: 1.5 })])).toContain("不小于 0");
  });

  it("0 号主码流是合法的", () => {
    expect(validateVideoParamItems([item({ streamNumber: 0 })])).toBeNull();
  });
});

describe("目录 StreamNumberList", () => {
  it("解析多值列表并去重排序", () => {
    expect(parseStreamNumberList("0/1")).toEqual([0, 1]);
    expect(parseStreamNumberList("0/1/2")).toEqual([0, 1, 2]);
    expect(parseStreamNumberList("1/0/1")).toEqual([0, 1]);
    expect(parseStreamNumberList(" 0 / 1 ")).toEqual([0, 1]);
  });

  it("未上报时返回空数组（调用方据此退化成按已回读行渲染）", () => {
    expect(parseStreamNumberList("")).toEqual([]);
    expect(parseStreamNumberList(null)).toEqual([]);
    expect(parseStreamNumberList("abc")).toEqual([]);
  });
});

describe("四态文案与语气", () => {
  it("六种状态各有文案", () => {
    expect(videoParamReconcileText("never_read")).toContain("尚未读取");
    expect(videoParamReconcileText("pending")).toContain("正在读取");
    expect(videoParamReconcileText("read_ok")).toContain("已读取");
    expect(videoParamReconcileText("type_absent")).toContain("未返回此配置类型");
    expect(videoParamReconcileText("mismatch")).toContain("值未生效");
    expect(videoParamReconcileText("failed")).toContain("未响应");
  });

  // ⛔ mismatch 的文案绝不能出现"失败/错误"：设备已接受命令，只是没照做
  // （典型是能力边界，如下发 1080P 实际 720P），设备没做错。
  it("mismatch 不是失败", () => {
    const text = videoParamReconcileText("mismatch");
    expect(text).not.toContain("失败");
    expect(text).not.toContain("错误");
    expect(videoParamReconcileTone("mismatch")).toBe("warn");
  });

  it("语气分级：mismatch/type_absent 是黄色，failed 才是红色", () => {
    expect(videoParamReconcileTone("read_ok")).toBe("ok");
    expect(videoParamReconcileTone("type_absent")).toBe("warn");
    expect(videoParamReconcileTone("mismatch")).toBe("warn");
    expect(videoParamReconcileTone("failed")).toBe("error");
    expect(videoParamReconcileTone("pending")).toBe("busy");
    expect(videoParamReconcileTone("never_read")).toBe("idle");
  });

  // ⛔ 版本只用来选措辞，不改变任何判定
  it("生效版本只影响措辞，不影响状态本身", () => {
    const as2016 = videoParamReconcileText("type_absent", { registeredVersion: "2016" });
    const as2022 = videoParamReconcileText("type_absent", { registeredVersion: "2022" });
    expect(as2016).toContain("2016");
    expect(as2016).toContain("未返回此配置类型");
    expect(as2022).not.toContain("2016");
    expect(as2022).toContain("厂商未实现");
  });

  // ⛔ 这个值有 `default:2016`，设备从未声明时也会是 2016。因此措辞用
  // "平台按 2016 版处理"，不能用"设备声明了 2016" —— 后者会把排障带偏。
  it("生效版本缺失时按「未实现」措辞，不外推成 2016", () => {
    for (const version of ["", undefined, null, "unknown"]) {
      const text = videoParamReconcileText("type_absent", { registeredVersion: version });
      expect(text).not.toContain("2016");
      expect(text).toContain("厂商未实现");
    }
    expect(videoParamReconcileText("type_absent")).toContain("厂商未实现");
  });

  // ⛔ 版本只影响"没拿到答案"和"设备没给这个类型"的措辞，
  // read_ok / mismatch 是设备**实际回答**，与版本无关。
  it("回读有结论时版本不参与措辞", () => {
    expect(videoParamReconcileText("read_ok", { registeredVersion: "2016" })).toBe("已读取设备当前配置");
    expect(videoParamReconcileText("mismatch", { registeredVersion: "2016" })).toBe("设备已接受命令，但值未生效");
  });
});

describe("空表单占位文案", () => {
  // ⛔ 空列表既可能是"还没问过"（never_read，操作问题），
  // 也可能是"问了但设备没有这个配置类型"（type_absent，能力问题）。
  // 两者都显示成"尚未读取"会让人去反复点读取，而真正该做的是换设备/换配置方式。
  it("type_absent 的空列表不能说成尚未读取", () => {
    const text = videoParamEmptyText("type_absent");
    expect(text).toContain("未返回该配置类型");
    expect(text).not.toContain("尚未读取");
  });

  it("读取在飞时占位文案走加载态，优先于状态本身", () => {
    expect(videoParamEmptyText("never_read", { pending: true })).toContain("正在读取");
    expect(videoParamEmptyText("type_absent", { pending: true })).toContain("正在读取");
    expect(videoParamEmptyText("pending")).toContain("正在读取");
  });

  it("其余状态各有对应措辞", () => {
    expect(videoParamEmptyText("never_read")).toContain("尚未读取");
    expect(videoParamEmptyText("failed")).toContain("未取到");
    // read_ok / mismatch 却一行都没有：设备回了该类型但没给可用条目。
    expect(videoParamEmptyText("read_ok")).toContain("未返回该码流");
    expect(videoParamEmptyText("mismatch")).toContain("未返回该码流");
  });
});

describe("可编辑性", () => {
  it("手里没值就不让改（空表单上让用户猜数字没有意义）", () => {
    expect(canEditVideoParams("never_read", false)).toBe(false);
    expect(canEditVideoParams("type_absent", false)).toBe(false);
  });

  it("有一批读取在飞时不让改", () => {
    expect(canEditVideoParams("pending", true)).toBe(false);
  });

  // ⛔ 设备说不支持，不是"用户不许试"的理由：被误登记成 2016 的真 2022 设备，
  // 不试一次就永远用不了。页面要做的只是把结论说清楚。
  it("设备不支持/值未生效时只要手上有值就仍可改", () => {
    expect(canEditVideoParams("type_absent", true)).toBe(true);
    expect(canEditVideoParams("mismatch", true)).toBe(true);
    expect(canEditVideoParams("failed", true)).toBe(true);
    expect(canEditVideoParams("read_ok", true)).toBe(true);
  });
});

// ---- 字典注入（2026-10-05：三张码表的真源改为 sys_dict）----

describe("展示名走注入的查表", () => {
  it("传了查表就按查表翻译，字典改了界面必须跟着变", () => {
    expect(videoFormatText("2", { "2": "AVC（现场口径）" })).toBe("AVC（现场口径）");
    expect(bitRateTypeText("1", { "1": "定码率" })).toBe("定码率");
    expect(resolutionText("5", { "5": "高清 720P" })).toBe("高清 720P");
  });

  // ⛔ 三个函数的"认不出来"口径本来就不同，字典化不能把它们拉平：
  //    编码/码率类型 → `未知(N)`；分辨率 → 原样（`WxH` 是标准允许的形态）。
  it("字典未覆盖的码值仍各按各的口径兜底", () => {
    expect(videoFormatText("9", { "2": "H.264" })).toBe("未知(9)");
    expect(bitRateTypeText("9", {})).toBe("未知(9)");
    expect(resolutionText("1920x1080", {})).toBe("1920x1080");
    expect(resolutionText("9", {})).toBe("9");
  });

  it("不传查表时用与种子逐字对齐的兜底常量", () => {
    for (const [code, label] of Object.entries(VIDEO_FORMAT_LABEL_FALLBACK)) {
      expect(videoFormatText(code)).toBe(label);
    }
    for (const [code, label] of Object.entries(RESOLUTION_LABEL_FALLBACK)) {
      expect(resolutionText(code)).toBe(label);
    }
    for (const [code, label] of Object.entries(BIT_RATE_TYPE_LABEL_FALLBACK)) {
      expect(bitRateTypeText(code)).toBe(label);
    }
  });

  it("空值一律未上报，与查表无关", () => {
    expect(videoFormatText("", { "2": "X" })).toBe("未上报");
    expect(resolutionText(null, { "5": "X" })).toBe("未上报");
    expect(bitRateTypeText(undefined, { "1": "X" })).toBe("未上报");
  });
});

describe("对账用的码值换算（⛔ 与字典文案无关）", () => {
  it("编码格式按码值出归一 token，认不出就是空串", () => {
    expect(videoFormatCodecToken("1")).toBe("MPEG4");
    expect(videoFormatCodecToken("2")).toBe("H264");
    expect(videoFormatCodecToken("3")).toBe("SVAC");
    expect(videoFormatCodecToken("4")).toBe("3GP");
    expect(videoFormatCodecToken("5")).toBe("H265");
    // 非附录 G 码值 ⇒ 认不出来（调用方据此判"比不出来"，**不是**"不一致"）
    expect(videoFormatCodecToken("9")).toBe("");
    expect(videoFormatCodecToken("")).toBe("");
    expect(videoFormatCodecToken(null)).toBe("");
  });

  it("分辨率先认码值，再认 WxH 原文", () => {
    expect(resolutionPixels("5")).toEqual({ width: 1280, height: 720 });
    expect(resolutionPixels("4")).toEqual({ width: 720, height: 576 });
    expect(resolutionPixels("1920x1080")).toEqual({ width: 1920, height: 1080 });
    expect(resolutionPixels("1920×1080")).toEqual({ width: 1920, height: 1080 });
    expect(resolutionPixels("")).toBeNull();
    expect(resolutionPixels("未上报")).toBeNull();
  });

  it("归一与像素解析对脏输入不抛错", () => {
    expect(normalizeCodecToken("h.264")).toBe("H264");
    expect(normalizeCodecToken("H264")).toBe("H264");
    expect(normalizeCodecToken("未上报")).toBe("");
    expect(pixelsOf("1920x1080")).toEqual({ width: 1920, height: 1080 });
    expect(pixelsOf("abc")).toBeNull();
  });

  // ⭐ 本次改造的分水岭：**字典只改展示名，绝不能改判定输入**。
  // 改前是"先翻译成人读串、再从串反推"，现场把 `2` 的名字换个写法，对账就会静默失效。
  it("字典把展示名改掉，码值换算的结果一个都不许变", () => {
    const weird = { "2": "高清编码", "5": "1280×720", "1": "定码率" };
    expect(videoFormatText("2", weird)).toBe("高清编码");
    expect(resolutionText("5", weird)).toBe("1280×720");
    expect(bitRateTypeText("1", weird)).toBe("定码率");

    expect(videoFormatCodecToken("2")).toBe("H264");
    expect(resolutionPixels("5")).toEqual({ width: 1280, height: 720 });
  });
});
