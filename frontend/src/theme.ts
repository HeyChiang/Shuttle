// 外观设置：明暗模式与终端字号，持久化到 localStorage。
import { computed, ref, watch } from "vue";

export type ThemeMode = "system" | "light" | "dark";

const THEME_KEY = "shuttle.theme";
const FONT_KEY = "shuttle.termFontSize";

export const TERM_FONT_MIN = 10;
export const TERM_FONT_MAX = 24;
const TERM_FONT_DEFAULT = 13;
const THEME_DEFAULT: ThemeMode = "system";

function read(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* 本地存储不可用时仅本次生效 */
  }
}

const storedMode = read(THEME_KEY);
/** 明暗模式，默认跟随系统 */
export const themeMode = ref<ThemeMode>(
  storedMode === "light" || storedMode === "dark" ? storedMode : "system",
);

const storedFont = Number(read(FONT_KEY));
/** SSH 终端文字大小，单位 px */
export const termFontSize = ref(
  Number.isFinite(storedFont) && storedFont >= TERM_FONT_MIN && storedFont <= TERM_FONT_MAX
    ? Math.round(storedFont)
    : TERM_FONT_DEFAULT,
);

const media = window.matchMedia("(prefers-color-scheme: dark)");
const systemDark = ref(media.matches);
media.addEventListener("change", (e) => {
  systemDark.value = e.matches;
});

/** 实际生效的明暗主题 */
export const isDark = computed(() =>
  themeMode.value === "system" ? systemDark.value : themeMode.value === "dark",
);

watch(
  isDark,
  (dark) => {
    document.documentElement.dataset.theme = dark ? "dark" : "light";
  },
  { immediate: true },
);

watch(themeMode, (mode) => write(THEME_KEY, mode));
watch(termFontSize, (size) => write(FONT_KEY, String(size)));

/** 恢复外观默认值：跟随系统 + 默认字号 */
export function resetAppearance() {
  themeMode.value = THEME_DEFAULT;
  termFontSize.value = TERM_FONT_DEFAULT;
}
