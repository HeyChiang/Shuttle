// 全局状态与后端事件路由。
// 所有 Wails 事件只在这里订阅一次，再分发给各终端与面板组件。
import { computed, reactive, ref } from "vue";
import { Events } from "@wailsio/runtime";
import { t } from "./i18n";
import * as SSH from "../bindings/shuttle/sshservice";
import {
  SshClosed,
  SshData,
  SshExec,
  SshStatus,
  SftpProgress,
} from "../bindings/events";
import type {
  ExecEvent,
  HostConfig,
  SessionInfo,
  TransferEvent,
} from "../bindings/shuttle/internal/sshcore/models";

// ---------------------------------------------------------------- 状态

/** 已保存的连接档案 */
export const hosts = ref<HostConfig[]>([]);
/** 当前存活（或正在连接）的会话 */
export const sessions = ref<SessionInfo[]>([]);
/** 可选字符集，由后端提供 */
export const encodings = ref<string[]>(["UTF-8"]);

/** 同步输入：打开后任意终端敲下的按键会广播到全部连接 */
export const syncInput = ref(false);
/** 参与广播命令的会话 id */
export const selectedIds = reactive(new Set<string>());
/** 当前聚焦的会话 */
export const activeId = ref("");

// ---------------------------------------------------------------- 合并分屏

/** 分屏方向：row = 左右并排，column = 上下排列 */
export type SplitDirection = "row" | "column";

/** 分屏布局树：叶子是一个终端，split 节点把所在区域按方向切成若干份 */
export type SplitNode =
  | { type: "leaf"; id: string }
  | { type: "split"; direction: SplitDirection; children: SplitNode[] };

/** 合并显示的终端分组：ids 为组内全部会话，layout 决定嵌套分屏结构 */
export interface SplitGroup {
  ids: string[];
  layout: SplitNode;
}

/** 合并显示的终端分组：每组是一组会话 id，未分组的会话单独占一个标签。 */
export const splitGroups = ref<SplitGroup[]>([]);
/** 分屏同步输入：打开后组内任一终端输入都会广播到同组其他终端。 */
export const splitSync = ref(false);

/** 顶栏标签：单个会话，或合并后的多个会话 */
export interface SessionTab {
  key: string;
  sessions: SessionInfo[];
  group: boolean;
}

/** 顶栏标签列表，按会话连接顺序排列；合并组出现在其首个成员的位置。 */
export const tabs = computed<SessionTab[]>(() => {
  const out: SessionTab[] = [];
  const used = new Set<string>();
  for (const s of sessions.value) {
    if (used.has(s.id)) continue;
    const group = splitGroups.value.find((g) => g.ids.includes(s.id));
    if (group) {
      const list = group.ids
        .map((id) => sessionById(id))
        .filter((item): item is SessionInfo => !!item);
      list.forEach((item) => used.add(item.id));
      if (list.length) {
        out.push({
          key: `g:${list.map((item) => item.id).join(",")}`,
          sessions: list,
          group: list.length > 1,
        });
      }
    } else {
      used.add(s.id);
      out.push({ key: `s:${s.id}`, sessions: [s], group: false });
    }
  }
  return out;
});

/** 某会话所在的分组（未分组时返回 undefined） */
export function splitGroupOf(id: string): SplitGroup | undefined {
  return splitGroups.value.find((g) => g.ids.includes(id));
}

/** 从布局树里移除若干终端，返回修剪后的树（全部移除时为 null） */
function pruneLeaves(node: SplitNode, ids: string[]): SplitNode | null {
  if (node.type === "leaf") return ids.includes(node.id) ? null : node;
  const children = node.children
    .map((child) => pruneLeaves(child, ids))
    .filter((child): child is SplitNode => !!child);
  if (!children.length) return null;
  if (children.length === 1) return children[0];
  return { type: "split", direction: node.direction, children };
}

/** 把布局树里的一个叶子替换成新的子树 */
function replaceLeaf(node: SplitNode, id: string, next: SplitNode): SplitNode {
  if (node.type === "leaf") return node.id === id ? next : node;
  return {
    type: "split",
    direction: node.direction,
    children: node.children.map((child) => replaceLeaf(child, id, next)),
  };
}

/**
 * 把拖入的标签并入指定终端面板：只在该面板内再分屏，组内其他面板的大小不受影响。
 * direction / sourceFirst 由拖放落点在目标面板内的位置决定。
 * 返回合并后的会话 id 列表。
 */
