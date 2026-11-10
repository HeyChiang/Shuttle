<script setup lang="ts">
// 单个终端面板：xterm.js 实例 + 尺寸同步 + 数据路由。
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Clipboard } from "@wailsio/runtime";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import {
  activeId,
  isConnected,
  reconnect,
  registerTerminal,
  resizeSession,
  sendKeys,
} from "../store";
import { isDark, termFontSize } from "../theme";
import type { SessionInfo } from "../../bindings/shuttle/internal/sshcore/models";

const props = defineProps<{
  session: SessionInfo;
  /** 网格模式下是否可见；单窗口模式下由 activeId 决定 */
  visible: boolean;
  /** 是否处于合并分屏：显示终端标题栏，可拖回顶部标签栏脱组 */
  merged?: boolean;
}>();

const emit = defineEmits<{
  (e: "activate", id: string): void;
  (e: "close"): void;
  (e: "pane-drag-start", id: string, ev: PointerEvent): void;
}>();

const { t } = useI18n();

const paneEl = ref<HTMLDivElement>();
const bodyEl = ref<HTMLDivElement>();
const term = shallowRef<Terminal>();
let fit: FitAddon | undefined;
let unregister: (() => void) | undefined;
let observer: ResizeObserver | undefined;

const connected = computed(() => isConnected(props.session));

/** 正在建立连接：展示加载圈 */
const connecting = computed(() => props.session.status === "connecting");

const statusText = computed(() => {
  switch (props.session.status) {
    case "connected":
      return t("session.connected");
    case "connecting":
      return t("session.connecting");
    case "error":
      return t("session.failed");
    default:
      return t("session.disconnected");
  }
});

/** 连接失败/已断开时可以手动重连 */
const canRetry = computed(
  () => props.session.status === "error" || props.session.status === "closed",
);

/** 失败原因，仅在可重连状态下展示 */
const failMessage = computed(() => (canRetry.value ? props.session.message : ""));

// Termius 风格：以品牌绿为主色的中性深色调色板
const DARK_TERM_THEME = {
  background: "#16181d",
  foreground: "#dfe3ea",
  cursor: "#3ddc84",
  cursorAccent: "#16181d",
  selectionBackground: "#2f5d46",
  black: "#15171c",
  red: "#ff6b6b",
  green: "#3ddc84",
  yellow: "#ffb454",
  blue: "#5aa9ff",
  magenta: "#c678dd",
  cyan: "#56d4dd",
  white: "#dfe3ea",
  brightBlack: "#6b7380",
  brightRed: "#ff8a8a",
  brightGreen: "#52e694",
  brightYellow: "#ffc978",
  brightBlue: "#7dc0ff",
  brightMagenta: "#d79ae8",
  brightCyan: "#7fe3e3",
  brightWhite: "#f5f7fa",
};

// 明亮主题下的终端配色，保证浅色背景上的可读性
const LIGHT_TERM_THEME = {
  background: "#fbfcfd",
  foreground: "#24292f",
  cursor: "#12a05c",
  cursorAccent: "#fbfcfd",
  selectionBackground: "#bfe3cc",
  black: "#24292f",
  red: "#cf222e",
  green: "#1a7f37",
  yellow: "#9a6700",
  blue: "#0969da",
  magenta: "#8250df",
  cyan: "#1b7c83",
  white: "#6e7781",
  brightBlack: "#57606a",
  brightRed: "#a40e26",
  brightGreen: "#116329",
  brightYellow: "#7d4e00",
  brightBlue: "#0550ae",
  brightMagenta: "#6639ba",
  brightCyan: "#0f6b73",
  brightWhite: "#8c959f",
};

function termTheme() {
  return isDark.value ? DARK_TERM_THEME : LIGHT_TERM_THEME;
}

function retry() {
  void reconnect(props.session.id);
}

function doFit() {
  if (!fit || !paneEl.value || !props.visible) return;
  if (paneEl.value.clientWidth === 0 || paneEl.value.clientHeight === 0) return;
  try {
    fit.fit();
  } catch {
    /* 容器尚未完成布局 */
  }
}

function activate() {
  activeId.value = props.session.id;
  emit("activate", props.session.id);
}

/** 拖动终端标题栏：松手到顶部标签栏即可脱离工作组 */
function onHeadPointerDown(e: PointerEvent) {
  if (!props.merged) return;
  if (e.button !== 0) return;
  if ((e.target as HTMLElement).closest(".pane-close")) return;
  emit("pane-drag-start", props.session.id, e);
}

/** 复制当前选中文本到系统剪贴板 */
async function copySelection() {
  const instance = term.value;
  const text = instance?.getSelection();
  if (!instance || !text) return;
  try {
    await Clipboard.SetText(text);
    instance.clearSelection();
  } catch {
    /* 剪贴板不可用时保持选中状态 */
  }
}

