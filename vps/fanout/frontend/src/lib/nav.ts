import {
  ArrowLeftRight,
  Database,
  LayoutGrid,
  Link2,
  Plug,
  Settings,
  type LucideIcon,
} from "@lucide/vue";

export interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
  group: "运行" | "资源" | "系统";
  badge?: "ready" | "candidates";
}

export const navItems: NavItem[] = [
  { to: "/", label: "总览", icon: LayoutGrid, group: "运行" },
  { to: "/exits", label: "出口", icon: ArrowLeftRight, group: "运行", badge: "ready" },
  { to: "/nodes", label: "节点池", icon: Database, group: "资源", badge: "candidates" },
  { to: "/subscription", label: "订阅", icon: Link2, group: "资源" },
  { to: "/access", label: "接入", icon: Plug, group: "资源" },
  { to: "/settings", label: "设置", icon: Settings, group: "系统" },
];

export const navGroups = ["运行", "资源", "系统"] as const;
