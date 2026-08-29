export const MEDIA_MENU_PARENT_PATH = "/media";

export type MediaMenuSectionTitleKey =
  | "media-section-cluster"
  | "media-section-monitoring"
  | "media-section-ingress"
  | "media-section-capabilities";

export interface MediaMenuSectionDefinition {
  key: "cluster" | "monitoring" | "ingress" | "capabilities";
  titleKey: MediaMenuSectionTitleKey;
  icon: string;
  paths: readonly string[];
}

export interface MediaMenuSection extends MediaMenuSectionDefinition {
  items: Menu.MenuOptions[];
}

export interface MediaMenuPresentation {
  sections: MediaMenuSection[];
  ungrouped: Menu.MenuOptions[];
}

export const MEDIA_MENU_SECTIONS: readonly MediaMenuSectionDefinition[] = [
  {
    key: "cluster",
    titleKey: "media-section-cluster",
    icon: "lucide:Server",
    paths: [
      "/gb28181/zlm/overview",
      "/gb28181/zlm/nodes",
      "/gb28181/zlm/scheduler",
      "/gb28181/zlm/scheduler/logs"
    ]
  },
  {
    key: "monitoring",
    titleKey: "media-section-monitoring",
    icon: "lucide:Activity",
    paths: [
      "/gb28181/zlm/runtime",
      "/gb28181/zlm/streams",
      "/gb28181/zlm/sessions"
    ]
  },
  {
    key: "ingress",
    titleKey: "media-section-ingress",
    icon: "lucide:RadioTower",
    paths: [
      "/gb28181/zlm/proxies",
      "/gb28181/zlm/ffmpeg-sources",
      "/gb28181/zlm/rtp-servers"
    ]
  },
  {
    key: "capabilities",
    titleKey: "media-section-capabilities",
    icon: "lucide:SlidersHorizontal",
    paths: [
      "/gb28181/cloud-recordings",
      "/gb28181/recording-schedules",
      "/gb28181/zlm/config"
    ]
  }
];

interface MediaMenuPlacement {
  sectionIndex: number;
  itemIndex: number;
}

const placementByPath = new Map<string, MediaMenuPlacement>();
MEDIA_MENU_SECTIONS.forEach((section, sectionIndex) => {
  section.paths.forEach((path, itemIndex) => placementByPath.set(path, { sectionIndex, itemIndex }));
});

function isVisibleMenuItem(item: Menu.MenuOptions) {
  return !item.meta.hide && (item.meta.type === 1 || item.meta.type === 2);
}

export function groupMediaMenuItems(routeTree: Menu.MenuOptions[]): MediaMenuPresentation {
  const sections = MEDIA_MENU_SECTIONS.map(section => ({ ...section, items: [] as Menu.MenuOptions[] }));
  const ungrouped: Menu.MenuOptions[] = [];

  for (const item of routeTree) {
    if (!isVisibleMenuItem(item)) continue;
    const placement = placementByPath.get(item.path);
    if (placement) sections[placement.sectionIndex].items.push(item);
    else ungrouped.push(item);
  }

  for (const section of sections) {
    section.items.sort((left, right) => {
      const leftOrder = placementByPath.get(left.path)?.itemIndex ?? Number.MAX_SAFE_INTEGER;
      const rightOrder = placementByPath.get(right.path)?.itemIndex ?? Number.MAX_SAFE_INTEGER;
      return leftOrder - rightOrder;
    });
  }

  return {
    sections: sections.filter(section => section.items.length > 0),
    ungrouped
  };
}