/** 把系统剪贴板内容粘贴到终端当前光标处 */
async function pasteClipboard() {
  const instance = term.value;
  if (!instance) return;
  try {
    const text = await Clipboard.Text();
    if (text) instance.paste(text);
  } catch {
    /* 剪贴板不可用时忽略 */
  }
  instance.focus();
}

/** 右键粘贴：有选中文本时优先粘贴选中内容（PuTTY / Xshell 习惯），否则粘贴系统剪贴板 */
async function onContextMenu(e: MouseEvent) {
  e.preventDefault();
  // 阻止 xterm 内置的右键处理，避免它改写隐藏 textarea 的内容
  e.stopPropagation();
  const instance = term.value;
  if (!instance) return;
  const selection = instance.getSelection();
  if (selection) {
    try {
      await Clipboard.SetText(selection);
    } catch {
      /* 写剪贴板失败时仍然粘贴到终端 */
    }
    instance.paste(selection);
  } else {
    try {
      const text = await Clipboard.Text();
      if (text) instance.paste(text);
    } catch {
      /* 剪贴板不可用时忽略 */
    }
  }
  instance.focus();
}

onMounted(() => {
  const instance = new Terminal({
    cursorBlink: true,
    fontSize: termFontSize.value,
    lineHeight: 1.25,
    scrollback: 8000,
    fontFamily:
      '"JetBrains Mono", "Cascadia Mono", Consolas, "Microsoft YaHei UI", monospace',
    theme: termTheme(),
  });
  fit = new FitAddon();
  instance.loadAddon(fit);
  instance.loadAddon(new WebLinksAddon());
  instance.open(bodyEl.value!);
  term.value = instance;

  // 后端输出 -> 终端
  unregister = registerTerminal(props.session.id, (data) => instance.write(data));

  // 用户按键 -> 后端（同步输入时广播到全部会话）
  instance.onData((data) => sendKeys(props.session.id, data));

  // Ctrl+Shift+C 复制选中内容，Ctrl+V 粘贴剪贴板内容
  instance.attachCustomKeyEventHandler((e) => {
    if (e.type !== "keydown" || !(e.ctrlKey || e.metaKey)) return true;
    const key = e.key.toLowerCase();
    if (e.shiftKey && key === "c") {
      void copySelection();
      return false;
    }
    if (!e.shiftKey && key === "v") {
      void pasteClipboard();
      return false;
    }
    return true;
  });

  // 终端尺寸变化 -> 通知远端 PTY
  instance.onResize(({ cols, rows }) => resizeSession(props.session.id, cols, rows));

  requestAnimationFrame(() => {
    doFit();
    resizeSession(props.session.id, instance.cols, instance.rows);
    if (props.visible) instance.focus();
  });

  if (paneEl.value && typeof ResizeObserver !== "undefined") {
    observer = new ResizeObserver(() => doFit());
    observer.observe(paneEl.value);
  }
});

onBeforeUnmount(() => {
  observer?.disconnect();
  unregister?.();
  term.value?.dispose();
});

watch(
  () => props.visible,
  (v) => {
    if (!v) return;
    // 面板变为可见（新建连接、切换标签、返回终端页）后自动聚焦，无需再手动点击
    requestAnimationFrame(() => {
      doFit();
      focus();
    });
  },
);

watch(
  () => [props.session.status, activeId.value],
  () => requestAnimationFrame(doFit),
);

/** 明暗主题切换时同步终端配色 */
watch(isDark, () => {
  const instance = term.value;
  if (instance) instance.options.theme = termTheme();
});

/** 字号变更后重新计算行列数并上报远端 PTY */
watch(termFontSize, (size) => {
  const instance = term.value;
  if (!instance) return;
  instance.options.fontSize = size;
  requestAnimationFrame(doFit);
});

function focus() {
  term.value?.focus();
}
</script>

