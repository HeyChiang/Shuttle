<script setup lang="ts">
// Shuttle 主界面：顶部 SFTP / 主页 / 搜索，主体在主页、终端、SFTP 之间切换。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Clipboard, Window } from "@wailsio/runtime";
import * as SSH from "../bindings/shuttle/sshservice";
import type { HostConfig } from "../bindings/shuttle/internal/sshcore/models";
import HostDialog from "./components/HostDialog.vue";
import ScriptRunner from "./components/ScriptRunner.vue";
import SftpView from "./components/SftpView.vue";
import SnippetDialog from "./components/SnippetDialog.vue";
import TerminalPane from "./components/TerminalPane.vue";
import {
  deleteSnippet,
  firstLine,
  snippets,
  upsertSnippet,
  type Snippet,
} from "./snippets";
import { highlightShell } from "./highlight";
import {
  activeId,
  blankHost,
  connect,
  detachFromGroup,
  disconnect,
  encodings,
  hosts,
  loadHosts,
  mergeIntoPane,
  newSessionId,
  persistHosts,
  sessionLabel,
  sessions,
  splitGroup,
  splitGroupOf,
  splitSync,
  startEventBridge,
  swapTabs,
  tabs,
  type SessionTab,
  type SplitDirection,
  type SplitNode,
} from "./store";
import {
  TERM_FONT_MAX,
  TERM_FONT_MIN,
  resetAppearance,
  termFontSize,
  themeMode,
  type ThemeMode,
} from "./theme";

const query = ref("");
const dialogOpen = ref(false);
const editing = ref<HostConfig | null>(null);
const isMax = ref(false);
/** 当前页面：主页 / 代码块 / SFTP / 终端 */
const page = ref<"home" | "snippets" | "sftp" | "term">("home");
/** 顶栏搜索面板 */
const searchOpen = ref(false);
const searchInput = ref<HTMLInputElement>();
/** 顶栏已打开终端列表浮层 */
const sessionMenuOpen = ref(false);
/** 左侧栏设置入口 */
const settingsOpen = ref(false);
const settingsMaskEl = ref<HTMLElement>();
/** 设置左栏选中项：导入、导出与主题、字号 */
const settingsTab = ref<"import" | "export" | "theme" | "font">("import");
/** 「外观」二级菜单是否展开 */
const appearanceOpen = ref(true);
/** 主题选项：跟随系统 / 浅色 / 深色 */
const themeOptions: { value: ThemeMode; label: string }[] = [
  { value: "system", label: "跟随系统" },
  { value: "light", label: "浅色" },
  { value: "dark", label: "深色" },
];

/** 打开设置弹窗后聚焦遮罩，让 Esc 可以直接关闭 */
watch(settingsOpen, async (open) => {
  if (!open) return;
  await nextTick();
  settingsMaskEl.value?.focus();
});
const sessionQuery = ref("");
const sessionInput = ref<HTMLInputElement>();
/** 顶栏会话标签是否放不下（放不下的部分收进「更多」按钮） */
const tabsOverflow = ref(false);
/** 顶栏平铺展示的标签数量，超出部分收进「更多」浮层 */
const visibleTabCount = ref(Number.MAX_SAFE_INTEGER);
const titlebarEl = ref<HTMLElement>();
const sessionsEl = ref<HTMLElement>();
const dragEl = ref<HTMLElement>();
/** 隐藏的等宽副本，用于测量每个标签的自然宽度 */
const tabsGhostEl = ref<HTMLElement>();
/** 「更多」按钮，用于测量它自身占用的宽度 */
const moreBtnEl = ref<HTMLElement>();
let titlebarObserver: ResizeObserver | undefined;

/** 标签间距，需与 .tb-tabs / .tb-sessions 的 gap 保持一致 */
const TAB_GAP = 4;
/** 「更多」按钮未渲染时的宽度估算值 */
const MORE_FALLBACK_WIDTH = 74;

/** 当前活动标签：包含当前聚焦会话的标签（合并组整体算一个标签） */
const activeTab = computed(
  () =>
    tabs.value.find((t) => t.sessions.some((s) => s.id === activeId.value)) ?? tabs.value[0],
);

/** 平铺展示的标签：当前活动标签若被收起，会占用最后一个平铺位，保证始终可见 */
const visibleTabs = computed(() => {
  const list = tabs.value;
  if (!tabsOverflow.value) return list.slice(0, visibleTabCount.value);
  const shown = list.slice(0, visibleTabCount.value);
  const active = activeTab.value;
  if (!active || shown.some((t) => t.key === active.key)) return shown;
  return shown.length ? [...shown.slice(0, -1), active] : [active];
});
/** 收进「更多」浮层的会话（含合并组里的会话） */
const hiddenSessions = computed(() => {
  const shown = new Set(visibleTabs.value.flatMap((t) => t.sessions.map((s) => s.id)));
  return sessions.value.filter((s) => !shown.has(s.id));
});

/** 标签主标题：合并组显示为「工作组」，数量由角标显示 */
function tabLabel(t: SessionTab): string {
  return t.group ? "工作组" : sessionLabel(t.sessions[0]);
}

/** 标签悬浮提示：列出组内全部服务器 */
function tabTitle(t: SessionTab): string {
  return t.sessions
    .map((s) => `${sessionLabel(s)} · ${s.user}@${s.host}:${s.port}`)
    .join("\n");
}

/** 标签状态点：优先显示最严重的状态 */
function tabStatus(t: SessionTab): string {
  const statuses = t.sessions.map((s) => s.status);
  if (statuses.includes("error")) return "error";
  if (statuses.includes("connecting")) return "connecting";
  if (statuses.every((s) => s === "connected")) return "connected";
  return "closed";
}

/** 拖拽来源：顶部标签，或分屏里的某个终端面板 */
interface DragState {
  kind: "tab" | "pane";
  tab?: SessionTab;
  paneId?: string;
  startX: number;
  startY: number;
  moved: boolean;
}

/** 终端容器，用于把面板位置换算成预览用的百分比坐标 */
const terminalsEl = ref<HTMLElement>();
/** 拖拽状态：不用响应式，只在指针事件里读写 */
let dragState: DragState | null = null;
/** 正在拖拽的标签 key，用于标签的拖动样式 */
const dragTabKey = ref<string | null>(null);
/** 松手会落在哪个标签上：交换位置 */
const dropTabKey = ref<string | null>(null);
/** 松手会落在哪个终端面板上：分屏 */
const dropPlan = ref<{ targetId: string; direction: SplitDirection; before: boolean } | null>(null);
/** 拖拽面板标题栏时是否停在顶部标签栏上方（脱组） */
const paneDropActive = ref(false);
/** 拖拽真正开始后，压住紧随其后的 click */
let suppressClick = false;

/** 指针按下开始拖拽；移动超过阈值才算真正拖起来 */
function beginDrag(e: PointerEvent, init: Pick<DragState, "kind" | "tab" | "paneId">) {
  if (e.button !== 0) return;
  endDrag();
  dragState = { ...init, startX: e.clientX, startY: e.clientY, moved: false };
  dragTabKey.value = init.kind === "tab" ? (init.tab?.key ?? null) : null;
  suppressClick = false;
  window.addEventListener("pointermove", onDragMove);
  window.addEventListener("pointerup", onDragUp);
  window.addEventListener("pointercancel", endDrag);
}

/** 拖动顶部标签：落在另一个标签上交换位置，落在终端面板上分屏 */
function onTabPointerDown(e: PointerEvent, t: SessionTab) {
  if ((e.target as HTMLElement).closest(".x")) return;
  beginDrag(e, { kind: "tab", tab: t });
}

/** 拖动分屏面板标题栏：落在顶部标签栏即脱离工作组 */
function onPanePointerDown(id: string, e: PointerEvent) {
  beginDrag(e, { kind: "pane", paneId: id });
}

function onDragMove(e: PointerEvent) {
  const st = dragState;
  if (!st) return;
  if (!st.moved) {
    if (Math.hypot(e.clientX - st.startX, e.clientY - st.startY) < 4) return;
    st.moved = true;
    document.body.style.userSelect = "none";
  }
  e.preventDefault();
  const hit = document.elementFromPoint(e.clientX, e.clientY) as HTMLElement | null;

  if (st.kind === "pane") {
    // 整个标题栏都是脱组落点，避免只在标签那一小块才生效
    paneDropActive.value = !!hit?.closest(".titlebar");
    return;
  }

  const tabEl = hit?.closest("[data-tab-key]") as HTMLElement | null;
  if (tabEl && tabEl.dataset.tabKey !== st.tab?.key) {
    dropTabKey.value = tabEl.dataset.tabKey ?? null;
    dropPlan.value = null;
    splitPreview.value = null;
    return;
  }
  dropTabKey.value = null;

  const paneEl = hit?.closest("[data-pane-id]") as HTMLElement | null;
  const targetId = paneEl?.dataset.paneId;
  if (!paneEl || !targetId) {
    dropPlan.value = null;
    splitPreview.value = null;
    return;
  }
  const geo = dropSplitAt(e.clientX, e.clientY, paneEl);
  dropPlan.value = { targetId, ...geo };
  splitPreview.value = { ...geo, rect: previewRect(paneEl, geo) };
}

/** 终端面板区域（相对终端容器的百分比坐标） */
interface PaneRect {
  left: number;
  top: number;
  width: number;
  height: number;
}

/** 拖入终端区域时的分屏落点预览 */
const splitPreview = ref<{
  direction: SplitDirection;
  before: boolean;
  rect: PaneRect;
} | null>(null);

/** 根据落点在目标面板内的位置判断分屏方向：靠近左右 => 左右分屏，靠近上下 => 上下分屏 */
function dropSplitAt(x: number, y: number, el: HTMLElement) {
  const rect = el.getBoundingClientRect();
  const fx = (x - rect.left) / Math.max(rect.width, 1);
  const fy = (y - rect.top) / Math.max(rect.height, 1);
  const horizontal = Math.abs(fx - 0.5) >= Math.abs(fy - 0.5);
  return horizontal
    ? { direction: "row" as const, before: fx < 0.5 }
    : { direction: "column" as const, before: fy < 0.5 };
}

/** 目标面板里将要分出去的那一半，换算成相对终端容器的百分比 */
function previewRect(
  pane: HTMLElement,
  geo: { direction: SplitDirection; before: boolean },
): PaneRect {
  const full = terminalsEl.value?.getBoundingClientRect();
  if (!full || !full.width || !full.height) {
    return { left: 0, top: 0, width: 0, height: 0 };
  }
  const box = pane.getBoundingClientRect();
  const left = ((box.left - full.left) / full.width) * 100;
  const top = ((box.top - full.top) / full.height) * 100;
  const width = (box.width / full.width) * 100;
  const height = (box.height / full.height) * 100;
  // 预览只覆盖目标面板的一半，让用户清楚新终端会插到哪里
  return geo.direction === "row"
    ? { left: geo.before ? left : left + width / 2, top, width: width / 2, height }
    : { left, top: geo.before ? top : top + height / 2, width, height: height / 2 };
}

