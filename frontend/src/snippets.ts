// 代码块：保存可复用的脚本片段，SSH 终端页可引入后执行。
// 数据量小且无敏感信息，直接持久化到 localStorage。
import { ref, watch } from "vue";

export interface Snippet {
  id: string;
  name: string;
  code: string;
  updatedAt: number;
}

const STORAGE_KEY = "shuttle.snippets";

function readStored(): Snippet[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const list = JSON.parse(raw);
    if (!Array.isArray(list)) return [];
    return list
      .filter((s) => s && typeof s.id === "string")
      .map((s) => ({
        id: s.id,
        name: String(s.name || ""),
        code: String(s.code || ""),
        updatedAt: Number(s.updatedAt) || 0,
      }));
  } catch {
    return [];
  }
}

/** 已保存的代码块，按最近更新排序展示 */
export const snippets = ref<Snippet[]>(readStored());

watch(
  snippets,
  (list) => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(list));
    } catch {
      /* 本地存储不可用时仅本次生效 */
    }
  },
  { deep: true },
);

/** 生成代码块 id。不使用 crypto.randomUUID，兼容非安全上下文的自定义协议。 */
export function newSnippetId(): string {
  return `c${Date.now().toString(36)}${Math.random().toString(36).slice(2, 7)}`;
}

export function blankSnippet(): Snippet {
  return { id: newSnippetId(), name: "", code: "", updatedAt: 0 };
}

/** 新增或更新代码块，未命名时用首行内容兜底 */
export function upsertSnippet(snippet: Snippet) {
  const name = snippet.name.trim() || firstLine(snippet.code) || "未命名代码块";
  const next: Snippet = { ...snippet, name, updatedAt: Date.now() };
  const idx = snippets.value.findIndex((s) => s.id === next.id);
  if (idx >= 0) snippets.value[idx] = next;
  else snippets.value = [next, ...snippets.value];
}

export function deleteSnippet(id: string) {
  snippets.value = snippets.value.filter((s) => s.id !== id);
}

/** 取代码首行，用于列表摘要 */
export function firstLine(code: string): string {
  return code.replace(/\r\n?/g, "\n").split("\n").find((l) => l.trim())?.trim() ?? "";
}
