import type { Component } from "vue";
import {
  Activity,
  BellRing,
  Blocks,
  BookOpen,
  Box,
  Building,
  Building2,
  CalendarClock,
  Cctv,
  Clapperboard,
  ClipboardList,
  Cloud,
  CodeXml,
  Database,
  FileText,
  FileClock,
  Folder,
  Gauge,
  GitBranch,
  History,
  House,
  KeyRound,
  LayoutDashboard,
  LayoutGrid,
  ListTodo,
  Menu,
  MonitorPlay,
  Network,
  Radio,
  RadioTower,
  Router,
  Server,
  ServerCog,
  Settings,
  Settings2,
  Shield,
  ShieldCheck,
  SlidersHorizontal,
  Tags,
  UserCog,
  UserRound,
  Users,
  UsersRound,
  Video,
  Waypoints,
  Webcam,
  Workflow
} from "@lucide/vue";

export const LUCIDE_ICON_PREFIX = "lucide:";

export const lucideMenuIcons: Record<string, Component> = {
  Activity,
  BellRing,
  Blocks,
  BookOpen,
  Box,
  Building,
  Building2,
  CalendarClock,
  Cctv,
  Clapperboard,
  ClipboardList,
  Cloud,
  CodeXml,
  Database,
  FileText,
  FileClock,
  Folder,
  Gauge,
  GitBranch,
  History,
  House,
  KeyRound,
  LayoutDashboard,
  LayoutGrid,
  ListTodo,
  Menu,
  MonitorPlay,
  Network,
  Radio,
  RadioTower,
  Router,
  Server,
  ServerCog,
  Settings,
  Settings2,
  Shield,
  ShieldCheck,
  SlidersHorizontal,
  Tags,
  UserCog,
  UserRound,
  Users,
  UsersRound,
  Video,
  Waypoints,
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
  if (!iconName) return undefined;
  return lucideMenuIcons[iconName] || lucideMenuIcons[Object.keys(lucideMenuIcons).find((name) => name.toLowerCase() === iconName.toLowerCase()) || ""];
};