/** 结束拖拽并清空全部落点状态 */
function endDrag() {
  dragState = null;
  dragTabKey.value = null;
  dropTabKey.value = null;
  dropPlan.value = null;
  paneDropActive.value = false;
  splitPreview.value = null;
  document.body.style.userSelect = "";
  window.removeEventListener("pointermove", onDragMove);
  window.removeEventListener("pointerup", onDragUp);
  window.removeEventListener("pointercancel", endDrag);
}

/** 松手：按当前落点执行交换标签 / 分屏 / 脱组 */
function onDragUp() {
  const st = dragState;
  if (!st) return;
  const tabKey = dropTabKey.value;
  const plan = dropPlan.value;
  const paneDrop = paneDropActive.value;
  const moved = st.moved;
  endDrag();
  if (!moved) return;
  suppressClick = true;

  if (st.kind === "pane") {
    if (!paneDrop || !st.paneId) return;
    detachFromGroup(st.paneId);
    activeId.value = st.paneId;
    page.value = "term";
    return;
  }
  const source = st.tab;
  if (!source) return;
  if (tabKey) {
    const target = tabs.value.find((t) => t.key === tabKey);
    if (target && target.key !== source.key) {
      swapTabs(
        source.sessions.map((s) => s.id),
        target.sessions.map((s) => s.id),
      );
    }
    return;
  }
  if (!plan) return;
  const merged = mergeIntoPane(
    source.sessions.map((s) => s.id),
    plan.targetId,
    plan.direction,
    plan.before,
  );
  if (merged.length > 1) {
    activeId.value = plan.targetId;
    page.value = "term";
  }
}

/** 点击标签：拖拽刚结束时吞掉紧随其后的 click */
function onTabClick(t: SessionTab) {
  if (suppressClick) {
    suppressClick = false;
    return;
  }
  openTab(t);
}

/** 落点预览的定位样式 */
const previewStyle = computed(() => {
  const p = splitPreview.value;
  if (!p) return {};
  return {
    left: `${p.rect.left}%`,
    top: `${p.rect.top}%`,
    width: `${p.rect.width}%`,
    height: `${p.rect.height}%`,
  };
});

/** 点击标签：打开对应终端（合并组打开第一个面板） */
function openTab(t: SessionTab) {
  activeId.value = t.sessions[0].id;
  page.value = "term";
}

/** 关闭标签：合并组会断开组内全部会话 */
function closeTab(t: SessionTab) {
  t.sessions.forEach((s) => closeSession(s.id));
}

/** 当前标签内是否显示该终端面板 */
function isPaneVisible(id: string): boolean {
  return activeTab.value?.sessions.some((s) => s.id === id) ?? false;
}

/** 拆分当前合并标签，恢复为各自独立的终端 */
function splitActiveTab() {
  const t = activeTab.value;
  if (!t || !t.group) return;
  splitGroup(t.sessions[0].id);
  activeId.value = t.sessions[0].id;
}

/** 递归平分布局树，得到每个终端面板的百分比区域 */
function collectRects(node: SplitNode, rect: PaneRect, out: Map<string, PaneRect>) {
  if (node.type === "leaf") {
    out.set(node.id, rect);
    return;
  }
  const n = node.children.length || 1;
  node.children.forEach((child, i) => {
    if (node.direction === "row") {
      const width = rect.width / n;
      collectRects(
        child,
        { left: rect.left + width * i, top: rect.top, width, height: rect.height },
        out,
      );
    } else {
      const height = rect.height / n;
      collectRects(
        child,
        { left: rect.left, top: rect.top + height * i, width: rect.width, height },
        out,
      );
    }
  });
}

/** 当前工作组内各终端面板的位置：嵌套分屏只影响被拖入的那个面板 */
const paneRects = computed(() => {
  const out = new Map<string, PaneRect>();
  const t = activeTab.value;
  if (!t) return out;
  const group = splitGroupOf(t.sessions[0].id);
  if (!group) {
    out.set(t.sessions[0].id, { left: 0, top: 0, width: 100, height: 100 });
    return out;
  }
  collectRects(group.layout, { left: 0, top: 0, width: 100, height: 100 }, out);
  return out;
});

/** 单个终端面板的定位样式，不在当前标签内的面板隐藏 */
function paneStyle(id: string): Record<string, string> {
  const rect = paneRects.value.get(id);
  if (!rect) return { display: "none" };
  return {
    left: `${rect.left}%`,
    top: `${rect.top}%`,
    width: `${rect.width}%`,
    height: `${rect.height}%`,
  };
}

/** 当前标签是否处于合并分屏状态 */
const mergedLayout = computed(() => (activeTab.value?.sessions.length ?? 1) > 1);

/** 工作组成员信息，用于工具栏悬浮提示 */
const groupMembersTitle = computed(() =>
  (activeTab.value?.sessions ?? [])
    .map((s) => `${sessionLabel(s)} · ${s.user}@${s.host}:${s.port}`)
    .join("\n"),
);

/** 计算能放下多少个标签，剩余部分收进「更多」按钮 */
function updateTabsOverflow() {
  const ghost = tabsGhostEl.value;
  if (!ghost) return;
  const widths = Array.from(ghost.children).map((el) => (el as HTMLElement).offsetWidth);
  if (!widths.length) {
    tabsOverflow.value = false;
    visibleTabCount.value = Number.MAX_SAFE_INTEGER;
    return;
  }
  // 可用宽度 = 当前会话区已占宽度 + 可被挤占的拖拽区宽度（保留 20px 最小拖拽区）
  const available =
    (sessionsEl.value?.offsetWidth ?? 0) + (dragEl.value?.offsetWidth ?? 0) - 20;
  const natural = widths.reduce((sum, w, i) => sum + w + (i ? TAB_GAP : 0), 0);
  const wasOverflow = tabsOverflow.value;

  if (natural <= available) {
    tabsOverflow.value = false;
    visibleTabCount.value = Number.MAX_SAFE_INTEGER;
    sessionMenuOpen.value = false;
    return;
  }

  // 逐个累加标签宽度；当前活动标签在收起列表里时，最后一个平铺位留给它
  const activeIdx = tabs.value.findIndex((t) =>
    t.sessions.some((s) => s.id === activeId.value),
  );
  const moreWidth = moreBtnEl.value?.offsetWidth ?? MORE_FALLBACK_WIDTH;
  let prefix = 0;
  let count = 0;
  for (let k = 1; k <= widths.length; k++) {
    // 前 k 个标签的总宽度（当前会话尚未进入前 k 个时，末位换成它的宽度）
    const total = activeIdx >= k ? prefix + widths[activeIdx] : prefix + widths[k - 1];
    const used = total + (k - 1) * TAB_GAP + TAB_GAP + moreWidth;
    if (used > available) break;
    count = k;
    prefix += widths[k - 1];
  }
  // 空间极窄时也至少保留当前打开的会话
  if (!count && activeIdx >= 0) count = 1;
  tabsOverflow.value = true;
  if (visibleTabCount.value !== count) {
    visibleTabCount.value = count;
    sessionMenuOpen.value = false;
  }
  // 「更多」按钮刚由不显示变为显示，等它渲染后按真实宽度再校正一次
  if (!wasOverflow) void nextTick(updateTabsOverflow);
}

/** 标签合并、增删或重命名后重新评估标签是否放得下 */
watch(
  () =>
    tabs.value
      .map((t) => `${t.key}:${t.sessions.map((s) => sessionLabel(s)).join("/")}`)
      .join("|"),
  () => void nextTick(updateTabsOverflow),
);

/** 切换当前终端后，需要重新分配平铺位置以保证它始终可见 */
watch(activeId, () => void nextTick(updateTabsOverflow));

/** 页面切换会增减顶栏按钮，可用宽度随之变化 */
watch(page, () => void nextTick(updateTabsOverflow));

/** 会话在远端 exit 后自动关闭，若已无终端则回到主页 */
watch(
  () => sessions.value.length,
  (count) => {
    if (!count && page.value === "term") page.value = "home";
  },
);

/** 点击页面空白处收起浮层 */
function closePopups() {
  hostMenu.value = null;
  sessionMenuOpen.value = false;
  settingsOpen.value = false;
  if (!searchOpen.value) return;
  searchOpen.value = false;
  // 顶栏搜索与主页搜索共用 query，退出顶栏搜索时清空，避免主页被残留过滤
  if (page.value !== "home") query.value = "";
}

function toggleSearch() {
  searchOpen.value = !searchOpen.value;
}

/** 会话列表浮层开关，与服务器搜索浮层互斥 */
function toggleSessionMenu() {
  searchOpen.value = false;
  sessionMenuOpen.value = !sessionMenuOpen.value;
}

watch(sessionMenuOpen, (open) => {
  if (open) void nextTick(() => sessionInput.value?.focus());
  else sessionQuery.value = "";
});

/** 已打开终端按关键词过滤：备注名、IP、用户名 */
const filteredSessions = computed(() => {
  const q = sessionQuery.value.trim().toLowerCase();
  if (!q) return sessions.value;
  return sessions.value.filter((s) =>
    [sessionLabel(s), s.host, s.user].some((v) => (v || "").toLowerCase().includes(q)),
  );
});

/** 从会话列表跳到对应终端 */
function pickSession(id: string) {
  sessionMenuOpen.value = false;
  openSession(id);
}

function pickHost(h: HostConfig) {
  searchOpen.value = false;
  query.value = "";
  connectHost(h);
}

/** 顶栏搜索框回车：打开列表第一条 */
function openFirstMatch() {
  const first = filteredHosts.value[0];
  if (first) pickHost(first);
}

watch(searchOpen, (open) => {
  if (open) void nextTick(() => searchInput.value?.focus());
});

/** 依据名称生成稳定的头像底色 */
const AVATAR_COLORS = ["#3ddc84", "#5aa9ff", "#c678dd", "#ffb454", "#56d4dd", "#ff7b8a"];
function avatarColor(name: string): string {
  let h = 0;
  const text = name || "?";
  for (let i = 0; i < text.length; i += 1) h = (h * 31 + text.charCodeAt(i)) >>> 0;
  return AVATAR_COLORS[h % AVATAR_COLORS.length];
}

function initialOf(name: string): string {
  const t = (name || "?").trim();
  return t ? t[0].toUpperCase() : "?";
}

function authLabel(h: HostConfig): string {
  if (h.authMethod === "key") return "私钥";
  if (h.authMethod === "agent") return "SSH Agent";
  return "密码";
}

const filteredHosts = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) return hosts.value;
  return hosts.value.filter((h) =>
    [h.host, h.name, h.group].some((v) => (v || "").toLowerCase().includes(q)),
  );
});

