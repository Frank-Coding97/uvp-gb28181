export type PreviewField = {
  key: string;
  label: string;
  kind: "select" | "number" | "switch" | "text";
  value: string | number | boolean;
  options?: string[];
  unit?: string;
  min?: number;
  max?: number;
};
export type PreviewGroup = { key: string; label: string; fields: PreviewField[] };
export type PreviewTab = { key: string; label: string; subtitle: string; groups: PreviewGroup[] };
const select = (key: string, label: string, options: string[]): PreviewField => ({
  key,
  label,
  kind: "select",
  value: options[0],
  options
});
const number = (key: string, label: string, value: number, unit = "", min = 0, max = 99999): PreviewField => ({
  key,
  label,
  kind: "number",
  value,
  unit,
  min,
  max
});
const toggle = (key: string, label: string, value = true): PreviewField => ({ key, label, kind: "switch", value });
const text = (key: string, label: string, value: string): PreviewField => ({ key, label, kind: "text", value });

// Visual review fixtures only. These values never enter a device API.
export const previewTabs: PreviewTab[] = [
  {
    key: "ptz",
    label: "云台控制",
    subtitle: "移动、定位与镜头",
    groups: [
      { key: "direction", label: "方向与镜头", fields: [number("speed", "云台速度", 5, "档", 1, 10)] },
      {
        key: "precise",
        label: "精准定位",
        fields: [
          number("pan", "水平角度", 180, "°", 0, 360),
          number("tilt", "垂直角度", 0, "°", -90, 90),
          number("zoom", "变倍", 1, "倍", 1, 32)
        ]
      },
      {
        key: "scan",
        label: "扫描与辅助",
        fields: [
          select("scanMode", "扫描模式", ["水平扫描", "自动扫描"]),
          number("scanSpeed", "扫描速度", 5, "档", 1, 10),
          toggle("wiper", "雨刷", false),
          toggle("light", "辅助灯", false),
          number("auxId", "辅助开关编号", 1, "", 1, 255),
          toggle("auxOn", "辅助开关", false)
        ]
      }
    ]
  },
  {
    key: "image",
    label: "画面设置",
    subtitle: "叠加、镜像与隐私遮挡",
    groups: [
      {
        key: "osd",
        label: "文字与时间",
        fields: [
          toggle("timeOn", "显示时间"),
          select("dateStyle", "时间格式", ["YYYY-MM-DD HH:mm:ss", "YYYY年MM月DD日 HH:mm:ss"]),
          toggle("textOn", "显示文字"),
          text("osdText", "叠加文字", "后置摄像头"),
          number("textX", "文字位置 X", 1450, "px", 0, 1920),
          number("textY", "文字位置 Y", 24, "px", 0, 1080)
        ]
      },
      {
        key: "mirror",
        label: "画面镜像",
        fields: [select("mirrorMode", "镜像方式", ["不镜像", "左右翻转", "上下翻转", "中心镜像"])]
      },
      {
        key: "mask",
        label: "隐私遮挡",
        fields: [
          toggle("maskOn", "启用遮挡"),
          select("maskRegion", "当前区域", ["区域 1", "区域 2", "区域 3", "区域 4"]),
          number("maskX", "左上角 X", 240, "px", 0, 1920),
          number("maskY", "左上角 Y", 180, "px", 0, 1080),
          number("maskRight", "右下角 X", 720, "px", 0, 1920),
          number("maskBottom", "右下角 Y", 480, "px", 0, 1080)
        ]
      }
    ]
  },
  {
    key: "encoding",
    label: "视频编码",
    subtitle: "按码流配置编码与传输质量",
    groups: [
      {
        key: "video",
        label: "编码参数",
        fields: [
          select("codec", "编码格式", ["H.264", "H.265", "SVAC"]),
          select("resolution", "分辨率", ["1920 × 1080", "1280 × 720", "704 × 576"]),
          number("fps", "帧率", 25, "fps", 1, 60),
          select("bitrateMode", "码率控制", ["VBR", "CBR"]),
          number("bitrate", "目标码率", 4096, "kb/s", 64, 16384),
          number("iframe", "关键帧间隔", 50, "帧", 1, 250)
        ]
      },
      { key: "range", label: "能力范围", fields: [select("rangeCodec", "查询编码", ["H.264", "H.265", "SVAC"])] },
      {
        key: "svac",
        label: "SVAC 配置",
        fields: [
          select("svacDirection", "配置对象", ["编码配置", "解码配置"]),
          toggle("svc", "可伸缩编码", false),
          toggle("roi", "感兴趣区域", false),
          select("roiLevel", "区域优先级", ["普通", "高"])
        ]
      }
    ]
  },
  {
    key: "record",
    label: "录像存储",
    subtitle: "设备录像、计划与存储介质",
    groups: [
      {
        key: "plan",
        label: "录像计划",
        fields: [
          toggle("planOn", "启用计划"),
          select("recordStream", "录像码流", ["主码流", "子码流 1"]),
          select("recordType", "录像类型", ["连续录像", "报警录像"])
        ]
      },
      {
        key: "alarmRecord",
        label: "报警录像",
        fields: [
          toggle("alarmRecordOn", "启用报警录像"),
          number("preRecord", "预录时长", 10, "秒", 0, 300),
          number("postRecord", "延录时长", 30, "秒", 0, 600)
        ]
      },
      { key: "storage", label: "存储管理", fields: [select("storageCard", "存储介质", ["SD 卡 1", "SD 卡 2"])] }
    ]
  },
  {
    key: "alarm",
    label: "报警控制",
    subtitle: "布撤防、复位与事件上报",
    groups: [
      {
        key: "guard",
        label: "布防与复位",
        fields: [
          select("guardTarget", "控制对象", ["当前通道", "整个设备"]),
          select("alarmType", "复位类型", ["全部报警", "移动侦测", "区域入侵"])
        ]
      },
      { key: "report", label: "报警上报", fields: [toggle("motion", "移动侦测上报"), toggle("intrusion", "区域入侵上报")] }
    ]
  },
  {
    key: "device",
    label: "设备维护",
    subtitle: "基础配置、抓拍与维护",
    groups: [
      {
        key: "basic",
        label: "基本参数",
        fields: [
          text("deviceName", "设备名称", "后置摄像头"),
          number("expires", "注册有效期", 3600, "秒", 60, 86400),
          number("heartbeat", "心跳间隔", 60, "秒", 1, 3600),
          number("missed", "心跳超时次数", 3, "次", 1, 100)
        ]
      },
      {
        key: "snapshot",
        label: "设备抓拍",
        fields: [number("snapCount", "抓拍张数", 1, "张", 1, 10), number("snapInterval", "抓拍间隔", 1, "秒", 1, 3600)]
      },
      { key: "upgrade", label: "固件升级", fields: [text("firmware", "固件地址", ""), text("firmwareVersion", "目标版本", "")] }
    ]
  },
  {
    key: "probe",
    label: "视频探针",
    subtitle: "链路状态与媒体质量",
    groups: [
      { key: "live", label: "实时状态", fields: [] },
      {
        key: "sample",
        label: "采样诊断",
        fields: [
          select("duration", "采样时长", ["10 秒", "30 秒", "60 秒"]),
          select("depth", "检测范围", ["完整检测", "视频码流", "音频码流"])
        ]
      }
    ]
  }
];
