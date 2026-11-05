<script setup lang="ts">
// 代码块编辑弹窗。传入 snippet 时打开；保存后通过 submit 事件回传副本。
import { ref, watch } from "vue";
import { blankSnippet, type Snippet } from "../snippets";
import CodeEditor from "./CodeEditor.vue";

const props = defineProps<{ snippet: Snippet | null }>();
const emit = defineEmits<{
  (e: "submit", snippet: Snippet): void;
  (e: "cancel"): void;
}>();

const form = ref<Snippet>(blankSnippet());
const error = ref("");

watch(
  () => props.snippet,
  (s) => {
    error.value = "";
    form.value = s ? { ...s } : blankSnippet();
  },
  { immediate: true },
);

function submit() {
  if (!form.value.code.trim()) {
    error.value = "请填写代码内容";
    return;
  }
  emit("submit", { ...form.value });
}
</script>

<template>
  <div class="mask" @click.self="emit('cancel')" @keydown.esc="emit('cancel')">
    <div class="dialog">
      <header>
        <h3>{{ snippet ? "编辑代码块" : "新增代码块" }}</h3>
        <button class="close" title="关闭" @click="emit('cancel')">
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
          <span>名称</span>
          <input v-model="form.name" placeholder="例如：磁盘巡检" />
        </label>

        <label>
          <span>代码</span>
          <CodeEditor
            v-model="form.code"
            height="240px"
            placeholder="支持多行脚本，例如：&#10;df -h&#10;free -m"
            @keydown.ctrl.enter.prevent="submit"
            @keydown.meta.enter.prevent="submit"
          />
        </label>
      </div>

      <p v-if="error" class="error">{{ error }}</p>

      <footer>
        <span class="hint">Ctrl + Enter 保存</span>
        <div class="actions">
          <button class="ghost" @click="emit('cancel')">取消</button>
          <button class="primary" @click="submit">{{ snippet ? "保存" : "创建并保存" }}</button>
        </div>
      </footer>
    </div>
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
  z-index: 50;
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
label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  color: var(--win-text-3);
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