/** 按分组聚合，保持原有顺序；分组名为空时归入「默认分组」 */
const groupedHosts = computed(() => {
  const groups = new Map<string, HostConfig[]>();
  for (const h of filteredHosts.value) {
    const key = h.group?.trim() || "默认分组";
    const list = groups.get(key);
    if (list) list.push(h);
    else groups.set(key, [h]);
  }
  return [...groups].map(([name, list]) => ({ name, hosts: list }));
});

/** 主页列表数据：按分组聚合 */
const homeSections = computed(() => groupedHosts.value.filter((g) => g.hosts.length));

async function refreshMax() {
  try {
    isMax.value = await Window.IsMaximised();
  } catch {
    /* 忽略 */
  }
}

function minimise() {
  void Window.Minimise();
}

async function toggleMax() {
  await Window.ToggleMaximise();
  await refreshMax();
}

function closeWin() {
  void Window.Close();
}

onMounted(async () => {
  startEventBridge();
  void refreshMax();
  // 窗口尺寸变化时重新判断顶栏会话标签是否放得下
  if (typeof ResizeObserver !== "undefined" && titlebarEl.value) {
    titlebarObserver = new ResizeObserver(() => updateTabsOverflow());
    titlebarObserver.observe(titlebarEl.value);
  }
  void nextTick(updateTabsOverflow);
  try {
    encodings.value = (await SSH.Encodings()) ?? encodings.value;
  } catch {
    /* 使用默认列表 */
  }
  await loadHosts();
});

onBeforeUnmount(() => {
  titlebarObserver?.disconnect();
  window.clearTimeout(noticeTimer);
});

function openNew() {
  editing.value = null;
  dialogOpen.value = true;
}

function openEdit(host: HostConfig) {
  editing.value = { ...host };
  dialogOpen.value = true;
}

async function submitHost(host: HostConfig) {
  const idx = hosts.value.findIndex((h) => h.id === host.id);
  const next = [...hosts.value];
  if (idx >= 0) next[idx] = host;
  else next.push(host);
  await persistHosts(next);
  dialogOpen.value = false;
  editing.value = null;
}

/** 待删除的连接档案：非空时展示自定义删除确认弹窗 */
const pendingDelete = ref<HostConfig | null>(null);
const confirmMaskEl = ref<HTMLElement>();

async function removeHost(host: HostConfig) {
  pendingDelete.value = host;
  await nextTick();
  confirmMaskEl.value?.focus();
}

async function confirmDelete() {
  const host = pendingDelete.value;
  if (!host) return;
  pendingDelete.value = null;
  await persistHosts(hosts.value.filter((h) => h.id !== host.id));
}

function duplicateHost(host: HostConfig) {
  const copy: HostConfig = {
    ...host,
    id: `${host.id}-copy-${Date.now().toString(36)}`,
    name: `${host.name || host.host} 副本`,
  };
  void persistHosts([...hosts.value, copy]);
}

// ---------------------------------------------------------------- 导入 / 导出

/** 导入、导出的结果提示，短暂显示在窗口底部 */
const notice = ref("");
let noticeTimer: number | undefined;

function showNotice(text: string) {
  notice.value = text;
  window.clearTimeout(noticeTimer);
  noticeTimer = window.setTimeout(() => {
    notice.value = "";
  }, 3600);
}

/** 导入 / 导出密码弹窗：仅在加密导出或导入加密文件时出现 */
const transferMode = ref<"import" | "export" | null>(null);
const transferPassword = ref("");
const transferConfirm = ref("");
const transferError = ref("");
const transferMaskEl = ref<HTMLElement>();

async function openTransfer(mode: "import" | "export") {
  transferMode.value = mode;
  transferPassword.value = "";
  transferConfirm.value = "";
  transferError.value = "";
  await nextTick();
  transferMaskEl.value?.focus();
}

function resetTransfer() {
  transferMode.value = null;
  transferPassword.value = "";
  transferConfirm.value = "";
  transferError.value = "";
}

/** 取消密码输入；加密导入还需让后端丢弃暂存的文件路径。 */
function closeTransfer() {
  if (transferMode.value === "import") void SSH.CancelImport();
  resetTransfer();
}

/** 直接导出明文 JSON；加密是可选项，只有主动点击加密导出才会输入密码。 */
async function askExportHosts() {
  if (!hosts.value.length) {
    showNotice("还没有可导出的主机");
    return;
  }
  try {
    const path = await SSH.ExportHosts("");
    if (path) showNotice(`已导出到 ${path}`);
  } catch (e) {
    showNotice(String(e));
  }
}

/** 加密导出：为文件中的凭据额外设置密码。 */
function askEncryptedExport() {
  if (!hosts.value.length) {
    showNotice("还没有可导出的主机");
    return;
  }
  void openTransfer("export");
}

/** 导入主机：明文文件直接导入，加密文件再询问密码。 */
async function askImportHosts() {
  try {
    await finishImport("");
  } catch (e) {
    // 后端已记住所选文件，输入密码后无需重新选择。
    if (String(e).includes("已加密")) {
      void openTransfer("import");
      return;
    }
    showNotice(String(e));
  }
}

function submitTransfer() {
  if (transferMode.value === "export") void exportHosts();
  else if (transferMode.value === "import") void submitImport();
}

async function exportHosts() {
  const password = transferPassword.value;
  if (!password) {
    transferError.value = "请输入加密密码";
    return;
  }
  if (password !== transferConfirm.value) {
    transferError.value = "两次输入的密码不一致";
    return;
  }
  try {
    const path = await SSH.ExportHosts(password);
    resetTransfer();
    if (path) showNotice(`已导出到 ${path}`);
  } catch (e) {
    transferError.value = String(e);
  }
}

async function submitImport() {
  if (!transferPassword.value) {
    transferError.value = "请输入文件密码";
    return;
  }
  try {
    await finishImport(transferPassword.value);
  } catch (e) {
    // 密码错误等失败保留弹窗，方便直接修改后重试。
    transferError.value = String(e);
  }
}

/** 导入主机：按 user@host:port 去重后追加到现有列表 */
async function finishImport(password: string) {
  const list = await SSH.ImportHosts(password);
  // 取消文件选择时后端返回 null，视为放弃导入。
  if (!list) {
    resetTransfer();
    return;
  }
  const exists = new Set(hosts.value.map((h) => `${h.user}@${h.host}:${h.port}`));
  const added: HostConfig[] = [];
  for (const item of list) {
    if (!item?.host) continue;
    const key = `${item.user}@${item.host}:${item.port}`;
    if (exists.has(key)) continue;
    exists.add(key);
    // 重新生成 id，避免与现有档案冲突。
    added.push({ ...blankHost(), ...item, id: newSessionId(), sessionId: "" });
  }
  if (!added.length) {
    resetTransfer();
    showNotice(list.length ? "导入完成：没有新增主机（均已存在）" : "文件里没有主机配置");
    return;
  }
  await persistHosts([...hosts.value, ...added]);
  resetTransfer();
  showNotice(`已导入 ${added.length} 台主机`);
}

// ---------------------------------------------------------------- 代码块

const snippetQuery = ref("");
const snippetDialogOpen = ref(false);
const editingSnippet = ref<Snippet | null>(null);

/** 代码块搜索：名称与内容都可匹配 */
const filteredSnippets = computed(() => {
  const q = snippetQuery.value.trim().toLowerCase();
  if (!q) return snippets.value;
  return snippets.value.filter((s) =>
    [s.name, s.code].some((v) => (v || "").toLowerCase().includes(q)),
  );
});

function openNewSnippet() {
  editingSnippet.value = null;
  snippetDialogOpen.value = true;
}

function openEditSnippet(snippet: Snippet) {
  editingSnippet.value = { ...snippet };
  snippetDialogOpen.value = true;
}

function submitSnippet(snippet: Snippet) {
  upsertSnippet(snippet);
  snippetDialogOpen.value = false;
  editingSnippet.value = null;
}

function copySnippet(snippet: Snippet) {
  void Clipboard.SetText(snippet.code);
}

/** 待删除的代码块：非空时展示自定义删除确认弹窗 */
const pendingSnippet = ref<Snippet | null>(null);
const snippetConfirmEl = ref<HTMLElement>();

async function askDeleteSnippet(snippet: Snippet) {
  pendingSnippet.value = snippet;
  await nextTick();
  snippetConfirmEl.value?.focus();
}

function confirmDeleteSnippet() {
  const snippet = pendingSnippet.value;
  pendingSnippet.value = null;
  if (snippet) deleteSnippet(snippet.id);
}

/** 代码行数，用于卡片摘要 */
function snippetLines(code: string): number {
  return code.replace(/\r\n?/g, "\n").split("\n").length;
}

function connectHost(host: HostConfig) {
  page.value = "term";
  void connect(host);
}

/** 主页卡片操作菜单：当前主机 + 相对视口的坐标 */
const hostMenu = ref<{ host: HostConfig; left: number; top: number } | null>(null);
const hostMenuEl = ref<HTMLElement>();
/** 菜单宽度，用于「更多」按钮右对齐与首次定位 */
const HOST_MENU_WIDTH = 178;

/** 显示菜单并贴边修正坐标，避免超出窗口 */
async function showHostMenu(host: HostConfig, left: number, top: number) {
  hostMenu.value = { host, left, top };
  await nextTick();
  const el = hostMenuEl.value;
  if (!el || !hostMenu.value) return;
  const rect = el.getBoundingClientRect();
  hostMenu.value.left = Math.max(8, Math.min(left, window.innerWidth - rect.width - 8));
  hostMenu.value.top = Math.max(8, Math.min(top, window.innerHeight - rect.height - 8));
}

/** 右键卡片：菜单跟随光标 */
function openHostMenuAtCursor(host: HostConfig, e: MouseEvent) {
  void showHostMenu(host, e.clientX + 2, e.clientY + 2);
}

/** 点击「更多」按钮：菜单右对齐展开在按钮下方 */
function openHostMenuFromButton(host: HostConfig, e: MouseEvent) {
  const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
  void showHostMenu(host, r.right - HOST_MENU_WIDTH, r.bottom + 6);
}

/** 执行菜单动作并收起菜单 */
function runHostAction(action: "connect" | "edit" | "duplicate" | "delete") {
  const host = hostMenu.value?.host;
  hostMenu.value = null;
  if (!host) return;
  if (action === "connect") connectHost(host);
  else if (action === "edit") openEdit(host);
  else if (action === "duplicate") duplicateHost(host);
  else void removeHost(host);
}

/** 点击顶部会话标签：切换焦点并回到终端视图 */
function openSession(id: string) {
  activeId.value = id;
  page.value = "term";
}

function closeSession(id: string) {
  const rest = sessions.value.filter((s) => s.id !== id);
  void disconnect(id);
  if (!rest.length) sessionMenuOpen.value = false;
  if (!rest.length && page.value === "term") page.value = "home";
}
</script>