export function mergeIntoPane(
  sourceIds: string[],
  targetId: string,
  direction: SplitDirection,
  sourceFirst = false,
): string[] {
  const src = sourceIds.filter((id) => id !== targetId && sessionById(id));
  const group = splitGroupOf(targetId);
  if (!src.length) return group?.ids ?? [targetId];
  // 拖来的标签已经和目标面板在同一个工作组里：不做处理
  if (group && src.some((id) => group.ids.includes(id))) return group.ids;

  const targetLeaf: SplitNode = { type: "leaf", id: targetId };
  const sources: SplitNode[] = src.map((id) => ({ type: "leaf", id }));
  const split: SplitNode = {
    type: "split",
    direction,
    children: sourceFirst ? [...sources, targetLeaf] : [targetLeaf, ...sources],
  };

  // 先把来源会话从各自的分组里摘掉，剩一个成员的组自动解散
  const next = splitGroups.value
    .filter((g) => g !== group)
    .map((g): SplitGroup | null => {
      if (!g.ids.some((id) => src.includes(id))) return g;
      const ids = g.ids.filter((id) => !src.includes(id));
      const layout = pruneLeaves(g.layout, src);
      return layout && ids.length > 1 ? { ids, layout } : null;
    })
    .filter((g): g is SplitGroup => !!g);

  const mergedIds = group ? [...group.ids, ...src] : [targetId, ...src];
  const layout = group ? replaceLeaf(group.layout, targetId, split) : split;
  next.push({ ids: mergedIds, layout });
  splitGroups.value = next;
  return mergedIds;
}

/** 交换两个标签的位置（合并组作为一个整体移动）。 */
export function swapTabs(sourceIds: string[], targetIds: string[]) {
  if (sourceIds.some((id) => targetIds.includes(id))) return;
  const list = tabs.value;
  const srcIdx = list.findIndex((t) =>
    t.sessions.some((s) => sourceIds.includes(s.id)),
  );
  const dstIdx = list.findIndex((t) =>
    t.sessions.some((s) => targetIds.includes(s.id)),
  );
  if (srcIdx < 0 || dstIdx < 0 || srcIdx === dstIdx) return;
  const next = [...list];
  [next[srcIdx], next[dstIdx]] = [next[dstIdx], next[srcIdx]];
  sessions.value = next.flatMap((t) => t.sessions);
}

/** 拆分分组：组内会话恢复为各自独立的标签。 */
export function splitGroup(id: string) {
  const group = splitGroups.value.find((g) => g.ids.includes(id));
  if (!group) return;
  splitGroups.value = splitGroups.value.filter((g) => g !== group);
}

/** 把某个会话从合并组里移出，恢复成独立标签（其余成员仍留在组内）。 */
export function detachFromGroup(id: string) {
  const group = splitGroupOf(id);
  if (!group) return;
  const rest = group.ids.filter((sid) => sid !== id);
  const layout = pruneLeaves(group.layout, [id]);
  splitGroups.value = splitGroups.value
    .map((g): SplitGroup | null =>
      g === group ? (layout && rest.length > 1 ? { ids: rest, layout } : null) : g,
    )
    .filter((g): g is SplitGroup => !!g);
}

/** 会话 id -> 原始连接档案，用于断线后重新连接 */
const sessionConfigs = new Map<string, HostConfig>();
/** 广播命令的执行结果 */
export const execResults = ref<ExecEvent[]>([]);
/** 传输进度（最新的在前） */
export const transfers = ref<TransferEvent[]>([]);
/** 底部抽屉是否展开 */
export const drawerOpen = ref(true);

// ---------------------------------------------------------------- 工具

let seq = 0;
/** 生成会话 id。不使用 crypto.randomUUID，兼容非安全上下文的自定义协议。 */
export function newSessionId(): string {
  seq += 1;
  return `s${Date.now().toString(36)}${seq.toString(36)}${Math.random()
    .toString(36)
    .slice(2, 7)}`;
}

export function sessionById(id: string): SessionInfo | undefined {
  return sessions.value.find((s) => s.id === id);
}

export function sessionLabel(s: SessionInfo): string {
  return s.name || `${s.user}@${s.host}`;
}

export function isConnected(s: SessionInfo): boolean {
  return s.status === "connected";
}

