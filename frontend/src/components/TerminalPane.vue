<script setup lang="ts">
// 单个终端面板：xterm.js 实例 + 尺寸同步 + 数据路由。
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from "vue";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import {
  activeId,
  isConnected,
  registerTerminal,
  removeSession,
  resizeSession,
  selectedIds,
  sendKeys,
  sessionLabel,
} from "../store";
import type { SessionInfo } from "../../bindings/shuttle/internal/sshcore/models";

const props = defineProps<{
  session: SessionInfo;
  /** 网格模式下是否可见；单窗口模式下由 activeId 决定 */
  visible: boolean;
}>();

const emit = defineEmits<{ (e: "activate", id: string): void }>();

const paneEl = ref<HTMLDivElement>();
const bodyEl = ref<HTMLDivElement>();
const term = shallowRef<Terminal>();
let fit: FitAddon | undefined;
let unregister: (() => void) | undefined;
let observer: ResizeObserver | undefined;

const connected = computed(() => isConnected(props.session));
const isActive = computed(() => activeId.value === props.session.id);
const checked = computed({
  get: () => selectedIds.has(props.session.id),
  set: (v: boolean) => {
    if (v) selectedIds.add(props.session.id);
    else selectedIds.delete(props.session.id);
  },
});

const statusText = computed(() => {
  switch (props.session.status) {
    case "connected":
      return "已连接";
    case "connecting":
      return "连接中";
    case "error":
      return "连接失败";
    default:
      return "已断开";
  }
});

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

onMounted(() => {
  const instance = new Terminal({
    cursorBlink: true,
    fontSize: 13,
    lineHeight: 1.25,
    scrollback: 8000,
    fontFamily:
      '"JetBrains Mono", "Cascadia Mono", Consolas, "Microsoft YaHei UI", monospace',
    theme: {
      background: "#16181d",
      foreground: "#dfe3ea",
      cursor: "#3ddc84",
      cursorAccent: "#16181d",
      selectionBackground: "#2f5d46",
      // Termius 风格：以品牌绿为主色的中性深色调色板
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
    },
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

  // 终端尺寸变化 -> 通知远端 PTY
  instance.onResize(({ cols, rows }) => resizeSession(props.session.id, cols, rows));

  requestAnimationFrame(() => {
    doFit();
    resizeSession(props.session.id, instance.cols, instance.rows);
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
    if (v) requestAnimationFrame(doFit);
  },
);

watch(
  () => [props.session.status, activeId.value],
  () => requestAnimationFrame(doFit),
);

function focus() {
  term.value?.focus();
}
</script>

<template>
  <section
    class="pane"
    :class="{ active: isActive, offline: !connected }"
    :style="{ display: visible ? 'flex' : 'none' }"
    @mousedown="activate"
  >
    <header class="pane-head">
      <label class="pick" @click.stop>
        <input v-model="checked" type="checkbox" :disabled="!connected" />
      </label>
      <span
        class="dot"
        :class="session.status"
        :title="statusText"
      />
      <div class="titles" @click="activate">
        <strong>{{ sessionLabel(session) }}</strong>
        <small>{{ session.user }}@{{ session.host }}:{{ session.port }}</small>
      </div>
      <span class="enc" :title="'字符编码：' + session.encoding">{{ session.encoding }}</span>
      <button class="icon" title="聚焦终端" @click.stop="focus">
        <svg viewBox="0 0 16 16">
          <circle cx="8" cy="8" r="2.2" />
          <path d="M8 1.5v2.4M8 12.1v2.4M1.5 8h2.4M12.1 8h2.4" />
        </svg>
      </button>
      <button class="icon danger" title="关闭会话" @click.stop="removeSession(session.id)">
        <svg viewBox="0 0 16 16"><path d="M4.2 4.2l7.6 7.6M11.8 4.2l-7.6 7.6" /></svg>
      </button>
    </header>
    <div ref="paneEl" class="pane-body" @click="activate">
      <div ref="bodyEl" class="term" />
      <div v-if="!connected" class="overlay">
        <span>{{ statusText }}</span>
        <small v-if="session.message">{{ session.message }}</small>
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
  border: 1px solid var(--win-stroke);
  border-radius: var(--win-radius-lg);
  overflow: hidden;
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}
.pane.active {
  border-color: rgba(61, 220, 132, 0.5);
  box-shadow: 0 0 0 1px rgba(61, 220, 132, 0.16);
}
.pane-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  background: var(--win-nav);
  border-bottom: 1px solid var(--win-stroke);
  font-size: 12px;
  flex: none;
}
.pane.active .pane-head {
  background: #1f232a;
}
.pick {
  display: flex;
  align-items: center;
}
.pick input {
  accent-color: var(--win-accent);
  cursor: default;
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
  background: var(--win-text-3);
}
.dot.connected {
  background: var(--win-ok);
}
.dot.connecting {
  background: #ffd35c;
  animation: pulse 1s ease-in-out infinite;
}
.dot.error {
  background: #ff6b6b;
}
@keyframes pulse {
  50% {
    opacity: 0.35;
  }
}
.titles {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1;
  cursor: default;
}
.titles strong {
  color: var(--win-text);
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.titles small {
  color: var(--win-text-3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.enc {
  flex: none;
  padding: 1px 6px;
  border-radius: var(--win-radius);
  background: var(--win-fill);
  color: var(--win-text-3);
  font-size: 10px;
}
.icon {
  flex: none;
  width: 24px;
  height: 24px;
  display: grid;
  place-items: center;
  border: 0;
  border-radius: var(--win-radius);
  background: transparent;
  color: var(--win-text-2);
  cursor: default;
}
.icon svg {
  width: 13px;
  height: 13px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.3;
  stroke-linecap: round;
}
.icon:hover {
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.icon.danger:hover {
  background: rgba(255, 107, 107, 0.2);
  color: #ff9b9b;
}
.pane-body {
  position: relative;
  flex: 1;
  min-height: 0;
  padding: 4px 6px 6px;
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
  background: rgba(22, 24, 29, 0.88);
  color: var(--win-err);
  font-size: 13px;
  text-align: center;
  padding: 0 16px;
}
.overlay small {
  color: var(--win-text-3);
  font-size: 11px;
  word-break: break-all;
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
  background-color: rgba(255, 255, 255, 0.16);
  border: 3px solid transparent;
  background-clip: content-box;
  border-radius: 999px;
}
:deep(.xterm-viewport)::-webkit-scrollbar-thumb:hover {
  background-color: rgba(255, 255, 255, 0.26);
}
</style>