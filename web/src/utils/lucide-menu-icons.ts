import type { Component } from "vue";
import {
  Activity,
  Blocks,
  BookOpen,
  Box,
  Building2,
  CalendarClock,
  Cctv,
  CodeXml,
  Database,
  FileText,
  Folder,
  Gauge,
  History,
  House,
  KeyRound,
  LayoutGrid,
  ListTodo,
  Menu,
  MonitorPlay,
  Network,
  Radio,
  RadioTower,
  Router,
  Server,
  Settings,
  Shield,
  SlidersHorizontal,
  Tags,
  UserCog,
  UserRound,
  Users,
  Video,
  Webcam,
  Workflow
} from "@lucide/vue";

export const LUCIDE_ICON_PREFIX = "lucide:";

export const lucideMenuIcons: Record<string, Component> = {
  Activity,
  Blocks,
  BookOpen,
  Box,
  Building2,
  CalendarClock,
  Cctv,
  CodeXml,
  Database,
  FileText,
  Folder,
  Gauge,
  History,
  House,
  KeyRound,
  LayoutGrid,
  ListTodo,
  Menu,
  MonitorPlay,
  Network,
  Radio,
  RadioTower,
  Router,
  Server,
  Settings,
  Shield,
  SlidersHorizontal,
  Tags,
  UserCog,
  UserRound,
  Users,
  Video,
  Webcam,
  Workflow
};

export const lucideMenuIconNames = Object.keys(lucideMenuIcons);

export const toLucideIconValue = (name: string) => `${LUCIDE_ICON_PREFIX}${name}`;

export const getLucideIconName = (value?: string) => {
  if (!value) return "";
  return value.startsWith(LUCIDE_ICON_PREFIX) ? value.slice(LUCIDE_ICON_PREFIX.length) : "";
};

export const getLucideIconComponent = (value?: string) => {
  const iconName = getLucideIconName(value);
  return iconName ? lucideMenuIcons[iconName] : undefined;
};
