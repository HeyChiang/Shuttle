// 界面多语言：默认跟随系统语言，可在设置中切换并记住选择。
import { createI18n } from "vue-i18n";
import en from "./locales/en";
import zhCN from "./locales/zh-CN";
import es from "./locales/es";

export type Locale = "en" | "zh-CN" | "es";

export const localeOptions: ReadonlyArray<{ value: Locale; label: string }> = [
  { value: "en", label: "English" },
  { value: "zh-CN", label: "简体中文" },
  { value: "es", label: "Español" },
];

const STORAGE_KEY = "shuttle.locale";

function detectLocale(): Locale {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (localeOptions.some((o) => o.value === saved)) return saved as Locale;
  } catch {
    /* 忽略无法读取存储的情况 */
  }
  const nav = (navigator.language || "en").toLowerCase();
  if (nav.startsWith("zh")) return "zh-CN";
  if (nav.startsWith("es")) return "es";
  return "en";
}

export const i18n = createI18n({
  legacy: false,
  locale: detectLocale(),
  fallbackLocale: "en",
  messages: { en, "zh-CN": zhCN, es },
});

export function currentLocale(): Locale {
  return i18n.global.locale.value as Locale;
}

/** 切换语言：持久化到 localStorage，同时更新窗口标题与 html lang */
export function setLocale(next: Locale) {
  i18n.global.locale.value = next;
  try {
    localStorage.setItem(STORAGE_KEY, next);
  } catch {
    /* 忽略无法写入存储的情况 */
  }
  document.documentElement.lang = next;
  document.title = t("app.title");
}

export function t(key: string, named?: Record<string, unknown>): string {
  return i18n.global.t(key, named ?? {}) as unknown as string;
}

document.documentElement.lang = currentLocale();
document.title = t("app.title");