/** 参与广播的目标会话（已勾选且已连接） */
export function broadcastTargets(): string[] {
  return sessions.value
    .filter((s) => isConnected(s) && selectedIds.has(s.id))
    .map((s) => s.id);
}

// ---------------------------------------------------------------- 终端数据路由

type DataSink = (data: string) => void;
const dataSinks = new Map<string, DataSink>();

/** 终端组件挂载时注册自己的写入函数，返回注销函数。 */
export function registerTerminal(id: string, sink: DataSink): () => void {
  dataSinks.set(id, sink);
  return () => {
    if (dataSinks.get(id) === sink) dataSinks.delete(id);
  };
}

type FocusSink = () => void;
const focusSinks = new Map<string, FocusSink>();

/** 终端组件挂载时注册聚焦函数，返回注销函数。 */
export function registerTerminalFocus(id: string, focus: FocusSink): () => void {
  focusSinks.set(id, focus);
  return () => {
    if (focusSinks.get(id) === focus) focusSinks.delete(id);
  };
}

/** 把键盘焦点交还给指定会话的终端，脚本执行后可直接继续输入。 */
export function focusTerminal(id: string) {
  focusSinks.get(id)?.();
}

// ---------------------------------------------------------------- 事件订阅

let started = false;

export function startEventBridge() {
  if (started) return;
  started = true;

  Events.On(SshData, (ev) => {
    const payload = ev.data;
    if (!payload) return;
    const sink = dataSinks.get(payload.sessionId);
    if (sink) sink(payload.data);
  });

  Events.On(SshStatus, (ev) => {
    const payload = ev.data;
    if (!payload) return;
    const s = sessionById(payload.sessionId);
    if (!s) return;
    s.status = payload.status;
    s.message = payload.message;
    if (payload.status === "connected" && !s.connectedAt) {
      s.connectedAt = Date.now();
    }
  });

  Events.On(SshClosed, (ev) => {
    const payload = ev.data;
    if (!payload) return;
    const s = sessionById(payload.sessionId);
    if (!s) return;
    // 远端执行 exit / logout 正常退出时直接关闭该终端标签；
    // 网络中断等异常断开则保留标签，方便用户一键重连。
    if (payload.normal) {
      removeSession(payload.sessionId);
      return;
    }
    s.status = "closed";
    s.message = payload.reason || t("session.closedMessage");
    selectedIds.delete(payload.sessionId);
  });

  Events.On(SshExec, (ev) => {
    const payload = ev.data;
    if (!payload) return;
    execResults.value = [payload, ...execResults.value].slice(0, 200);
  });

  Events.On(SftpProgress, (ev) => {
    const payload = ev.data;
    if (!payload) return;
    // 同一次传输会持续推送进度事件，按任务合并到同一条，避免出现多条进度条。
    const sameTask = (t: TransferEvent) =>
      t.sessionId === payload.sessionId &&
      t.direction === payload.direction &&
      t.name === payload.name;

    // 传输结束立即收起进度条，只在传输途中展示进度。
    if (payload.done) {
      transfers.value = transfers.value.filter((t) => !sameTask(t));
      return;
    }

    const at = transfers.value.findIndex((t) => sameTask(t));
    if (at >= 0) transfers.value[at] = payload;
    else transfers.value = [payload, ...transfers.value].slice(0, 60);
  });
}

// ---------------------------------------------------------------- 主机档案

export async function loadHosts() {
  try {
    const list = (await SSH.LoadHosts()) ?? [];
    // 补齐历史档案里缺失的默认值，避免界面出现空编码/空端口。
    hosts.value = list.map((h) => ({
      ...blankHost(),
      ...h,
      id: h.id || newSessionId(),
      group: h.group || t("group.default"),
      port: h.port || 22,
      authMethod: h.authMethod || "password",
      encoding: h.encoding || "UTF-8",
      termType: h.termType || "xterm-256color",
      cols: h.cols || 120,
      rows: h.rows || 32,
      timeoutSec: h.timeoutSec || 15,
    }));
  } catch (e) {
    console.error("Failed to load host profiles", e);
  }
}

export async function persistHosts(next: HostConfig[]) {
  hosts.value = next;
  await SSH.SaveHosts(next);
}

