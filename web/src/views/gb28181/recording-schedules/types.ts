export interface SchedulePeriod {
  start: string;
  end: string;
  nextDay?: boolean;
}

export interface ScheduleDay {
  name: string;
  enabled: boolean;
  periods: SchedulePeriod[];
}

export interface RecordingSchedule {
  id: string;
  name: string;
  enabled: boolean;
  description: string;
  cycle: string;
  timeSummary: string;
  channelCount: number;
  updatedAt: string;
  current: string;
  nextChange: string;
  days: ScheduleDay[];
}

export interface ScheduleChannel {
  id: string;
  name: string;
  code: string;
  device: string;
  deviceCode: string;
  online: boolean;
  mode: "按计划" | "持续录像" | "关闭";
  plan: string;
  matched: "是" | "否" | "—";
  state: "录像中" | "等待设备上线" | "时段外" | "已关闭";
  error: string;
  next: string;
}
