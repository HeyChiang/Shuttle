<script setup lang="ts">
// 代码块：在 SSH 终端页从右侧抽屉快速选择已保存脚本，执行或粘贴到当前会话。
import { computed, ref } from "vue";
import {
  activeId,
  isConnected,
  sendKeys,
  sessionById,
  sessionLabel,
} from "../store";
import { firstLine, snippets, type Snippet } from "../snippets";

const open = ref(false);
const maskEl = ref<HTMLElement>();

/** 当前聚焦会话的名称，用于提示脚本发往哪台服务器 */
const targetName = computed(() => {
  const s = sessionById(activeId.value);
  return s ? sessionLabel(s) : "";
});

/** 当前会话是否已连接，未连接时不允许执行 */
const connected = computed(() => {
  const s = sessionById(activeId.value);
  return !!s && isConnected(s);
});

/** 把脚本逐行回车发送到当前会话，效果等同粘贴到终端后执行 */
function send(script: string, withEnter = true): boolean {
  const s = sessionById(activeId.value);
  const body = script.replace(/\r\n?/g, "\n").trim();
  if (!s || !connected.value || !body) return false;
  sendKeys(s.id, body.replace(/\n/g, "\r") + (withEnter ? "\r" : ""));
  return true;
}

/** 双击列表中的代码块：直接执行并关闭抽屉 */
function runSnippet(snippet: Snippet) {
  if (send(snippet.code)) open.value = false;
}

/** 粘贴到终端但不回车，便于确认后再执行 */
function pasteSnippet(snippet: Snippet) {
  if (send(snippet.code, false)) open.value = false;
}
</script>

<template>
  <div class="code-launcher" style="--wails-draggable: no-drag">
    <button
      class="code-btn"
      :class="{ on: open }"
      title="代码块：从右侧展开，悬停可直接执行或粘贴"
      @click.stop="open = !open"
    >
      <svg viewBox="0 0 20 20">
        <path d="M7.4 5.4L3 10l4.4 4.6" />
        <path d="M12.6 5.4L17 10l-4.4 4.6" />
        <path d="M10.9 3.8l-1.8 12.4" />
      </svg>
    </button>
  </div>

  <Teleport to="body">
    <div
      v-if="open"
      ref="maskEl"
      class="drawer-mask"
      tabindex="-1"
      @click.self="open = false"
      @keydown.esc="open = false"
    >
      <aside class="code-drawer" role="dialog" aria-label="代码块">
        <header class="drawer-head">
          <div class="code-title">
            <strong>代码块</strong>
            <span class="code-target" :class="{ err: !connected }">
              {{ connected ? `发送到 ${targetName}` : "当前终端未连接" }}
            </span>
          </div>
          <button class="code-close" title="关闭" @click="open = false">×</button>
        </header>

        <div class="drawer-body">
          <section class="drawer-section list-section">
            <div class="snippet-list">
              <div
                v-for="s in snippets"
                :key="s.id"
                class="snippet-item"
                :title="s.name"
                @dblclick="runSnippet(s)"
              >
                <span class="snippet-text">
                  <span class="code-item-name">{{ s.name }}</span>
                  <span class="code-item-meta">{{ firstLine(s.code) }}</span>
                </span>
                <div class="item-actions">
                  <button class="act-btn" title="执行" @click="runSnippet(s)">执行</button>
                  <button class="act-btn" title="粘贴到终端" @click="pasteSnippet(s)">
                    粘贴
                  </button>
                </div>
              </div>
              <p v-if="!snippets.length" class="code-empty">
                还没有代码块<br />可在左侧「代码块」中添加
              </p>
            </div>
          </section>
        </div>
      </aside>
    </div>
  </Teleport>
</template>

<style scoped>
.code-launcher {
  flex: none;
  margin-right: 6px;
}
.code-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 28px;
  padding: 0;
  border: 1px solid var(--win-stroke);
  border-radius: 7px;
  background: var(--win-fill);
  color: var(--win-text);
  cursor: default;
}
.code-btn:hover {
  background: var(--win-fill-hover);
  border-color: var(--win-stroke-strong);
}
.code-btn.on {
  background: var(--win-fill-press);
  border-color: var(--win-stroke-strong);
}
.code-btn svg {
  width: 15px;
  height: 15px;
  fill: none;
  stroke: var(--win-accent);
  stroke-width: 1.4;
  stroke-linecap: round;
  stroke-linejoin: round;
}

/* 右侧抽屉：从窗口右边缘滑出 */
.drawer-mask {
  position: fixed;
  top: 38px;
  right: 0;
  bottom: 0;
  left: 0;
  z-index: 200;
  background: rgba(8, 10, 14, 0.4);
  outline: none;
  animation: drawer-fade 0.14s ease-out;
}
.code-drawer {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  display: flex;
  flex-direction: column;
  width: min(460px, 94vw);
  border-left: 1px solid var(--win-stroke-strong);
  background: var(--win-flyout);
  box-shadow: -18px 0 48px rgba(0, 0, 0, 0.42);
  animation: drawer-in 0.18s ease-out;
}
.drawer-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  flex: none;
  padding: 16px 18px 12px;
  border-bottom: 1px solid var(--win-divider);
}
.code-title {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}
.code-title strong {
  font-size: 14px;
  font-weight: 600;
}
.code-target {
  overflow: hidden;
  color: var(--win-text-3);
  font-size: 11.5px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.code-target.err {
  color: var(--win-warn);
}
.code-close {
  display: grid;
  place-items: center;
  flex: none;
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
.code-close:hover {
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.drawer-body {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 14px;
  min-height: 0;
  padding: 8px 6px;
  overflow: hidden;
}
.drawer-section {
  display: flex;
  flex: none;
  flex-direction: column;
  gap: 6px;
}
.list-section {
  flex: 1;
  min-height: 0;
}
/* 代码块列表：直接平铺，不再套外框 */
.snippet-list {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
.snippet-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 8px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--win-text);
  text-align: left;
  cursor: default;
}
.snippet-item:hover {
  background: var(--win-fill-hover);
}
.snippet-item:active {
  background: var(--win-fill-press);
}
.snippet-text {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.code-item-name {
  overflow: hidden;
  font-size: 12px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.code-item-meta {
  overflow: hidden;
  color: var(--win-text-3);
  font-family: var(--win-mono);
  font-size: 10.5px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.item-actions {
  display: flex;
  flex: none;
  gap: 4px;
  opacity: 0;
  transition: opacity 0.12s ease;
}
.snippet-item:hover .item-actions,
.snippet-item:focus-within .item-actions {
  opacity: 1;
}
.act-btn {
  height: 22px;
  padding: 0 8px;
  border: 1px solid var(--win-stroke);
  border-radius: 6px;
  background: var(--win-card);
  color: var(--win-text-2);
  font-size: 11px;
  cursor: default;
}
.act-btn:hover {
  border-color: var(--win-stroke-strong);
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.act-btn:active {
  background: var(--win-fill-press);
}
.code-empty {
  margin: 0;
  padding: 20px 10px;
  color: var(--win-text-3);
  font-size: 11px;
  line-height: 1.7;
  text-align: center;
}
@keyframes drawer-fade {
  from {
    opacity: 0;
  }
}
@keyframes drawer-in {
  from {
    transform: translateX(100%);
  }
}
</style>