export function blankHost(): HostConfig {
  return {
    id: newSessionId(),
    sessionId: "",
    name: "",
    group: t("group.default"),
    host: "",
    port: 22,
    user: "root",
    authMethod: "password",
    password: "",
    privateKeyPath: "",
    keyPassphrase: "",
    encoding: "UTF-8",
    termType: "xterm-256color",
    cols: 120,
    rows: 32,
    timeoutSec: 15,
  };
}

// ---------------------------------------------------------------- 会话操作

/** 同一服务器重复打开时，依次命名为「副本1」「副本2」… 避免标签重名。 */
function nextSessionName(base: string): string {
  const labels = new Set(sessions.value.map((s) => sessionLabel(s)));
  if (!labels.has(base)) return base;
  for (let n = 1; n < 1000; n++) {
    const candidate = t("session.copyN", { base, n });
    if (!labels.has(candidate)) return candidate;
  }
  return base;
}

export async function connect(cfg: HostConfig) {
  const sessionId = newSessionId();
  const name = nextSessionName(cfg.name || `${cfg.user}@${cfg.host}`);
  const info: SessionInfo = {
    id: sessionId,
    name,
    host: cfg.host,
    port: cfg.port,
    user: cfg.user,
    encoding: cfg.encoding,
    status: "connecting",
    message: t("session.connectingMessage"),
    cols: cfg.cols || 120,
    rows: cfg.rows || 32,
    connectedAt: 0,
  };
  sessions.value = [...sessions.value, info];
  selectedIds.add(sessionId);
  activeId.value = sessionId;
  sessionConfigs.set(sessionId, { ...cfg, sessionId });

  try {
    const real = await SSH.Connect({ ...cfg, sessionId });
    Object.assign(info, real, { id: sessionId, name });
  } catch (e) {
    info.status = "error";
    info.message = String(e);
  }
}

/** 连接失败或已断开后，用原始档案重连当前会话（复用同一个终端面板）。 */
export async function reconnect(id: string) {
  const s = sessionById(id);
  const cfg = sessionConfigs.get(id);
  if (!s || !cfg) return;

  s.status = "connecting";
  s.message = t("session.connectingMessage");
  s.connectedAt = 0;

  // 后端可能仍保留旧会话（如远端断开），先清理再重连，避免 id 冲突。
  try {
    await SSH.Disconnect(id);
  } catch {
    /* 旧会话可能已不存在 */
  }

  try {
    const real = await SSH.Connect({ ...cfg, sessionId: id });
    Object.assign(s, real, { id, name: s.name });
  } catch (e) {
    s.status = "error";
    s.message = String(e);
  }
}

export async function disconnect(id: string) {
  try {
    await SSH.Disconnect(id);
  } catch (e) {
    console.error(e);
  }
  removeSession(id);
}

export function removeSession(id: string) {
  sessions.value = sessions.value.filter((s) => s.id !== id);
  selectedIds.delete(id);
  dataSinks.delete(id);
  sessionConfigs.delete(id);
  // 同步清理合并分组：只剩一个成员的组自动拆散
  splitGroups.value = splitGroups.value
    .map((g): SplitGroup | null => {
      const ids = g.ids.filter((sid) => sid !== id);
      const layout = pruneLeaves(g.layout, [id]);
      return layout && ids.length > 1 ? { ids, layout } : null;
    })
    .filter((g): g is SplitGroup => !!g);
  if (activeId.value === id) {
    activeId.value = sessions.value[0]?.id ?? "";
  }
}

/** 发送按键。分屏同步时只广播到同组会话，全局同步时广播到全部连接。 */
export function sendKeys(id: string, data: string) {
  if (syncInput.value) {
    SSH.Write([], data).catch((e) => console.error("Failed to send input", e));
    return;
  }
  const group = splitGroupOf(id);
  if (splitSync.value && group && group.ids.length > 1) {
    const ids = group.ids.filter((sid) => {
      const s = sessionById(sid);
      return s ? isConnected(s) : false;
    });
    SSH.Write(ids.length ? ids : [id], data).catch((e) =>
      console.error("Failed to send input", e),
    );
    return;
  }
  SSH.Write([id], data).catch((e) => console.error("Failed to send input", e));
}

/** 广播一条命令到所选会话。 */
export async function runBroadcast(command: string) {
  const ids = broadcastTargets();
  if (!ids.length) {
    throw new Error(t("session.needSelection"));
  }
  await SSH.Exec(ids, command);
}

export function resizeSession(id: string, cols: number, rows: number) {
  SSH.Resize([id], cols, rows).catch(() => {});
}