<template>
  <div
    class="app"
    :class="{ 'no-rail': page === 'sftp' || page === 'term' }"
    @contextmenu.prevent="closePopups"
    @click="closePopups"
  >
    <!-- 第一行：品牌 + SFTP + 窗口控制 -->
    <header
      ref="titlebarEl"
      class="titlebar"
      :class="{ 'pane-drop': paneDropActive }"
    >
      <div class="tb-brand">
        <span class="logo">⌁</span>
        <strong>Shuttle</strong>
      </div>

      <button
        class="tb-btn"
        style="--wails-draggable: no-drag"
        :class="{ on: page === 'home' }"
        title="主页"
        @click="page = 'home'"
      >
        <svg viewBox="0 0 20 20">
          <path d="M3.2 9.6L10 4l6.8 5.6" />
          <path d="M5.4 8.8v6.6a1 1 0 0 0 1 1h7.2a1 1 0 0 0 1-1V8.8" />
        </svg>
        主页
      </button>

      <button
        class="tb-btn"
        style="--wails-draggable: no-drag"
        :class="{ on: page === 'sftp' }"
        :disabled="!hosts.length"
        :title="hosts.length ? '本地上传 / 远程下载 / 服务器互传' : '请先添加服务器'"
        @click="page = 'sftp'"
      >
        <svg viewBox="0 0 20 20">
          <path d="M10 3v10" />
          <path d="M6.2 7.4L10 3.6l3.8 3.8" />
          <path d="M4 13.4v1.8a1.4 1.4 0 0 0 1.4 1.4h9.2a1.4 1.4 0 0 0 1.4-1.4v-1.8" />
        </svg>
        SFTP
      </button>

      <!-- 已打开的服务器会话：能放下多少就平铺多少，放不下的收进「更多」按钮 + 搜索浮层 -->
      <div
        v-if="sessions.length"
        ref="sessionsEl"
        class="tb-sessions"
        style="--wails-draggable: no-drag"
      >
        <div v-if="visibleTabs.length" class="tb-tabs">
          <button
            v-for="t in visibleTabs"
            :key="t.key"
            class="tb-tab"
            :class="{
              on: page === 'term' && t.key === activeTab?.key,
              drop: dropTabKey === t.key,
              dragging: dragTabKey === t.key,
            }"
            :title="tabTitle(t)"
            :data-tab-key="t.key"
            @pointerdown="onTabPointerDown($event, t)"
            @click="onTabClick(t)"
          >
            <span class="dot" :class="tabStatus(t)" />
            <span v-if="t.group" class="tb-group" :title="`${t.sessions.length} 个终端并排显示`">
              {{ t.sessions.length }}
            </span>
            <span class="tb-tab-label">{{ tabLabel(t) }}</span>
            <span class="x" title="断开" @click.stop="closeTab(t)">×</span>
          </button>
        </div>

        <button
          v-if="tabsOverflow"
          ref="moreBtnEl"
          class="tb-btn tb-session-btn tb-more"
          :class="{ on: sessionMenuOpen }"
          :title="`还有 ${hiddenSessions.length} 个终端`"
          @click.stop="toggleSessionMenu"
        >
          <svg viewBox="0 0 20 20">
            <rect x="2.6" y="4" width="14.8" height="12" rx="1.8" />
            <path d="M5.8 8.2l2.3 2-2.3 2M10 12.4h4.2" />
          </svg>
          <span class="tb-count">{{ hiddenSessions.length }}</span>
        </button>

        <div v-if="tabsOverflow && sessionMenuOpen" class="search-pop session-pop" @click.stop>
          <div class="sp-input">
            <input
              ref="sessionInput"
              v-model="sessionQuery"
              placeholder="搜索已打开的终端"
              @keydown.esc="sessionMenuOpen = false"
            />
          </div>
          <div class="sp-list">
            <div
              v-for="s in filteredSessions"
              :key="s.id"
              class="sp-item sp-session"
              :class="{ on: page === 'term' && s.id === activeId }"
              :title="sessionLabel(s)"
              @click="pickSession(s.id)"
            >
              <span class="dot" :class="s.status" />
              <span class="sp-name">{{ sessionLabel(s) }}</span>
              <span class="x" title="断开" @click.stop="closeSession(s.id)">×</span>
            </div>
            <p v-if="!filteredSessions.length" class="sp-empty">
              {{ sessionQuery.trim() ? "没有匹配的终端" : "没有已打开的终端" }}
            </p>
          </div>
        </div>

        <!-- 隐藏副本：不参与布局，仅用于测量全部标签的自然宽度 -->
        <div ref="tabsGhostEl" class="tb-tabs tb-tabs-ghost" aria-hidden="true">
          <button v-for="t in tabs" :key="t.key" class="tb-tab" tabindex="-1">
            <span class="dot" :class="tabStatus(t)" />
            <span v-if="t.group" class="tb-group">{{ t.sessions.length }}</span>
            <span class="tb-tab-label">{{ tabLabel(t) }}</span>
            <span class="x">×</span>
          </button>
        </div>
      </div>

      <!-- 非主页时用「+」弹出服务器搜索 -->
      <div v-if="page !== 'home'" class="tb-search" style="--wails-draggable: no-drag">
        <button
          class="tb-btn tb-plus"
          :class="{ on: searchOpen }"
          title="搜索并连接服务器"
          @click.stop="toggleSearch"
        >
          <svg viewBox="0 0 20 20">
            <path d="M10 4.5v11M4.5 10h11" />
          </svg>
        </button>

        <div v-if="searchOpen" class="search-pop" @click.stop>
          <div class="sp-input">
            <input
              ref="searchInput"
              v-model="query"
              placeholder="搜索主机"
              @keydown.esc="searchOpen = false"
              @keydown.enter="openFirstMatch"
            />
          </div>
          <!-- 输入关键词搜索服务器，未输入时展示全部 -->
          <div class="sp-list">
            <button
              v-for="h in filteredHosts"
              :key="h.id"
              class="sp-item"
              @click="pickHost(h)"
            >
              <span class="sp-name">{{ h.name || h.host }}</span>
              <span class="sp-meta">{{ h.user }}@{{ h.host }}:{{ h.port }}</span>
            </button>
            <p v-if="!filteredHosts.length" class="sp-empty">
              {{ query.trim() ? "没有匹配的服务器" : "还没有保存的服务器" }}
            </p>
          </div>
        </div>
      </div>

      <div ref="dragEl" class="tb-drag" style="--wails-draggable: drag" />

      <!-- 终端页：代码块按钮固定在最右侧，点击后从右侧滑出抽屉 -->
      <ScriptRunner v-if="page === 'term'" />

      <div class="tb-controls" style="--wails-draggable: no-drag">
        <button class="win-btn" title="最小化" @click="minimise">
          <svg viewBox="0 0 10 10"><path d="M0 5h10" /></svg>
        </button>
        <button class="win-btn" :title="isMax ? '还原' : '最大化'" @click="toggleMax">
          <svg v-if="!isMax" viewBox="0 0 10 10">
            <rect x="0.5" y="0.5" width="9" height="9" fill="none" />
          </svg>
          <svg v-else viewBox="0 0 10 10">
            <rect x="0.5" y="2.5" width="7" height="7" fill="none" />
            <path d="M2.5 2.5V0.5h7v7h-2" fill="none" />
          </svg>
        </button>
        <button class="win-btn close" title="关闭" @click="closeWin">
          <svg viewBox="0 0 10 10"><path d="M0 0l10 10M10 0L0 10" /></svg>
        </button>
      </div>
    </header>

    <!-- 左侧导航列表：主页 / 代码块页展示，终端 / SFTP 页全屏时不占用空间 -->
    <aside
      v-if="page === 'home' || page === 'snippets'"
      class="rail"
      style="--wails-draggable: no-drag"
    >
      <nav class="rail-list">
        <button
          class="rail-item"
          :class="{ on: page === 'home' }"
          title="主机"
          @click="page = 'home'"
        >
          <svg viewBox="0 0 20 20">
            <rect x="3.2" y="3.6" width="13.6" height="5.4" rx="1.4" />
            <rect x="3.2" y="11" width="13.6" height="5.4" rx="1.4" />
            <path d="M6.2 6.3h2.2M6.2 13.7h2.2" />
          </svg>
          <span>主机</span>
        </button>

        <button
          class="rail-item"
          :class="{ on: page === 'snippets' }"
          title="代码块"
          @click="page = 'snippets'"
        >
          <svg viewBox="0 0 20 20">
            <path d="M7.4 5.4L3 10l4.4 4.6" />
            <path d="M12.6 5.4L17 10l-4.4 4.6" />
            <path d="M10.9 3.8l-1.8 12.4" />
          </svg>
          <span>代码块</span>
        </button>
      </nav>

      <div class="rail-foot">
        <button
          class="rail-item"
          :class="{ on: settingsOpen }"
          title="设置"
          @click.stop="settingsOpen = true"
        >
          <svg viewBox="0 0 20 20">
            <path
              d="M8.5 2.8h3l.4 1.8 1.6.9 1.7-.7 1.5 2.6-1.3 1.2v1.8l1.3 1.2-1.5 2.6-1.7-.7-1.6.9-.4 1.8h-3l-.4-1.8-1.6-.9-1.7.7-1.5-2.6 1.3-1.2V8.6L3.3 7.4l1.5-2.6 1.7.7 1.6-.9.4-1.8Z"
            />
            <circle cx="10" cy="10" r="2.6" />
          </svg>
          <span>设置</span>
        </button>
      </div>
    </aside>

    <!-- 设置弹窗：独立浮层页面 -->
    <Teleport to="body">
      <div
        v-if="settingsOpen"
        ref="settingsMaskEl"
        class="settings-mask"
        tabindex="-1"
        @click.self="settingsOpen = false"
        @keydown.esc="settingsOpen = false"
      >
        <div class="settings-dialog" role="dialog" aria-modal="true" aria-label="设置">
          <header class="settings-head">
            <strong>设置</strong>
            <button class="settings-close" title="关闭" @click="settingsOpen = false">×</button>
          </header>

          <div class="settings-main">
            <nav class="settings-nav">
              <button
                class="settings-nav-item"
                :class="{ on: settingsTab === 'import' }"
                @click="settingsTab = 'import'"
              >
                <svg viewBox="0 0 16 16">
                  <path d="M8 2.4v7.4M5.2 6.9L8 9.7l2.8-2.8" />
                  <path d="M3.2 11.6v1.4a1 1 0 0 0 1 1h7.6a1 1 0 0 0 1-1v-1.4" />
                </svg>
                <span>导入</span>
              </button>

              <button
                class="settings-nav-item"
                :class="{ on: settingsTab === 'export' }"
                @click="settingsTab = 'export'"
              >
                <svg viewBox="0 0 16 16">
                  <path d="M8 10.8V3.4M5.2 6.2L8 3.4l2.8 2.8" />
                  <path d="M3.2 11.6v1.4a1 1 0 0 0 1 1h7.6a1 1 0 0 0 1-1v-1.4" />
                </svg>
                <span>导出</span>
              </button>

              <button
                class="settings-nav-item"
                :class="{ on: settingsTab === 'theme' || settingsTab === 'font' }"
                @click="appearanceOpen = !appearanceOpen"
              >
                <svg viewBox="0 0 20 20">
                  <circle cx="10" cy="10" r="3.1" />
                  <path
                    d="M10 2.6v1.8M10 15.6v1.8M2.6 10h1.8M15.6 10h1.8M4.8 4.8l1.3 1.3M13.9 13.9l1.3 1.3M15.2 4.8l-1.3 1.3M6.1 13.9l-1.3 1.3"
                  />
                </svg>
                <span>外观</span>
                <span class="nav-caret" :class="{ open: appearanceOpen }">›</span>
              </button>

              <div v-show="appearanceOpen" class="settings-subnav">
                <button :class="{ on: settingsTab === 'theme' }" @click="settingsTab = 'theme'">
                  主题
                </button>
                <button :class="{ on: settingsTab === 'font' }" @click="settingsTab = 'font'">
                  终端文字大小
                </button>
              </div>
            </nav>

            <section class="settings-content">
              <template v-if="settingsTab === 'import'">
                <div class="set-group">
                  <span class="set-label">导入主机配置</span>
                  <p class="set-desc">
                    从 JSON 文件导入主机与登录凭据；若文件已加密，会提示输入导出时设置的密码。
                  </p>
                  <div class="set-btnrow">
                    <button class="btn primary" @click="askImportHosts">导入主机</button>
                  </div>
                </div>
              </template>

              <template v-else-if="settingsTab === 'export'">
                <div class="set-group">
                  <span class="set-label">导出主机配置</span>
                  <p class="set-desc">
                    导出的 JSON 包含主机与登录凭据，请妥善保管；如需保护凭据，可选用下方的加密导出。
                  </p>
                  <div class="set-btnrow">
                    <button class="btn primary" :disabled="!hosts.length" @click="askExportHosts">
                      导出主机
                    </button>
                  </div>
                  <button class="set-link" :disabled="!hosts.length" @click="askEncryptedExport">
                    加密导出（可选）
                  </button>
                </div>
              </template>

              <template v-else-if="settingsTab === 'theme'">
                <div class="set-group">
                  <span class="set-label">主题</span>
                  <div class="set-seg">
                    <button
                      v-for="opt in themeOptions"
                      :key="opt.value"
                      :class="{ on: themeMode === opt.value }"
                      @click="themeMode = opt.value"
                    >
                      {{ opt.label }}
                    </button>
                  </div>
                </div>
              </template>

              <template v-else>
                <div class="set-group">
                  <div class="set-row">
                    <span class="set-label">终端文字大小</span>
                    <span class="set-value">{{ termFontSize }} px</span>
                  </div>
                  <input
                    v-model.number="termFontSize"
                    class="set-range"
                    type="range"
                    :min="TERM_FONT_MIN"
                    :max="TERM_FONT_MAX"
                    step="1"
                  />
                  <span class="set-hint">仅影响 SSH 终端中的字符大小</span>
                </div>
              </template>
            </section>
          </div>

          <footer
            v-if="settingsTab === 'theme' || settingsTab === 'font'"
            class="settings-actions"
          >
            <span />
            <button class="btn" @click="resetAppearance">恢复默认</button>
          </footer>
        </div>
      </div>
    </Teleport>

    <!-- 第二行：搜索 + 新增（仅主页） -->
    <div v-if="page === 'home'" class="home-bar">
      <input v-model="query" placeholder="搜索主机：IP、备注名或分组" />
      <button class="btn primary new-btn" @click="openNew">新增主机</button>
    </div>

    <!-- 第二行：代码块搜索 + 新增（仅代码块页） -->
    <div v-if="page === 'snippets'" class="home-bar">
      <input v-model="snippetQuery" placeholder="搜索代码块：名称或代码内容" />
      <button class="btn primary new-btn" @click="openNewSnippet">新增代码块</button>
    </div>

    <!-- 第三行：服务器列表 / 终端 / SFTP -->
    <main class="content">
      <section v-show="page === 'home'" class="list-view">
        <div class="list-head">
          <div class="lh-text">
            <h2>主机</h2>
          </div>
        </div>

        <div v-if="homeSections.length" class="cards">
          <section v-for="g in homeSections" :key="g.name" class="group">
            <header class="group-head">
              <span class="group-name">{{ g.name }}</span>
              <span class="group-count">{{ g.hosts.length }} 台服务器</span>
            </header>

            <div class="group-cards">
              <article
                v-for="h in g.hosts"
                :key="h.id"
                class="card"
                @dblclick="connectHost(h)"
                @contextmenu.prevent.stop="openHostMenuAtCursor(h, $event)"
              >
                <div class="card-top">
                  <span class="avatar" :style="{ background: avatarColor(h.name || h.host) }">
                    {{ initialOf(h.name || h.host) }}
                  </span>
                  <div class="who">
                    <strong>{{ h.name || h.host }}</strong>
                    <small>{{ h.user }}@{{ h.host }}:{{ h.port }}</small>
                  </div>

                  <div class="more">
                    <button
                      class="more-btn"
                      title="更多操作"
                      @click.stop="openHostMenuFromButton(h, $event)"
                    >
                      <svg viewBox="0 0 16 16">
                        <circle cx="3.4" cy="8" r="1.3" />
                        <circle cx="8" cy="8" r="1.3" />
                        <circle cx="12.6" cy="8" r="1.3" />
                      </svg>
                    </button>
                  </div>
                </div>

                <div class="tags">
                  <span class="tag">{{ authLabel(h) }}</span>
                  <span class="tag">{{ h.encoding }}</span>
                </div>
              </article>
            </div>
          </section>
        </div>

        <div v-if="!hosts.length" class="empty-state">
          <h3>还没有保存主机连接</h3>
          <p>点击「新增主机」添加第一条 SSH 连接信息，或从已有配置文件导入。</p>
          <div class="empty-actions">
            <button class="btn primary" @click="openNew">新增主机</button>
            <button class="btn" @click="askImportHosts">从文件导入</button>
          </div>
        </div>
        <div v-else-if="!homeSections.length" class="empty-state">
          <h3>没有匹配的服务器</h3>
          <p>换个 IP 或备注名试试。</p>
        </div>
      </section>

      <section v-show="page === 'snippets'" class="list-view">
        <div class="list-head">
          <div class="lh-text">
            <h2>代码块</h2>
          </div>
        </div>

        <div v-if="filteredSnippets.length" class="cards">
          <article
            v-for="s in filteredSnippets"
            :key="s.id"
            class="card snippet-card"
            @dblclick="openEditSnippet(s)"
          >
            <div class="card-top">
              <span class="snippet-icon">
                <svg viewBox="0 0 20 20">
                  <path d="M7.4 5.4L3 10l4.4 4.6" />
                  <path d="M12.6 5.4L17 10l-4.4 4.6" />
                  <path d="M10.9 3.8l-1.8 12.4" />
                </svg>
              </span>
              <div class="who">
                <strong>{{ s.name }}</strong>
                <small>{{ snippetLines(s.code) }} 行 · {{ firstLine(s.code) }}</small>
              </div>

              <div class="more snippet-actions">
                <button class="more-btn" title="复制代码" @click.stop="copySnippet(s)">
                  <svg viewBox="0 0 16 16">
                    <rect x="5.6" y="5.6" width="8" height="8" rx="1.6" />
                    <path d="M10.4 3.2H4.2a1.6 1.6 0 0 0-1.6 1.6v6.2" />
                  </svg>
                </button>
                <button class="more-btn" title="编辑" @click.stop="openEditSnippet(s)">
                  <svg viewBox="0 0 16 16">
                    <path d="M11.3 2.7l2 2-7.4 7.4-2.7.7.7-2.7 7.4-7.4Z" />
                  </svg>
                </button>
                <button class="more-btn danger" title="删除" @click.stop="askDeleteSnippet(s)">
                  <svg viewBox="0 0 16 16">
                    <path
                      d="M2.8 4.4h10.4M6.4 4.4V2.9h3.2v1.5M4.4 4.4l.6 8.5a1 1 0 0 0 1 .9h3.9a1 1 0 0 0 1-.9l.6-8.5"
                    />
                  </svg>
                </button>
              </div>
            </div>

            <pre class="snippet-code"><code v-html="highlightShell(s.code)" /></pre>
          </article>
        </div>

        <div v-if="!snippets.length" class="empty-state">
          <h3>还没有代码块</h3>
          <p>把常用的巡检、部署脚本保存下来，在 SSH 终端里可一键引入执行。</p>
          <button class="btn primary" @click="openNewSnippet">新增代码块</button>
        </div>
        <div v-else-if="!filteredSnippets.length" class="empty-state">
          <h3>没有匹配的代码块</h3>
          <p>换个名称或代码关键词试试。</p>
        </div>
      </section>

      <section v-show="page === 'term'" class="term-view">
        <!-- 合并分屏工具栏：同步输入只影响当前合并组 -->
        <div v-if="mergedLayout" class="term-toolbar">
          <span class="tt-info" :title="groupMembersTitle">
            <svg viewBox="0 0 16 16" aria-hidden="true">
              <rect x="2" y="3" width="5" height="10" rx="1.2" />
              <rect x="9" y="3" width="5" height="10" rx="1.2" />
            </svg>
            工作组
            <span class="tt-count">{{ activeTab?.sessions.length }} 个终端</span>
          </span>
          <button
            class="tt-btn"
            :class="{ on: splitSync }"
            :title="splitSync ? '输入会同步到同组其他终端' : '输入只发送到当前终端'"
            @click="splitSync = !splitSync"
          >
            <svg viewBox="0 0 16 16" aria-hidden="true">
              <path d="M2.4 8h11.2" />
              <path d="M5.2 5.2L2.4 8l2.8 2.8" />
              <path d="M10.8 5.2L13.6 8l-2.8 2.8" />
            </svg>
            同步输入
          </button>
          <button class="tt-btn" title="把合并的终端拆回独立标签" @click="splitActiveTab">
            <svg viewBox="0 0 16 16" aria-hidden="true">
              <path d="M8 2.4v11.2" />
              <path d="M4 3l-2.4 2.4L4 7.8" />
              <path d="M12 3l2.4 2.4L12 7.8" />
            </svg>
            拆分窗口
          </button>
          <span class="tt-hint">拖动终端标题可拖回顶部标签栏</span>
        </div>
        <div ref="terminalsEl" class="terminals" :class="{ merged: mergedLayout }">
          <!-- 落点预览：只覆盖目标面板将要分出去的那一半 -->
          <div v-if="splitPreview" class="split-preview" :style="previewStyle">
            <span>{{ splitPreview.direction === "row" ? "左右分屏" : "上下分屏" }}</span>
          </div>
          <div
            v-for="s in sessions"
            :key="s.id"
            class="pane-slot"
            :data-pane-id="s.id"
            :style="paneStyle(s.id)"
          >
            <TerminalPane
              :session="s"
              :visible="page === 'term' && isPaneVisible(s.id)"
              :merged="mergedLayout"
              @activate="activeId = $event"
              @close="closeSession(s.id)"
              @pane-drag-start="onPanePointerDown"
            />
          </div>
        </div>
      </section>

      <SftpView v-if="page === 'sftp'" :hosts="hosts" />
    </main>

    <HostDialog
      v-if="dialogOpen"
      :host="editing"
      @submit="submitHost"
      @cancel="dialogOpen = false"
    />

    <SnippetDialog
      v-if="snippetDialogOpen"
      :snippet="editingSnippet"
      @submit="submitSnippet"
      @cancel="snippetDialogOpen = false"
    />

    <!-- 删除确认：自定义弹窗，替代浏览器原生 confirm -->
    <Teleport to="body">
      <div
        v-if="pendingDelete"
        ref="confirmMaskEl"
        class="confirm-mask"
        tabindex="-1"
        @click.self="pendingDelete = null"
        @keydown.esc="pendingDelete = null"
      >
        <div class="confirm-dialog" role="alertdialog" aria-modal="true">
          <div class="confirm-head">
            <span class="confirm-icon">
              <svg viewBox="0 0 16 16" aria-hidden="true">
                <path
                  d="M2.8 4.4h10.4M6.4 4.4V2.9h3.2v1.5M4.4 4.4l.6 8.5a1 1 0 0 0 1 .9h3.9a1 1 0 0 0 1-.9l.6-8.5"
                />
              </svg>
            </span>
            <div class="confirm-text">
              <h3>删除连接</h3>
              <p>
                确定要删除「<strong>{{ pendingDelete.name || pendingDelete.host }}</strong
                >」吗？
              </p>
            </div>
          </div>
          <div class="confirm-actions">
            <button class="btn" @click="pendingDelete = null">取消</button>
            <button class="btn danger" @click="confirmDelete">删除</button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 删除代码块确认 -->
    <Teleport to="body">
      <div
        v-if="pendingSnippet"
        ref="snippetConfirmEl"
        class="confirm-mask"
        tabindex="-1"
        @click.self="pendingSnippet = null"
        @keydown.esc="pendingSnippet = null"
      >
        <div class="confirm-dialog" role="alertdialog" aria-modal="true">
          <div class="confirm-head">
            <span class="confirm-icon">
              <svg viewBox="0 0 16 16" aria-hidden="true">
                <path
                  d="M2.8 4.4h10.4M6.4 4.4V2.9h3.2v1.5M4.4 4.4l.6 8.5a1 1 0 0 0 1 .9h3.9a1 1 0 0 0 1-.9l.6-8.5"
                />
              </svg>
            </span>
            <div class="confirm-text">
              <h3>删除代码块</h3>
              <p>
                确定要删除「<strong>{{ pendingSnippet.name }}</strong
                >」吗？
              </p>
            </div>
          </div>
          <div class="confirm-actions">
            <button class="btn" @click="pendingSnippet = null">取消</button>
            <button class="btn danger" @click="confirmDeleteSnippet">删除</button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 加密相关的密码输入：仅加密导出、或导入加密文件时出现 -->
    <Teleport to="body">
      <div
        v-if="transferMode"
        ref="transferMaskEl"
        class="confirm-mask"
        tabindex="-1"
        @click.self="closeTransfer"
        @keydown.esc="closeTransfer"
      >
        <div class="confirm-dialog pwd-dialog" role="dialog" aria-modal="true">
          <div class="confirm-head">
            <span class="confirm-icon warn">
              <svg viewBox="0 0 16 16" aria-hidden="true">
                <path d="M5 7V5.2a3 3 0 0 1 6 0V7M3.6 7h8.8v6.2H3.6z" />
              </svg>
            </span>
            <div class="confirm-text">
              <h3>{{ transferMode === "export" ? "加密导出" : "输入文件密码" }}</h3>
              <p v-if="transferMode === 'export'">
                设置一个密码，文件中的登录凭据会用它加密，导入时需要输入同一密码。
              </p>
              <p v-else>该文件已加密，请输入导出时设置的密码。</p>
            </div>
          </div>
          <div class="pwd-fields">
            <label>
              <span>{{ transferMode === "export" ? "加密密码" : "文件密码" }}</span>
              <input
                v-model="transferPassword"
                type="password"
                :placeholder="transferMode === 'export' ? '设置一个密码' : '输入文件密码'"
                @keydown.enter="submitTransfer"
              />
            </label>
            <label v-if="transferMode === 'export'">
              <span>确认密码</span>
              <input
                v-model="transferConfirm"
                type="password"
                placeholder="再次输入密码"
                @keydown.enter="submitTransfer"
              />
            </label>
            <p v-if="transferError" class="pwd-error">{{ transferError }}</p>
          </div>
          <div class="confirm-actions">
            <button class="btn" @click="closeTransfer">取消</button>
            <button class="btn primary" @click="submitTransfer">
              {{ transferMode === "export" ? "导出" : "导入" }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 导入 / 导出结果提示 -->
    <div v-if="notice" class="toast">{{ notice }}</div>

    <!-- 主页卡片操作菜单：跟随光标或「更多」按钮，统一弹在 body 上避免被卡片裁剪 -->
    <Teleport to="body">
      <div
        v-if="hostMenu"
        ref="hostMenuEl"
        class="host-menu"
        :style="{ left: `${hostMenu.left}px`, top: `${hostMenu.top}px` }"
        @click.stop
        @contextmenu.prevent
      >
        <button class="host-menu-item" @click="runHostAction('connect')">
          <svg viewBox="0 0 16 16">
            <path d="M5.5 2v3.2M10.5 2v3.2M3.6 5.2h8.8v1.9a4.4 4.4 0 0 1-8.8 0V5.2ZM8 11.5V14" />
          </svg>
          连接
        </button>
        <button class="host-menu-item" @click="runHostAction('edit')">
          <svg viewBox="0 0 16 16">
            <path d="M11.3 2.7l2 2-7.4 7.4-2.7.7.7-2.7 7.4-7.4Z" />
          </svg>
          编辑
        </button>
        <button class="host-menu-item" @click="runHostAction('duplicate')">
          <svg viewBox="0 0 16 16">
            <rect x="5.6" y="5.6" width="8" height="8" rx="1.6" />
            <path d="M10.4 3.2H4.2a1.6 1.6 0 0 0-1.6 1.6v6.2" />
          </svg>
          复制
        </button>
        <div class="host-menu-sep" />
        <button class="host-menu-item danger" @click="runHostAction('delete')">
          <svg viewBox="0 0 16 16">
            <path d="M2.8 4.4h10.4M6.4 4.4V2.9h3.2v1.5M4.4 4.4l.6 8.5a1 1 0 0 0 1 .9h3.9a1 1 0 0 0 1-.9l.6-8.5" />
          </svg>
          删除
        </button>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.app {
  display: grid;
  grid-template-columns: 196px minmax(0, 1fr);
  grid-template-rows: 38px auto minmax(0, 1fr);
  height: 100%;
  background: var(--win-bg);
  color: var(--win-text);
  overflow: hidden;
  font-size: 13px;
}
/* 终端 / SFTP 页隐藏左侧导航，内容占满整窗 */
.app.no-rail {
  grid-template-columns: minmax(0, 1fr);
}
.app.no-rail .content {
  grid-column: 1;
}

/* ------------------------------------------------------------- 左侧导航列表 */
.rail {
  grid-column: 1;
  grid-row: 2 / span 2;
  position: relative;
  z-index: 90;
  display: flex;
  flex-direction: column;
  padding: 10px;
  background: var(--win-nav);
  border-right: 1px solid var(--win-stroke);
}
.rail-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.rail-foot {
  margin-top: auto;
}
.rail-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  height: 36px;
  padding: 0 10px;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: var(--win-text-2);
  font-size: 12.5px;
  text-align: left;
  cursor: default;
  transition: background 0.12s ease, color 0.12s ease;
}
.rail-item:hover:not(:disabled) {
  background: var(--win-fill);
  color: var(--win-text);
}
.rail-item.on {
  background: var(--win-fill-press);
  border-color: var(--win-stroke-strong);
  color: var(--win-text);
  font-weight: 600;
}
.rail-item:disabled {
  color: var(--win-text-disabled);
}
.rail-item svg {
  flex: none;
  width: 18px;
  height: 18px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.3;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.rail-item.on svg {
  stroke: var(--win-accent);
}

/* 设置弹窗：独立浮层页面，居中展示 */
.settings-mask {
  position: fixed;
  inset: 0;
  z-index: 200;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(8, 10, 14, 0.55);
  backdrop-filter: blur(2px);
  outline: none;
  animation: confirm-fade 0.12s ease-out;
}
.settings-dialog {
  display: flex;
  flex-direction: column;
  width: min(560px, 94vw);
  border: 1px solid var(--win-stroke-strong);
  border-radius: 14px;
  background: var(--win-flyout);
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.55), 0 2px 8px rgba(0, 0, 0, 0.35);
  animation: confirm-pop 0.14s ease-out;
}
.settings-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 18px 12px;
}
.settings-head strong {
  font-size: 14px;
  font-weight: 600;
}
.settings-close {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--win-text-3);
  font-size: 17px;
  line-height: 1;
  cursor: default;
}
.settings-close:hover {
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.settings-main {
  display: flex;
  min-height: 248px;
}
/* 左栏：导入 / 导出为一级菜单，外观下挂主题与字号两个二级项 */
.settings-nav {
  flex: none;
  width: 152px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 4px 10px 16px;
  border-right: 1px solid var(--win-stroke);
}
.settings-nav-item {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 32px;
  padding: 0 10px;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: var(--win-text-2);
  font-size: 12.5px;
  text-align: left;
  cursor: default;
}
.settings-nav-item:hover {
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.settings-nav-item.on {
  border-color: var(--win-stroke-strong);
  background: var(--win-fill-press);
  color: var(--win-text);
  font-weight: 600;
}
.settings-nav-item svg {
  flex: none;
  width: 16px;
  height: 16px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.3;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.settings-nav-item.on svg {
  stroke: var(--win-accent);
}
/* 二级菜单展开箭头 */
.nav-caret {
  margin-left: auto;
  color: var(--win-text-3);
  font-size: 13px;
  line-height: 1;
  transition: transform 0.12s ease;
}
.nav-caret.open {
  transform: rotate(90deg);
}
.settings-subnav {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 2px 0 4px 22px;
}
.settings-subnav button {
  height: 28px;
  padding: 0 10px;
  border: 1px solid transparent;
  border-radius: 7px;
  background: transparent;
  color: var(--win-text-3);
  font-size: 12px;
  text-align: left;
  cursor: default;
}
.settings-subnav button:hover {
  background: var(--win-fill-hover);
  color: var(--win-text-2);
}
.settings-subnav button.on {
  background: var(--win-fill-press);
  color: var(--win-text);
  font-weight: 600;
}
.settings-content {
  flex: 1;
  min-width: 0;
  padding: 8px 20px 18px;
}
.settings-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 18px 16px;
  border-top: 1px solid var(--win-stroke);
}
.settings-actions .btn {
  height: 32px;
  padding: 0 16px;
  font-size: 12.5px;
}
.set-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.set-desc {
  margin: 0;
  color: var(--win-text-3);
  font-size: 12px;
  line-height: 1.7;
}
.set-btnrow {
  display: flex;
  gap: 8px;
  margin-top: 4px;
}
.set-btnrow .btn {
  height: 32px;
  padding: 0 16px;
  font-size: 12.5px;
}
/* 可选的加密导出入口：低调的文字按钮，不抢主操作 */
.set-link {
  align-self: flex-start;
  margin-top: 2px;
  padding: 2px 0;
  border: 0;
  background: none;
  color: var(--win-text-3);
  font-size: 12px;
  cursor: pointer;
}
.set-link:hover:not(:disabled) {
  color: var(--win-accent);
}
.set-link:disabled {
  opacity: 0.5;
  cursor: default;
}
.set-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.set-label {
  color: var(--win-text-2);
  font-size: 12px;
  font-weight: 600;
}
.set-value {
  color: var(--win-accent);
  font-size: 12px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.set-seg {
  display: flex;
  gap: 4px;
  padding: 3px;
  border: 1px solid var(--win-stroke);
  border-radius: 8px;
  background: var(--win-fill);
}
.set-seg button {
  flex: 1;
  height: 26px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: var(--win-text-2);
  font-size: 12px;
  cursor: default;
  white-space: nowrap;
}
.set-seg button:hover {
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.set-seg button.on {
  border-color: var(--win-stroke-strong);
  background: var(--win-fill-press);
  color: var(--win-text);
  font-weight: 600;
}
.set-range {
  -webkit-appearance: none;
  appearance: none;
  width: 100%;
  height: 4px;
  margin: 4px 0;
  border-radius: 999px;
  background: var(--win-stroke-strong);
  outline: none;
  cursor: default;
}
.set-range::-webkit-slider-thumb {
  -webkit-appearance: none;
  appearance: none;
  width: 14px;
  height: 14px;
  border: 2px solid var(--win-flyout);
  border-radius: 50%;
  background: var(--win-accent);
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.3);
}
.set-hint {
  color: var(--win-text-3);
  font-size: 11px;
}

/* ------------------------------------------------------------- 第一行 */
.titlebar {
  grid-column: 1 / -1;
  grid-row: 1;
  position: relative;
  z-index: 100;
  display: flex;
  align-items: center;
  gap: 10px;
  height: 38px;
  flex: none;
  padding-left: 10px;
  background: var(--win-chrome);
  user-select: none;
  --wails-draggable: drag;
}
.tb-brand {
  display: flex;
  align-items: center;
  gap: 7px;
  flex: none;
  padding-right: 2px;
}
.tb-brand strong {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.2px;
}
.logo {
  display: grid;
  place-items: center;
  width: 18px;
  height: 18px;
  border-radius: 4px;
  background: var(--win-accent);
  color: #06121f;
  font-size: 12px;
  font-weight: 700;
}
.tb-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 13px;
  border: 1px solid var(--win-stroke);
  border-radius: 7px;
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12.5px;
  font-weight: 600;
  cursor: default;
}
.tb-btn svg {
  width: 15px;
  height: 15px;
  fill: none;
  stroke: var(--win-accent);
  stroke-width: 1.4;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.tb-btn:hover {
  background: var(--win-fill-hover);
  border-color: var(--win-stroke-strong);
}
.tb-btn:active {
  background: var(--win-fill-press);
}
.tb-btn:disabled {
  opacity: 0.45;
}
.tb-btn:disabled svg {
  stroke: var(--win-text-disabled);
}
.tb-btn.on {
  border-color: var(--win-stroke-strong);
  background: var(--win-fill-press);
}
.tb-plus {
  padding: 0;
  width: 30px;
  justify-content: center;
}

/* 顶栏已打开终端：能放下的标签平铺，放不下的收进「更多」按钮 + 数量角标 */
.tb-sessions {
  position: relative;
  display: flex;
  align-items: center;
  gap: 4px;
  flex: 0 1 auto;
  min-width: 0;
}
/* 从分屏拖回顶部标题栏时的落点提示 */
.titlebar.pane-drop {
  outline: 1px dashed var(--win-accent);
  outline-offset: -2px;
  border-radius: 8px;
  background: var(--win-accent-soft);
}
.tb-tabs {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
  overflow: hidden;
}
/* 隐藏副本：绝对定位、不参与布局，用于测量标签总宽度 */
.tb-tabs-ghost {
  position: absolute;
  left: 0;
  top: 0;
  width: max-content;
  overflow: visible;
  visibility: hidden;
  pointer-events: none;
}
.tb-tab {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: none;
  max-width: 190px;
  height: 28px;
  padding: 0 9px;
  border-radius: 7px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text-2);
  font-size: 12.5px;
  cursor: default;
  user-select: none;
}
.tb-tab:hover {
  background: var(--win-fill-hover);
  border-color: var(--win-stroke-strong);
}
.tb-tab.on {
  background: var(--win-fill-press);
  border-color: var(--win-stroke-strong);
  color: var(--win-text);
}
.tb-tab-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tb-tab .x {
  flex: none;
  width: 16px;
  height: 16px;
  display: grid;
  place-items: center;
  border-radius: 4px;
  color: var(--win-text-3);
  font-size: 14px;
  line-height: 1;
}
.tb-tab .x:hover {
  background: var(--win-fill-press);
  color: var(--win-err);
}
/* 合并组角标：显示并排显示的终端数量 */
.tb-group {
  flex: none;
  display: grid;
  place-items: center;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: 999px;
  background: var(--win-accent-soft);
  color: var(--win-accent);
  font-size: 10.5px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
/* 拖动合并标签的视觉反馈 */
.tb-tab.dragging {
  opacity: 0.55;
}
.tb-tab.drop {
  background: var(--win-accent-soft);
  border-color: var(--win-accent);
  color: var(--win-text);
}
.tb-session-btn {
  gap: 7px;
}
/* 「更多」按钮：只保留图标 + 收起数量，尽量少占宽度 */
.tb-more {
  flex: none;
  gap: 5px;
  padding: 0 8px;
}
.tb-count {
  display: grid;
  place-items: center;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--win-accent-soft);
  color: var(--win-accent);
  font-size: 11px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
/* 「更多」收起列表：弹出在页面正中间 */
.search-pop.session-pop {
  position: fixed;
  top: 50%;
  left: 50%;
  right: auto;
  width: 420px;
  max-width: calc(100vw - 48px);
  transform: translate(-50%, -50%);
}
.sp-session {
  cursor: default;
}
.sp-session.on {
  background: var(--win-fill-press);
}
.sp-session .x {
  flex: none;
  width: 18px;
  height: 18px;
  display: grid;
  place-items: center;
  border-radius: 4px;
  color: var(--win-text-3);
  font-size: 14px;
  line-height: 1;
}
.sp-session .x:hover {
  background: var(--win-fill-press);
  color: var(--win-err);
}

/* 顶栏搜索浮层 */
.tb-search {
  position: relative;
  flex: none;
}
.search-pop {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  z-index: 20;
  width: 328px;
  display: flex;
  flex-direction: column;
  padding: 6px;
  border: 1px solid var(--win-stroke-strong);
  border-radius: 10px;
  background: var(--win-flyout);
  box-shadow: 0 16px 36px rgba(0, 0, 0, 0.5);
}
.sp-input {
  display: flex;
  align-items: center;
  flex: none;
  height: 32px;
  padding: 0 9px;
  border: 1px solid var(--win-stroke-strong);
  border-radius: 7px;
  background: var(--win-fill-press);
}
.sp-input input {
  flex: 1;
  min-width: 0;
  height: 28px;
  border: 0;
  background: transparent;
  color: var(--win-text);
  font-size: 12.5px;
  outline: none;
}
.sp-input input::placeholder {
  color: var(--win-text-3);
}
.sp-list {
  max-height: 300px;
  overflow: auto;
  margin-top: 4px;
}
.sp-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 9px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--win-text);
  text-align: left;
  cursor: default;
}
.sp-item:hover {
  background: var(--win-fill-hover);
}
.sp-name {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 12.5px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sp-meta {
  flex: none;
  max-width: 62%;
  color: var(--win-text-3);
  font-size: 11px;
  font-family: var(--win-mono);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sp-empty {
  margin: 0;
  padding: 14px 10px;
  color: var(--win-text-3);
  font-size: 12px;
  text-align: center;
}
.tb-drag {
  flex: 1;
  align-self: stretch;
  min-width: 20px;
}
.tb-controls {
  display: flex;
  align-self: stretch;
  flex: none;
}
.win-btn {
  width: 46px;
  border: 0;
  background: transparent;
  color: var(--win-text-2);
  display: grid;
  place-items: center;
  cursor: default;
}
.win-btn svg {
  width: 10px;
  height: 10px;
  stroke: currentColor;
  stroke-width: 1;
  fill: none;
}
.win-btn:hover {
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.win-btn:active {
  background: var(--win-fill-press);
}
.win-btn.close:hover {
  background: var(--win-close);
  color: #fff;
}

/* ------------------------------------------------------------- 第二行 */
.home-bar {
  grid-column: 2;
  grid-row: 2;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
  flex: none;
  padding: 10px 18px;
  border-bottom: 1px solid var(--win-stroke);
  background: var(--win-bg);
}
.home-bar input {
  align-self: stretch;
  min-width: 0;
  height: 34px;
  padding: 0 13px;
  border-radius: 9px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 13px;
  outline: none;
  transition: border-color 0.12s ease, box-shadow 0.12s ease;
}
.home-bar input::placeholder {
  color: var(--win-text-3);
}
.home-bar input:hover {
  background: var(--win-fill-hover);
}
.home-bar input:focus {
  border-color: var(--win-accent);
  box-shadow: 0 0 0 3px var(--win-accent-soft);
}
.new-btn {
  height: 32px;
  padding: 0 14px;
}

/* ------------------------------------------------------------- 第三行 */
.content {
  grid-column: 2;
  grid-row: 3;
  flex: 1;
  min-height: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.list-view {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 14px 18px 18px;
}
.list-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
  flex: none;
  padding-bottom: 12px;
}
.lh-text h2 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}
.cards {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
.group + .group {
  margin-top: 18px;
}
.group-head {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding-bottom: 8px;
}
.group-name {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--win-text-2);
}
.group-count {
  color: var(--win-text-3);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.group-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  align-content: start;
  gap: 12px;
}
.card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 14px;
  border-radius: 12px;
  border: 1px solid var(--win-stroke);
  background: var(--win-card);
  transition: border-color 0.12s ease, background 0.12s ease, transform 0.12s ease;
}
.card:hover {
  border-color: var(--win-stroke-strong);
  background: var(--win-fill-hover);
  z-index: 2;
}
.card-top {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.avatar {
  flex: none;
  width: 36px;
  height: 36px;
  border-radius: 9px;
  display: grid;
  place-items: center;
  color: rgba(8, 16, 24, 0.86);
  font-size: 15px;
  font-weight: 700;
  text-transform: uppercase;
}
.who {
  flex: 1;
  min-width: 0;
}
.who strong {
  display: block;
  font-size: 13.5px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.who small {
  display: block;
  margin-top: 2px;
  color: var(--win-text-3);
  font-size: 11.5px;
  font-family: var(--win-mono);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.tag {
  padding: 2px 8px;
  border-radius: 6px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text-3);
  font-size: 10.5px;
}
.more {
  flex: none;
}
.more-btn {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: var(--win-text-3);
  opacity: 0;
  transition: opacity 0.12s ease, background 0.12s ease;
  cursor: default;
}
.card:hover .more-btn {
  opacity: 1;
}
.more-btn:hover {
  background: var(--win-fill-hover);
  border-color: var(--win-stroke);
  color: var(--win-text);
}
.more-btn svg {
  width: 15px;
  height: 15px;
  fill: currentColor;
}

/* 代码块卡片：图标 + 名称 + 代码预览 */
.snippet-card {
  gap: 10px;
}
.snippet-icon {
  flex: none;
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: 9px;
  background: var(--win-accent-soft);
  color: var(--win-accent);
}
.snippet-icon svg {
  width: 18px;
  height: 18px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.3;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.snippet-code {
  margin: 0;
  max-height: 120px;
  overflow: hidden;
  padding: 10px 12px;
  border: 1px solid var(--win-stroke);
  border-radius: 8px;
  background: var(--win-term);
  color: var(--win-text-2);
  font-family: var(--win-mono);
  font-size: 11.5px;
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.snippet-actions {
  display: flex;
  gap: 2px;
}
.snippet-actions .more-btn svg {
  fill: none;
  stroke: currentColor;
  stroke-width: 1.2;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.more-btn.danger:hover {
  color: var(--win-err);
}

/* 卡片操作菜单：右键与「更多」按钮共用，跟随界面深色风格 */
.host-menu {
  position: fixed;
  z-index: 120;
  min-width: 178px;
  padding: 5px;
  border: 1px solid var(--win-stroke-strong);
  border-radius: 10px;
  background: var(--win-flyout);
  box-shadow: 0 18px 42px rgba(0, 0, 0, 0.55), 0 2px 8px rgba(0, 0, 0, 0.35);
  animation: host-menu-in 0.11s ease-out;
}
@keyframes host-menu-in {
  from {
    opacity: 0;
    transform: translateY(-3px) scale(0.98);
  }
}
.host-menu-item {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  padding: 7px 9px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--win-text);
  font-size: 12.5px;
  text-align: left;
  cursor: default;
  transition: background 0.1s ease, color 0.1s ease;
}
.host-menu-item svg {
  flex: none;
  width: 14px;
  height: 14px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.3;
  stroke-linecap: round;
  stroke-linejoin: round;
  color: var(--win-text-3);
  transition: color 0.1s ease;
}
.host-menu-item:hover {
  background: var(--win-fill-hover);
}
.host-menu-item:hover svg {
  color: var(--win-text-2);
}
.host-menu-item.danger {
  color: var(--win-err);
}
.host-menu-item.danger svg {
  color: var(--win-err);
  opacity: 0.8;
}
.host-menu-item.danger:hover {
  background: rgba(255, 107, 107, 0.1);
}
.host-menu-item:disabled {
  color: var(--win-text-disabled);
}
.host-menu-item:disabled svg {
  color: var(--win-text-disabled);
}
.host-menu-item:disabled:hover {
  background: transparent;
}
.host-menu-sep {
  height: 1px;
  margin: 4px 6px;
  background: var(--win-stroke);
}

/* 删除确认弹窗：沿用深色浮层风格，避免浏览器原生 confirm 破坏观感 */
.confirm-mask {
  position: fixed;
  inset: 0;
  z-index: 200;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(8, 10, 14, 0.62);
  backdrop-filter: blur(2px);
  outline: none;
  animation: confirm-fade 0.12s ease-out;
}
.confirm-dialog {
  width: min(400px, 92vw);
  padding: 20px;
  border: 1px solid var(--win-stroke-strong);
  border-radius: 14px;
  background: var(--win-flyout);
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.55), 0 2px 8px rgba(0, 0, 0, 0.35);
  animation: confirm-pop 0.14s ease-out;
}
.confirm-head {
  display: flex;
  gap: 12px;
  align-items: flex-start;
}
.confirm-icon {
  flex: none;
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: 10px;
  background: rgba(255, 107, 107, 0.12);
  color: var(--win-err);
}
.confirm-icon svg {
  width: 17px;
  height: 17px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.3;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.confirm-icon.warn {
  background: rgba(255, 180, 84, 0.14);
  color: var(--win-warn);
}
.confirm-text {
  min-width: 0;
}
.confirm-text h3 {
  margin: 1px 0 6px;
  font-size: 14px;
  font-weight: 600;
  color: var(--win-text);
}
.confirm-text p {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.6;
  color: var(--win-text-2);
  overflow-wrap: anywhere;
}
.confirm-text strong {
  color: var(--win-text);
  font-weight: 600;
}
.confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 18px;
}
.confirm-actions .btn {
  height: 32px;
  padding: 0 18px;
  font-size: 13px;
}
/* 导入 / 导出密码弹窗 */
.pwd-dialog {
  width: min(420px, 92vw);
}
.pwd-fields {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-top: 16px;
}
.pwd-fields label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: var(--win-text-3);
  font-size: 12px;
}
.pwd-fields input {
  height: 32px;
  padding: 0 10px;
  border: 1px solid var(--win-stroke);
  border-radius: 8px;
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12.5px;
  outline: none;
}
.pwd-fields input::placeholder {
  color: var(--win-text-3);
}
.pwd-fields input:focus {
  border-color: var(--win-accent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--win-accent) 18%, transparent);
}
.pwd-error {
  margin: 0;
  color: var(--win-close);
  font-size: 12px;
  line-height: 1.5;
}
.btn.danger {
  background: var(--win-close);
  border-color: transparent;
  color: #fff;
  font-weight: 600;
}
.btn.danger:hover:not(:disabled) {
  background: #f05256;
}
.btn.danger:active:not(:disabled) {
  background: #cf3b40;
}
@keyframes confirm-fade {
  from {
    opacity: 0;
  }
}
@keyframes confirm-pop {
  from {
    opacity: 0;
    transform: translateY(6px) scale(0.97);
  }
}

/* 通用按钮 */
.btn {
  height: 30px;
  padding: 0 12px;
  border-radius: 8px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12px;
  cursor: default;
  white-space: nowrap;
}
.btn:hover:not(:disabled) {
  background: var(--win-fill-hover);
}
.btn:active:not(:disabled) {
  background: var(--win-fill-press);
}
.btn:disabled {
  color: var(--win-text-disabled);
  cursor: not-allowed;
}
.btn.primary {
  background: var(--win-accent);
  border-color: transparent;
  color: var(--win-accent-text);
  font-weight: 600;
}
.btn.primary:hover:not(:disabled) {
  background: var(--win-accent-hover);
}

.empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  text-align: center;
  color: var(--win-text-3);
}
.empty-state h3 {
  margin: 0;
  color: var(--win-text-2);
  font-size: 15px;
  font-weight: 600;
}
.empty-state p {
  margin: 0 0 8px;
  font-size: 12px;
}
.empty-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* 导入 / 导出结果提示 */
.toast {
  position: fixed;
  left: 50%;
  bottom: 24px;
  z-index: 300;
  max-width: min(680px, 80vw);
  padding: 9px 15px;
  border: 1px solid var(--win-stroke-strong);
  border-radius: 9px;
  background: var(--win-flyout);
  box-shadow: 0 10px 28px rgba(0, 0, 0, 0.28);
  color: var(--win-text);
  font-size: 12.5px;
  overflow-wrap: anywhere;
  pointer-events: none;
  transform: translateX(-50%);
  animation: confirm-pop 0.14s ease-out;
}

/* ------------------------------------------------------------- 终端视图 */
.term-view {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--win-text-3);
  flex: none;
}
.dot.connected {
  background: var(--win-ok);
}
.dot.connecting {
  background: var(--win-warn);
}
.dot.error {
  background: var(--win-err);
}
.terminals {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  position: relative;
}
/* 合并分屏：1px 缝隙透出描边色，形成分隔线 */
.terminals.merged {
  background: var(--win-stroke);
}
.pane-slot {
  position: absolute;
  overflow: hidden;
  box-sizing: border-box;
}
/* 合并时每个面板留出 1px 间隙，嵌套分屏也只会影响自己所在的区域 */
.terminals.merged .pane-slot {
  padding: 0 1px 1px 0;
}
.pane-slot > :deep(.pane) {
  width: 100%;
  height: 100%;
}
/* 拖拽落点预览：覆盖目标面板将要分出去的那一半 */
.split-preview {
  position: absolute;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--win-accent);
  background: var(--win-accent-soft);
  border-radius: 4px;
  pointer-events: none;
  transition: all 0.08s ease-out;
}
.split-preview > span {
  padding: 4px 10px;
  border-radius: 999px;
  background: var(--win-accent);
  color: #fff;
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.02em;
  white-space: nowrap;
}
/* 合并分屏顶部工具栏 */
.term-toolbar {
  flex: none;
  display: flex;
  align-items: center;
  gap: 8px;
  height: 36px;
  padding: 0 10px;
  border-bottom: 1px solid var(--win-stroke);
  background: var(--win-chrome);
  color: var(--win-text-2);
  font-size: 12px;
}
.term-toolbar .tt-info {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--win-text);
  font-weight: 600;
}
/* 工作组成员数量角标 */
.term-toolbar .tt-count {
  padding: 1px 6px;
  border-radius: 999px;
  background: var(--win-fill);
  color: var(--win-text-3);
  font-size: 11px;
  font-weight: 400;
  font-variant-numeric: tabular-nums;
}
/* 工具栏右侧操作提示 */
.term-toolbar .tt-hint {
  margin-left: auto;
  color: var(--win-text-3);
  font-size: 11.5px;
}
.term-toolbar svg {
  width: 14px;
  height: 14px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.4;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.term-toolbar .tt-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  height: 24px;
  padding: 0 9px;
  border: 1px solid var(--win-stroke);
  border-radius: 7px;
  background: var(--win-fill);
  color: var(--win-text-2);
  font-size: 12px;
  cursor: default;
}
.term-toolbar .tt-btn:hover {
  background: var(--win-fill-hover);
  border-color: var(--win-stroke-strong);
  color: var(--win-text);
}
.term-toolbar .tt-btn.on {
  background: var(--win-accent-soft);
  border-color: var(--win-accent);
  color: var(--win-accent);
}
</style>