<template>
  <section
    class="pane"
    :style="{ display: visible ? 'flex' : 'none' }"
    @mousedown="activate"
  >
    <!-- 分屏时的终端标题栏：显示身份，可拖回顶部标签栏脱离工作组 -->
    <header
      v-if="merged"
      class="pane-head"
      @pointerdown="onHeadPointerDown"
    >
      <span class="pane-dot" :class="session.status" />
      <span class="pane-name">{{ session.name }}</span>
      <span class="pane-host">{{ session.user }}@{{ session.host }}:{{ session.port }}</span>
      <button
        class="pane-close"
        :title="t('session.closePane')"
        draggable="false"
        @mousedown.stop
        @click.stop="emit('close')"
      >
        ×
      </button>
    </header>
    <div ref="paneEl" class="pane-body" @click="activate" @contextmenu.capture="onContextMenu">
      <div ref="bodyEl" class="term" />
      <div v-if="!connected" class="overlay" @click.stop>
        <svg v-if="connecting" class="spinner" viewBox="0 0 48 48" aria-hidden="true">
          <circle class="track" cx="24" cy="24" r="20" />
          <circle class="arc" cx="24" cy="24" r="20" />
        </svg>
        <span class="overlay-title" :class="{ err: !connecting }">{{ statusText }}</span>
        <p v-if="connecting" class="overlay-sub">{{ session.user }}@{{ session.host }}:{{ session.port }}</p>
        <p v-if="failMessage" class="overlay-msg">{{ failMessage }}</p>
        <button v-if="canRetry" class="overlay-retry" @click="retry">{{ t("session.reconnect") }}</button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.pane {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  background: var(--win-term);
  overflow: hidden;
}
/* 分屏终端标题栏 */
.pane-head {
  flex: none;
  display: flex;
  align-items: center;
  gap: 7px;
  height: 30px;
  padding: 0 6px 0 10px;
  border-bottom: 1px solid var(--win-stroke);
  background: var(--win-chrome);
  color: var(--win-text-2);
  font-size: 12px;
  user-select: none;
  cursor: grab;
}
.pane-head:active {
  cursor: grabbing;
}
.pane-dot {
  flex: none;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--win-text-3);
}
.pane-dot.connected {
  background: var(--win-ok);
}
.pane-dot.connecting {
  background: var(--win-warn);
}
.pane-dot.error {
  background: var(--win-err);
}
.pane-name {
  flex: none;
  max-width: 45%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--win-text);
  font-weight: 600;
}
.pane-host {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--win-text-3);
  font-family: var(--win-mono);
  font-size: 11px;
}
/* 鼠标进入终端区域后显示右上角关闭按钮 */
.pane-close {
  flex: none;
  margin-left: auto;
  display: grid;
  place-items: center;
  width: 20px;
  height: 20px;
  border: none;
  border-radius: 5px;
  background: transparent;
  color: var(--win-text-3);
  font-size: 15px;
  line-height: 1;
  cursor: default;
  opacity: 0;
  pointer-events: none;
  transition:
    opacity 0.12s ease,
    background 0.12s ease,
    color 0.12s ease;
}
.pane:hover .pane-close {
  opacity: 1;
  pointer-events: auto;
}
.pane-close:hover {
  background: var(--win-fill-press);
  color: var(--win-err);
}
.pane-body {
  position: relative;
  flex: 1;
  min-height: 0;
  padding: 6px 8px;
}
.term {
  width: 100%;
  height: 100%;
}
.overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: var(--win-overlay);
  color: var(--win-text-2);
  font-size: 13px;
  text-align: center;
  padding: 0 16px;
}
.overlay-title {
  color: var(--win-text);
  font-weight: 600;
}
.overlay-title.err {
  color: var(--win-err);
}
.overlay-sub {
  margin: 0;
  color: var(--win-text-3);
  font-size: 11.5px;
  font-family: var(--win-mono);
}
/* Material 风格不确定进度环 */
.spinner {
  width: 38px;
  height: 38px;
  margin-bottom: 4px;
}
.spinner circle {
  fill: none;
  stroke-width: 4;
  stroke-linecap: round;
  transform-origin: 50% 50%;
}
.spinner .track {
  stroke: var(--win-spinner-track);
}
.spinner .arc {
  stroke: var(--win-accent);
  animation:
    spinner-rotate 1.6s linear infinite,
    spinner-dash 1.4s ease-in-out infinite;
}
@keyframes spinner-rotate {
  to {
    transform: rotate(360deg);
  }
}
@keyframes spinner-dash {
  0% {
    stroke-dasharray: 10, 116;
    stroke-dashoffset: 0;
  }
  50% {
    stroke-dasharray: 90, 36;
    stroke-dashoffset: -18;
  }
  100% {
    stroke-dasharray: 90, 36;
    stroke-dashoffset: -110;
  }
}
.overlay-msg {
  margin: 0;
  max-width: 560px;
  max-height: 40vh;
  overflow: auto;
  color: var(--win-text-2);
  font-size: 12px;
  line-height: 1.55;
  word-break: break-word;
  white-space: pre-wrap;
}
.overlay-retry {
  margin-top: 6px;
  height: 28px;
  padding: 0 14px;
  border: 1px solid transparent;
  border-radius: 7px;
  background: var(--win-accent);
  color: var(--win-accent-text);
  font-size: 12.5px;
  font-weight: 600;
  cursor: default;
}
.overlay-retry:hover {
  background: var(--win-accent-hover);
}
.overlay-retry:active {
  background: var(--win-accent-press);
}
:deep(.xterm-viewport) {
  scrollbar-width: thin;
}
:deep(.xterm-viewport)::-webkit-scrollbar {
  width: 10px;
}
:deep(.xterm-viewport)::-webkit-scrollbar-track {
  background: transparent;
}
:deep(.xterm-viewport)::-webkit-scrollbar-thumb {
  background-color: var(--win-scroll);
  border: 3px solid transparent;
  background-clip: content-box;
  border-radius: 999px;
}
:deep(.xterm-viewport)::-webkit-scrollbar-thumb:hover {
  background-color: var(--win-scroll-hover);
}
</style>