<script setup lang="ts">
// 代码块编辑弹窗。传入 snippet 时打开；保存后通过 submit 事件回传副本。
import { nextTick, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import * as SSH from "../../bindings/shuttle/sshservice";
import { blankSnippet, type Snippet } from "../snippets";
import CodeEditor from "./CodeEditor.vue";

const props = defineProps<{ snippet: Snippet | null }>();
const { t } = useI18n();
const emit = defineEmits<{
  (e: "submit", snippet: Snippet): void;
  (e: "cancel"): void;
}>();

const form = ref<Snippet>(blankSnippet());
const error = ref("");
/** 全屏模式：放大代码编辑器，便于查看和修改长脚本 */
const fullscreen = ref(false);
/** 弹窗内的代码编辑区，退出全屏后把焦点还给这里 */
const codeWrap = ref<HTMLElement | null>(null);

/** 退出全屏，并把焦点放回代码编辑框，避免停留在全屏按钮上 */
function exitFullscreen() {
  fullscreen.value = false;
  nextTick(() => {
    codeWrap.value?.querySelector("textarea")?.focus();
  });
}

/** 全屏时 Esc 只退出全屏，不能关闭整个编辑弹窗 */
function onFullscreenEsc(e: KeyboardEvent) {
  if (e.key !== "Escape") return;
  e.preventDefault();
  e.stopPropagation();
  exitFullscreen();
}
watch(fullscreen, (on) => {
  if (on) window.addEventListener("keydown", onFullscreenEsc, true);
  else window.removeEventListener("keydown", onFullscreenEsc, true);
});
onBeforeUnmount(() => window.removeEventListener("keydown", onFullscreenEsc, true));

watch(
  () => props.snippet,
  (s) => {
    error.value = "";
    form.value = s
      ? { ...s, files: [...(s.files ?? [])], targetDir: s.targetDir ?? "" }
      : blankSnippet();
  },
  { immediate: true },
);

/** 选择本地附件文件，重复路径自动去重 */
async function pickFiles() {
  try {
    const paths = await SSH.PickFiles(t("snippetDialog.pickFilesTitle"));
    if (!paths?.length) return;
    form.value.files = [...new Set([...form.value.files, ...paths])];
  } catch (e) {
    error.value = String(e);
  }
}

function removeFile(index: number) {
  form.value.files.splice(index, 1);
}

/** 列表只展示文件名，完整路径放在 title 里 */
function fileName(file: string): string {
  return file.split(/[\\/]/).pop() || file;
}

function submit() {
  if (!form.value.code.trim()) {
    error.value = t("snippetDialog.errCode");
    return;
  }
  if (form.value.files.length && !form.value.targetDir.trim()) {
    error.value = t("snippetDialog.errTargetDir");
    return;
  }
  emit("submit", { ...form.value, targetDir: form.value.targetDir.trim() });
}
</script>

<template>
  <div class="mask" @click.self="emit('cancel')" @keydown.esc="emit('cancel')">
    <div class="dialog">
      <header>
        <h3>{{ snippet ? t("snippetDialog.editTitle") : t("snippetDialog.newTitle") }}</h3>
        <button class="close" :title="t('common.close')" @click="emit('cancel')">
          <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
            <path
              d="M3.5 3.5l9 9M12.5 3.5l-9 9"
              fill="none"
              stroke="currentColor"
              stroke-width="1.2"
              stroke-linecap="round"
            />
          </svg>
        </button>
      </header>

      <div class="form">
        <label>
          <span>{{ t("snippetDialog.nameLabel") }}</span>
          <input v-model="form.name" :placeholder="t('snippetDialog.namePlaceholder')" />
        </label>

        <label>
          <span>{{ t("snippetDialog.codeLabel") }}</span>
          <div class="code-wrap" ref="codeWrap">
            <CodeEditor
              v-model="form.code"
              height="240px"
              :placeholder="t('snippetDialog.codePlaceholder')"
              @keydown.ctrl.enter.prevent="submit"
              @keydown.meta.enter.prevent="submit"
            />
            <button
              type="button"
              class="head-btn fs-open"
              :title="t('snippetDialog.fullscreen')"
              @click="fullscreen = true"
            >
              <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
                <path d="M6 2.5H2.5V6M10 2.5h3.5V6M6 13.5H2.5V10M10 13.5h3.5V10" />
              </svg>
            </button>
          </div>
        </label>

        <label>
          <span>{{ t("snippetDialog.targetDirLabel") }}</span>
          <input
            v-model="form.targetDir"
            :placeholder="t('snippetDialog.targetDirPlaceholder')"
          />
        </label>

        <div class="field">
          <div class="field-head">
            <span>{{ t("snippetDialog.filesLabel") }}</span>
            <button type="button" class="add-file" @click="pickFiles">
              {{ t("snippetDialog.addFile") }}
            </button>
          </div>
          <p v-if="!form.files.length" class="files-empty">
            {{ t("snippetDialog.filesEmpty") }}
          </p>
          <ul v-else class="file-list">
            <li v-for="(file, index) in form.files" :key="file" :title="file">
              <span class="file-name">{{ fileName(file) }}</span>
              <button
                type="button"
                class="file-remove"
                :title="t('common.delete')"
                @click="removeFile(index)"
              >
                ×
              </button>
            </li>
          </ul>
          <p class="files-hint">{{ t("snippetDialog.filesHint") }}</p>
        </div>
      </div>

      <p v-if="error" class="error">{{ error }}</p>

      <footer>
        <span class="hint">{{ t("snippetDialog.hint") }}</span>
        <div class="actions">
          <button class="ghost" @click="emit('cancel')">{{ t("common.cancel") }}</button>
          <button class="primary" @click="submit">{{ snippet ? t("common.save") : t("common.createSave") }}</button>
        </div>
      </footer>
    </div>
  </div>

  <!-- 全屏查看：只把代码编辑区放大到整个窗口，其他表单字段不展示 -->
  <div v-if="fullscreen" class="code-fullscreen">
    <CodeEditor v-model="form.code" :placeholder="t('snippetDialog.codePlaceholder')" />
    <button class="head-btn fs-exit" :title="t('snippetDialog.exitFullscreen')" @click="exitFullscreen">
      <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
        <path d="M2.5 6H6V2.5M13.5 6H10V2.5M2.5 10H6v3.5M13.5 10H10v3.5" />
      </svg>
    </button>
  </div>
</template>

<style scoped>
.mask {
  position: fixed;
  inset: 0;
  background: rgba(8, 10, 14, 0.62);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 400;
}
.dialog {
  width: min(620px, 92vw);
  max-height: 88vh;
  display: flex;
  flex-direction: column;
  background: var(--win-flyout);
  border: 1px solid var(--win-stroke-strong);
  border-radius: 12px;
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.55);
  overflow: hidden;
}
header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px;
  border-bottom: 1px solid var(--win-divider);
}
h3 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--win-text);
}
.head-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  padding: 0 10px;
  border: 1px solid var(--win-stroke);
  border-radius: 6px;
  background: var(--win-fill);
  color: var(--win-text-2);
  font-size: 11.5px;
  cursor: default;
}
.head-btn:hover {
  border-color: var(--win-stroke-strong);
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.head-btn svg {
  fill: none;
  stroke: currentColor;
  stroke-width: 1.2;
  stroke-linecap: round;
}
.close {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border: 0;
  border-radius: var(--win-radius);
  background: transparent;
  color: var(--win-text-2);
  cursor: default;
  font-size: 12px;
}
.close:hover {
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.form {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 18px;
  overflow: auto;
}
/* 全屏查看：仅代码编辑区铺满窗口，右下角悬浮退出按钮 */
.code-fullscreen {
  position: fixed;
  inset: 0;
  z-index: 450;
  display: flex;
  padding: 18px;
  background: var(--win-flyout);
  outline: none;
}
.code-fullscreen :deep(.code-editor) {
  flex: 1;
  min-height: 0;
  height: auto !important;
}
.fs-exit {
  position: fixed;
  right: 40px;
  bottom: 26px;
  z-index: 460;
}
/* 编辑框右下角的全屏入口 */
.code-wrap {
  position: relative;
}
.fs-open {
  position: absolute;
  right: 24px;
  bottom: 8px;
  z-index: 2;
}
/* 全屏按钮统一为小圆形图标，避开编辑框右侧滚动条 */
.fs-open,
.fs-exit {
  justify-content: center;
  width: 26px;
  height: 26px;
  padding: 0;
  border-radius: 50%;
  opacity: 0.9;
}
.fs-open:hover,
.fs-exit:hover {
  opacity: 1;
}
label,
.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  color: var(--win-text-3);
}
.field-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.add-file {
  height: 24px;
  padding: 0 10px;
  border: 1px solid var(--win-stroke);
  border-radius: 6px;
  background: var(--win-card);
  color: var(--win-text-2);
  font-size: 11px;
  cursor: default;
}
.add-file:hover {
  border-color: var(--win-stroke-strong);
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.files-empty {
  margin: 0;
  padding: 8px 10px;
  border: 1px dashed var(--win-stroke);
  border-radius: 8px;
  color: var(--win-text-disabled);
  font-size: 11px;
}
.file-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.file-list li {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 4px 5px 10px;
  border: 1px solid var(--win-stroke);
  border-radius: 8px;
  background: var(--win-fill);
}
.file-name {
  flex: 1;
  overflow: hidden;
  color: var(--win-text);
  font-family: var(--win-mono);
  font-size: 11.5px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.file-remove {
  flex: none;
  width: 22px;
  height: 22px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--win-text-3);
  font-size: 15px;
  line-height: 1;
  cursor: default;
}
.file-remove:hover {
  background: var(--win-fill-hover);
  color: var(--win-err);
}
.files-hint {
  margin: 0;
  color: var(--win-text-3);
  font-size: 11px;
  line-height: 1.6;
}
input {
  height: 32px;
  padding: 0 10px;
  border-radius: 8px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12px;
  outline: none;
  transition: border-color 0.12s ease, box-shadow 0.12s ease;
}
input::placeholder {
  color: var(--win-text-disabled);
}
input:hover:not(:focus) {
  background: var(--win-fill-hover);
}
input:focus {
  border-color: var(--win-accent);
  box-shadow: 0 0 0 3px var(--win-accent-soft);
}
.error {
  margin: 0;
  padding: 0 18px 10px;
  color: var(--win-err);
  font-size: 12px;
}
footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 14px 18px;
  border-top: 1px solid var(--win-divider);
}
.hint {
  color: var(--win-text-3);
  font-size: 11px;
}
.actions {
  display: flex;
  gap: 8px;
}
footer button {
  height: 32px;
  padding: 0 18px;
  border-radius: 8px;
  font-size: 13px;
  cursor: default;
  border: 1px solid transparent;
}
.ghost {
  background: var(--win-fill);
  border-color: var(--win-stroke);
  color: var(--win-text);
}
.ghost:hover {
  background: var(--win-fill-hover);
}
.ghost:active {
  background: var(--win-fill-press);
}
.primary {
  background: var(--win-accent);
  color: var(--win-accent-text);
}
.primary:hover {
  background: var(--win-accent-hover);
}
.primary:active {
  background: var(--win-accent-press);
}
</style>
