<script setup lang="ts">
// Shell 代码编辑框：透明 textarea 叠加在语法高亮层之上，保持原生输入体验。
import { computed, ref } from "vue";
import { highlightShell } from "../highlight";

defineOptions({ inheritAttrs: false });

const props = withDefaults(
  defineProps<{
    modelValue: string;
    placeholder?: string;
    height?: string;
  }>(),
  { placeholder: "", height: "280px" },
);

const emit = defineEmits<{
  (e: "update:modelValue", value: string): void;
}>();

const preEl = ref<HTMLElement>();
const inputEl = ref<HTMLTextAreaElement>();

/** 末尾补换行，避免最后一行被滚动区域裁掉 */
const html = computed(() => highlightShell(props.modelValue) + "\n");

function onInput(e: Event) {
  emit("update:modelValue", (e.target as HTMLTextAreaElement).value);
}

/** 高亮层跟随输入框滚动 */
function syncScroll() {
  const input = inputEl.value;
  const pre = preEl.value;
  if (!input || !pre) return;
  pre.scrollTop = input.scrollTop;
  pre.scrollLeft = input.scrollLeft;
}

function focus() {
  inputEl.value?.focus();
}

defineExpose({ focus });
</script>

<template>
  <div class="code-editor" :style="{ height }">
    <pre ref="preEl" class="hl" aria-hidden="true"><code v-html="html" /></pre>
    <textarea
      ref="inputEl"
      v-bind="$attrs"
      class="input"
      :value="modelValue"
      :placeholder="placeholder"
      spellcheck="false"
      @input="onInput"
      @scroll="syncScroll"
    />
  </div>
</template>

<style scoped>
.code-editor {
  position: relative;
  border: 1px solid var(--win-stroke);
  border-radius: 8px;
  background: var(--win-term);
  overflow: hidden;
  transition: border-color 0.12s ease, box-shadow 0.12s ease;
}
.code-editor:hover {
  border-color: var(--win-stroke-strong);
}
.code-editor:focus-within {
  border-color: var(--win-accent);
  box-shadow: 0 0 0 3px var(--win-accent-soft);
}
.hl,
.input {
  position: absolute;
  inset: 0;
  margin: 0;
  padding: 10px 12px;
  border: 0;
  font-family: var(--win-mono);
  font-size: 12.5px;
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  tab-size: 4;
  scrollbar-gutter: stable;
}
.hl {
  z-index: 0;
  overflow: hidden;
  color: var(--win-text);
  pointer-events: none;
}
.input {
  z-index: 1;
  width: 100%;
  height: 100%;
  background: transparent;
  color: transparent;
  caret-color: var(--win-text);
  resize: none;
  outline: none;
  -webkit-user-select: text;
  user-select: text;
  overflow: auto;
}
.input::placeholder {
  color: var(--win-text-3);
}
.input::selection {
  /* 文字保持透明，避免与下层高亮文字重叠 */
  color: transparent;
  background: rgba(61, 220, 132, 0.28);
}
</style>
