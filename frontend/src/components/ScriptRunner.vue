<script setup lang="ts">
// 代码块：在 SSH 终端页从右侧抽屉快速选择已保存脚本，执行或粘贴到当前会话。
import { computed, nextTick, ref } from "vue";
import { useI18n } from "vue-i18n";
import * as SSH from "../../bindings/shuttle/sshservice";
import {
  activeId,
  focusTerminal,
  isConnected,
  sendKeys,
  sessionById,
  sessionLabel,
  transfers,
} from "../store";
import { firstLine, snippets, type Snippet } from "../snippets";

const { t } = useI18n();
const open = ref(false);
const maskEl = ref<HTMLElement>();
/** 附件上传中，避免重复触发执行 */
const busy = ref(false);
/** 上传失败等执行错误 */
const error = ref("");

/** 当前会话正在上传的附件，用于展示实时进度 */
const uploading = computed(() => {
  const id = activeId.value;
  return transfers.value.find(
    (tr) => tr.sessionId === id && tr.direction === "upload" && !tr.done,
  );
});

/** 当前文件的上传百分比 */
const uploadPercent = computed(() => {
  const tr = uploading.value;
  if (!tr || tr.total <= 0) return 0;
  return Math.min(100, Math.round((tr.transferred / tr.total) * 100));
});

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

/** 双击列表中的代码块：先上传附件，再执行并关闭抽屉 */
async function runSnippet(snippet: Snippet) {
  const s = sessionById(activeId.value);
  if (!s || !connected.value || busy.value) return;
  error.value = "";

  const files = snippet.files ?? [];
  const dir = (snippet.targetDir ?? "").trim();
  if (files.length && !dir) {
    error.value = t("runner.needTargetDir");
    return;
  }
  if (files.length) {
    busy.value = true;
    try {
      await SSH.Upload(s.id, dir, files);
    } catch (e) {
      error.value = t("runner.uploadFailed", { error: String(e) });
      return;
    } finally {
      busy.value = false;
    }
  }
  if (send(snippet.code)) closeDrawer();
}

/** 粘贴到终端但不回车，便于确认后再执行 */
function pasteSnippet(snippet: Snippet) {
  if (busy.value) return;
  if (send(snippet.code, false)) closeDrawer();
}

function closeDrawer() {
  open.value = false;
  error.value = "";
  // 抽屉关闭后把焦点还给终端，脚本执行完可以直接继续输入
  const id = activeId.value;
  nextTick(() => focusTerminal(id));
}

function toggleDrawer() {
  if (open.value) closeDrawer();
  else open.value = true;
}
</script>

<template>
  <div class="code-launcher" style="--wails-draggable: no-drag">
    <button
      class="code-btn"
      :class="{ on: open }"
      :title="t('runner.tip')"
      @click.stop="toggleDrawer"
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
      @click.self="closeDrawer"
      @keydown.esc="closeDrawer"
    >
      <aside class="code-drawer" role="dialog" :aria-label="t('runner.title')">
        <header class="drawer-head">
          <div class="code-title">
            <strong>{{ t("runner.title") }}</strong>
            <span class="code-target" :class="{ err: !connected }">
              {{ connected ? t("runner.target", { name: targetName }) : t("runner.disconnected") }}
            </span>
          </div>
          <button class="code-close" :title="t('common.close')" @click="closeDrawer">×</button>
        </header>

        <div v-if="busy" class="run-status">
          <div class="run-line">
            <span class="run-text">
              {{
                uploading
                  ? t("runner.uploadingFile", { name: uploading.name })
                  : t("runner.uploading")
              }}
            </span>
            <span class="run-percent">{{ uploadPercent }}%</span>
          </div>
          <div class="run-bar">
            <div class="run-bar-fill" :style="{ width: uploadPercent + '%' }" />
          </div>
        </div>
        <p v-else-if="error" class="run-error">{{ error }}</p>

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
                  <span class="code-item-meta">
                    {{ firstLine(s.code) }}
                    <template v-if="s.files?.length">
                      · {{ t("runner.attachments", { n: s.files.length }) }}
                    </template>
                  </span>
                </span>
                <div class="item-actions">
                  <button
                    class="act-btn"
                    :title="t('runner.run')"
                    :disabled="busy"
                    @click="runSnippet(s)"
                  >
                    {{ t("runner.run") }}
                  </button>
                  <button
                    class="act-btn"
                    :title="t('runner.paste')"
                    :disabled="busy"
                    @click="pasteSnippet(s)"
                  >
                    {{ t("runner.paste") }}
                  </button>
                </div>
              </div>
              <p v-if="!snippets.length" class="code-empty">
                {{ t("runner.empty") }}<br />{{ t("runner.emptyHint") }}
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
.run-status,
.run-error {
  flex: none;
  margin: 0;
  padding: 8px 18px;
  border-bottom: 1px solid var(--win-divider);
  font-size: 11.5px;
  line-height: 1.5;
}
.run-status {
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: var(--win-text-2);
}
.run-line {
  display: flex;
  align-items: center;
  gap: 8px;
}
.run-text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.run-percent {
  flex: none;
  color: var(--win-text-3);
  font-family: var(--win-mono);
}
.run-bar {
  height: 4px;
  border-radius: 999px;
  background: var(--win-fill);
  overflow: hidden;
}
.run-bar-fill {
  height: 100%;
  background: var(--win-accent);
  transition: width 0.15s linear;
}
.run-error {
  color: var(--win-err);
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
.act-btn:disabled {
  opacity: 0.5;
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
