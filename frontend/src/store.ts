// 全局状态与后端事件路由。
// 所有 Wails 事件只在这里订阅一次，再分发给各终端与面板组件。
import { reactive, ref } from "vue";
import { Events } from "@wailsio/runtime";
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
    s.status = "closed";
    s.message = payload.reason || "连接已断开";
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
    const at = transfers.value.findIndex((t) => !t.done && sameTask(t));
    if (at >= 0) transfers.value[at] = payload;
    else transfers.value = [payload, ...transfers.value].slice(0, 60);

    // 完成的任务短暂保留后移除，让进度区始终只显示正在进行的传输。
    if (payload.done) {
      window.setTimeout(() => {
        transfers.value = transfers.value.filter((t) => !(t.done && sameTask(t)));
      }, 2000);
    }
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
      group: h.group || "默认分组",
      port: h.port || 22,
      authMethod: h.authMethod || "password",
      encoding: h.encoding || "UTF-8",
      termType: h.termType || "xterm-256color",
      cols: h.cols || 120,
      rows: h.rows || 32,
      timeoutSec: h.timeoutSec || 15,
    }));
  } catch (e) {
    console.error("读取连接档案失败", e);
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
    group: "默认分组",
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
  const prefix = `${base} 副本`;
  let max = 0;
  let seen = false;
  for (const s of sessions.value) {
    const label = sessionLabel(s);
    if (label === base) {
      seen = true;
      continue;
    }
    if (!label.startsWith(prefix)) continue;
    const n = Number.parseInt(label.slice(prefix.length), 10);
    if (Number.isFinite(n)) {
      seen = true;
      if (n > max) max = n;
    }
  }
  return seen ? `${prefix}${max + 1}` : base;
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
    message: "正在连接…",
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
  s.message = "正在连接…";
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
  if (activeId.value === id) {
    activeId.value = sessions.value[0]?.id ?? "";
  }
}

/** 发送按键。syncInput 打开时 ids 为空 => 后端广播到所有连接。 */
export function sendKeys(id: string, data: string) {
  const ids = syncInput.value ? [] : [id];
  SSH.Write(ids, data).catch((e) => console.error("发送失败", e));
}

/** 广播一条命令到所选会话。 */
export async function runBroadcast(command: string) {
  const ids = broadcastTargets();
  if (!ids.length) {
    throw new Error("请先勾选至少一台已连接的会话");
  }
  await SSH.Exec(ids, command);
}

export function resizeSession(id: string, cols: number, rows: number) {
  SSH.Resize([id], cols, rows).catch(() => {});
}