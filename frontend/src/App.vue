<script setup lang="ts">
// Shuttle 主界面：顶部 SFTP / 主页 / 搜索，主体在主页、终端、SFTP 之间切换。
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { Window } from "@wailsio/runtime";
import * as SSH from "../bindings/shuttle/sshservice";
import type { HostConfig } from "../bindings/shuttle/internal/sshcore/models";
import HostDialog from "./components/HostDialog.vue";
import SftpView from "./components/SftpView.vue";
import TerminalPane from "./components/TerminalPane.vue";
import {
  activeId,
  connect,
  disconnect,
  encodings,
  hosts,
  loadHosts,
  persistHosts,
  sessionLabel,
  sessions,
  startEventBridge,
} from "./store";

const query = ref("");
const dialogOpen = ref(false);
const editing = ref<HostConfig | null>(null);
const isMax = ref(false);
/** 当前页面：主页 / SFTP / 终端 */
const page = ref<"home" | "sftp" | "term">("home");
/** 顶栏搜索面板 */
const searchOpen = ref(false);
const searchInput = ref<HTMLInputElement>();
/** 当前展开更多菜单的卡片 id */
const menuFor = ref<string | null>(null);
/** 主页展示模式：分组 / 最近打开 */
const homeMode = ref<"group" | "recent">("group");

function toggleMenu(id: string) {
  menuFor.value = menuFor.value === id ? null : id;
}

/** 点击页面空白处收起浮层 */
function closePopups() {
  menuFor.value = null;
  if (!searchOpen.value) return;
  searchOpen.value = false;
  // 顶栏搜索与主页搜索共用 query，退出顶栏搜索时清空，避免主页被残留过滤
  if (page.value !== "home") query.value = "";
}

function toggleSearch() {
  searchOpen.value = !searchOpen.value;
}

function pickHost(h: HostConfig) {
  searchOpen.value = false;
  query.value = "";
  connectHost(h);
}

/** 顶栏搜索框列表：无关键词时展示最近打开，输入后搜索全部服务器 */
const paletteHosts = computed(() =>
  query.value.trim() ? filteredHosts.value : recentHosts.value,
);

/** 顶栏搜索框回车：打开列表第一条 */
function openFirstMatch() {
  const first = paletteHosts.value[0];
  if (first) pickHost(first);
}

watch(searchOpen, (open) => {
  if (open) void nextTick(() => searchInput.value?.focus());
});

function act(fn: () => void) {
  menuFor.value = null;
  fn();
}

/** 依据名称生成稳定的头像底色 */
const AVATAR_COLORS = ["#3ddc84", "#5aa9ff", "#c678dd", "#ffb454", "#56d4dd", "#ff7b8a"];
function avatarColor(name: string): string {
  let h = 0;
  const text = name || "?";
  for (let i = 0; i < text.length; i += 1) h = (h * 31 + text.charCodeAt(i)) >>> 0;
  return AVATAR_COLORS[h % AVATAR_COLORS.length];
}

function initialOf(name: string): string {
  const t = (name || "?").trim();
  return t ? t[0].toUpperCase() : "?";
}

function authLabel(h: HostConfig): string {
  if (h.authMethod === "key") return "私钥";
  if (h.authMethod === "agent") return "SSH Agent";
  return "密码";
}

const filteredHosts = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) return hosts.value;
  return hosts.value.filter((h) =>
    [h.host, h.name, h.group].some((v) => (v || "").toLowerCase().includes(q)),
  );
});

/** 按分组聚合，保持原有顺序；分组名为空时归入「默认分组」 */
const groupedHosts = computed(() => {
  const groups = new Map<string, HostConfig[]>();
  for (const h of filteredHosts.value) {
    const key = h.group?.trim() || "默认分组";
    const list = groups.get(key);
    if (list) list.push(h);
    else groups.set(key, [h]);
  }
  return [...groups].map(([name, list]) => ({ name, hosts: list }));
});

/* ------------------------------------------------------- 最近打开的服务器 */

const RECENT_KEY = "shuttle.recentHosts";
type RecentEntry = { id: string; at: number };

function readRecent(): RecentEntry[] {
  try {
    const raw = localStorage.getItem(RECENT_KEY);
    const list = raw ? (JSON.parse(raw) as RecentEntry[]) : [];
    return list.filter((e) => e && typeof e.id === "string" && typeof e.at === "number");
  } catch {
    return [];
  }
}

const recentEntries = ref<RecentEntry[]>(readRecent());

/** 记录一次打开；同一服务器只保留最新一条 */
function markRecent(id: string) {
  if (!id) return;
  recentEntries.value = [
    { id, at: Date.now() },
    ...recentEntries.value.filter((e) => e.id !== id),
  ].slice(0, 30);
  try {
    localStorage.setItem(RECENT_KEY, JSON.stringify(recentEntries.value));
  } catch {
    /* 忽略存储失败 */
  }
}

/** 最近打开且仍存在（并符合当前搜索）的服务器，按时间倒序 */
const recentHosts = computed(() => {
  const q = query.value.trim().toLowerCase();
  return recentEntries.value
    .map((e) => hosts.value.find((h) => h.id === e.id))
    .filter((h): h is HostConfig => !!h)
    .filter(
      (h) => !q || [h.host, h.name, h.group].some((v) => (v || "").toLowerCase().includes(q)),
    );
});

/** 主页列表数据：分组模式按分组聚合，最近模式单列一栏 */
const homeSections = computed(() => {
  if (homeMode.value === "recent") {
    return recentHosts.value.length
      ? [{ name: "最近打开", hosts: recentHosts.value, recent: true }]
      : [];
  }
  return groupedHosts.value
    .filter((g) => g.hosts.length)
    .map((g) => ({ ...g, recent: false }));
});

function recentLabel(id: string): string {
  const at = recentEntries.value.find((e) => e.id === id)?.at ?? 0;
  if (!at) return "";
  const diff = Date.now() - at;
  if (diff < 60_000) return "刚刚";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`;
  return new Date(at).toLocaleDateString();
}

async function refreshMax() {
  try {
    isMax.value = await Window.IsMaximised();
  } catch {
    /* 忽略 */
  }
}

function minimise() {
  void Window.Minimise();
}

async function toggleMax() {
  await Window.ToggleMaximise();
  await refreshMax();
}

function closeWin() {
  void Window.Close();
}

onMounted(async () => {
  startEventBridge();
  void refreshMax();
  try {
    encodings.value = (await SSH.Encodings()) ?? encodings.value;
  } catch {
    /* 使用默认列表 */
  }
  await loadHosts();
});

function openNew() {
  editing.value = null;
  dialogOpen.value = true;
}

function openEdit(host: HostConfig) {
  editing.value = { ...host };
  dialogOpen.value = true;
}

async function submitHost(host: HostConfig) {
  const idx = hosts.value.findIndex((h) => h.id === host.id);
  const next = [...hosts.value];
  if (idx >= 0) next[idx] = host;
  else next.push(host);
  await persistHosts(next);
  dialogOpen.value = false;
  editing.value = null;
}

async function removeHost(host: HostConfig) {
  if (!window.confirm(`删除连接档案「${host.name || host.host}」？`)) return;
  await persistHosts(hosts.value.filter((h) => h.id !== host.id));
}

function duplicateHost(host: HostConfig) {
  const copy: HostConfig = {
    ...host,
    id: `${host.id}-copy-${Date.now().toString(36)}`,
    name: `${host.name || host.host} 副本`,
  };
  void persistHosts([...hosts.value, copy]);
}

function connectHost(host: HostConfig) {
  page.value = "term";
  markRecent(host.id);
  void connect(host);
}

/** 点击顶部会话标签：切换焦点并回到终端视图 */
function openSession(id: string) {
  activeId.value = id;
  page.value = "term";
}

function closeSession(id: string) {
  const rest = sessions.value.filter((s) => s.id !== id);
  void disconnect(id);
  if (!rest.length && page.value === "term") page.value = "home";
}
</script>

<template>
  <div class="app" @contextmenu.prevent @click="closePopups">
    <!-- 第一行：品牌 + SFTP + 窗口控制 -->
    <header class="titlebar">
      <div class="tb-brand">
        <span class="logo">⌁</span>
        <strong>Shuttle</strong>
      </div>

      <button
        class="tb-btn"
        style="--wails-draggable: no-drag"
        :class="{ on: page === 'home' }"
        title="主页"
        @click="page = 'home'"
      >
        <svg viewBox="0 0 20 20">
          <path d="M3.2 9.6L10 4l6.8 5.6" />
          <path d="M5.4 8.8v6.6a1 1 0 0 0 1 1h7.2a1 1 0 0 0 1-1V8.8" />
        </svg>
        主页
      </button>

      <button
        class="tb-btn"
        style="--wails-draggable: no-drag"
        :class="{ on: page === 'sftp' }"
        :disabled="!hosts.length"
        :title="hosts.length ? '本地上传 / 远程下载 / 服务器互传' : '请先添加服务器'"
        @click="page = 'sftp'"
      >
        <svg viewBox="0 0 20 20">
          <path d="M10 3v10" />
          <path d="M6.2 7.4L10 3.6l3.8 3.8" />
          <path d="M4 13.4v1.8a1.4 1.4 0 0 0 1.4 1.4h9.2a1.4 1.4 0 0 0 1.4-1.4v-1.8" />
        </svg>
        SFTP
      </button>

      <!-- 已打开的服务器会话标签，常驻顶栏 -->
      <div v-if="sessions.length" class="tb-tabs" style="--wails-draggable: no-drag">
        <button
          v-for="s in sessions"
          :key="s.id"
          class="tb-tab"
          :class="{ on: page === 'term' && s.id === activeId }"
          :title="sessionLabel(s)"
          @click="openSession(s.id)"
        >
          <span class="dot" :class="s.status" />
          <span class="tb-tab-label">{{ sessionLabel(s) }}</span>
          <span class="x" title="断开" @click.stop="closeSession(s.id)">×</span>
        </button>
      </div>

      <!-- 非主页时用「+」弹出服务器搜索 -->
      <div v-if="page !== 'home'" class="tb-search" style="--wails-draggable: no-drag">
        <button
          class="tb-btn tb-plus"
          :class="{ on: searchOpen }"
          title="搜索并连接服务器"
          @click.stop="toggleSearch"
        >
          <svg viewBox="0 0 20 20">
            <path d="M10 4.5v11M4.5 10h11" />
          </svg>
        </button>

        <div v-if="searchOpen" class="search-pop" @click.stop>
          <div class="sp-input">
            <input
              ref="searchInput"
              v-model="query"
              placeholder="搜索服务器"
              @keydown.esc="searchOpen = false"
              @keydown.enter="openFirstMatch"
            />
          </div>
          <!-- 第一行搜索框，下面默认展示最近打开的服务器，输入后搜索全部 -->
          <div class="sp-list">
            <button
              v-for="h in paletteHosts"
              :key="h.id"
              class="sp-item"
              @click="pickHost(h)"
            >
              <span class="sp-name">{{ h.name || h.host }}</span>
              <span class="sp-meta">{{ h.user }}@{{ h.host }}:{{ h.port }}</span>
            </button>
            <p v-if="!paletteHosts.length" class="sp-empty">
              {{ query.trim() ? "没有匹配的服务器" : "还没有最近打开的服务器" }}
            </p>
          </div>
        </div>
      </div>

      <div class="tb-drag" style="--wails-draggable: drag" />

      <div class="tb-controls" style="--wails-draggable: no-drag">
        <button class="win-btn" title="最小化" @click="minimise">
          <svg viewBox="0 0 10 10"><path d="M0 5h10" /></svg>
        </button>
        <button class="win-btn" :title="isMax ? '还原' : '最大化'" @click="toggleMax">
          <svg v-if="!isMax" viewBox="0 0 10 10">
            <rect x="0.5" y="0.5" width="9" height="9" fill="none" />
          </svg>
          <svg v-else viewBox="0 0 10 10">
            <rect x="0.5" y="2.5" width="7" height="7" fill="none" />
            <path d="M2.5 2.5V0.5h7v7h-2" fill="none" />
          </svg>
        </button>
        <button class="win-btn close" title="关闭" @click="closeWin">
          <svg viewBox="0 0 10 10"><path d="M0 0l10 10M10 0L0 10" /></svg>
        </button>
      </div>
    </header>

    <!-- 第二行：搜索 + 新增（仅主页） -->
    <div v-if="page === 'home'" class="home-bar">
      <input v-model="query" placeholder="搜索服务器：IP、备注名或分组" />
      <button class="btn primary new-btn" @click="openNew">新增服务器</button>
    </div>

    <!-- 第三行：服务器列表 / 终端 / SFTP -->
    <main class="content">
      <section v-show="page === 'home'" class="list-view">
        <div class="list-head">
          <div class="lh-text">
            <h2>{{ homeMode === "group" ? "服务器" : "最近打开" }}</h2>
          </div>
          <div class="seg">
            <button :class="{ on: homeMode === 'group' }" @click="homeMode = 'group'">
              分组
            </button>
            <button :class="{ on: homeMode === 'recent' }" @click="homeMode = 'recent'">
              最近打开
            </button>
          </div>
        </div>

        <div v-if="homeSections.length" class="cards">
          <section v-for="g in homeSections" :key="g.name" class="group">
            <header v-if="!g.recent" class="group-head">
              <span class="group-name">{{ g.name }}</span>
              <span class="group-count">{{ g.hosts.length }} 台服务器</span>
            </header>

            <div class="group-cards">
              <article
                v-for="h in g.hosts"
                :key="h.id"
                class="card"
                @dblclick="connectHost(h)"
              >
                <div class="card-top">
                  <span class="avatar" :style="{ background: avatarColor(h.name || h.host) }">
                    {{ initialOf(h.name || h.host) }}
                  </span>
                  <div class="who">
                    <strong>{{ h.name || h.host }}</strong>
                    <small>{{ h.user }}@{{ h.host }}:{{ h.port }}</small>
                  </div>

                  <div class="more" :class="{ open: menuFor === h.id }">
                    <button
                      class="more-btn"
                      title="更多操作"
                      @click.stop="toggleMenu(h.id)"
                    >
                      <svg viewBox="0 0 16 16">
                        <circle cx="3.4" cy="8" r="1.3" />
                        <circle cx="8" cy="8" r="1.3" />
                        <circle cx="12.6" cy="8" r="1.3" />
                      </svg>
                    </button>
                    <div v-if="menuFor === h.id" class="menu" @click.stop>
                      <button @click="act(() => connectHost(h))">连接</button>
                      <button @click="act(() => openEdit(h))">编辑</button>
                      <button @click="act(() => duplicateHost(h))">复制</button>
                      <span class="sep" />
                      <button class="danger" @click="act(() => void removeHost(h))">删除</button>
                    </div>
                  </div>
                </div>

                <div class="tags">
                  <span class="tag">{{ authLabel(h) }}</span>
                  <span class="tag">{{ h.encoding }}</span>
                  <span v-if="g.recent && recentLabel(h.id)" class="tag">
                    {{ recentLabel(h.id) }}
                  </span>
                </div>
              </article>
            </div>
          </section>
        </div>

        <div v-if="!hosts.length" class="empty-state">
          <h3>还没有保存的服务器</h3>
          <p>点击上方「新增服务器」添加第一条 SSH 连接信息。</p>
          <button class="btn primary" @click="openNew">新增服务器</button>
        </div>
        <div v-else-if="!homeSections.length && homeMode === 'recent' && !query.trim()" class="empty-state">
          <h3>还没有最近打开的服务器</h3>
          <p>连接任意服务器后，会按时间顺序记录在这里。</p>
        </div>
        <div v-else-if="!homeSections.length" class="empty-state">
          <h3>没有匹配的服务器</h3>
          <p>换个 IP 或备注名试试。</p>
        </div>
      </section>

      <section v-show="page === 'term'" class="term-view">
        <div class="terminals">
          <TerminalPane
            v-for="s in sessions"
            :key="s.id"
            :session="s"
            :visible="page === 'term' && s.id === activeId"
            @activate="activeId = $event"
          />
        </div>
      </section>

      <SftpView
        v-if="page === 'sftp'"
        :hosts="hosts"
        :default-host-id="filteredHosts[0]?.id"
      />
    </main>

    <HostDialog
      v-if="dialogOpen"
      :host="editing"
      @submit="submitHost"
      @cancel="dialogOpen = false"
    />
  </div>
</template>

<style scoped>
.app {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--win-bg);
  color: var(--win-text);
  overflow: hidden;
  font-size: 13px;
}

/* ------------------------------------------------------------- 第一行 */
.titlebar {
  position: relative;
  z-index: 100;
  display: flex;
  align-items: center;
  gap: 10px;
  height: 32px;
  flex: none;
  padding-left: 10px;
  background: var(--win-chrome);
  user-select: none;
  --wails-draggable: drag;
}
.tb-brand {
  display: flex;
  align-items: center;
  gap: 7px;
  flex: none;
  padding-right: 2px;
}
.tb-brand strong {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.2px;
}
.logo {
  display: grid;
  place-items: center;
  width: 16px;
  height: 16px;
  border-radius: 3px;
  background: var(--win-accent);
  color: #06121f;
  font-size: 11px;
  font-weight: 700;
}
.tb-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 24px;
  padding: 0 11px;
  border: 1px solid var(--win-stroke);
  border-radius: 6px;
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12px;
  font-weight: 600;
  cursor: default;
}
.tb-btn svg {
  width: 14px;
  height: 14px;
  fill: none;
  stroke: var(--win-accent);
  stroke-width: 1.4;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.tb-btn:hover {
  background: var(--win-fill-hover);
  border-color: var(--win-stroke-strong);
}
.tb-btn:active {
  background: var(--win-fill-press);
}
.tb-btn:disabled {
  opacity: 0.45;
}
.tb-btn:disabled svg {
  stroke: var(--win-text-disabled);
}
.tb-btn.on {
  border-color: var(--win-stroke-strong);
  background: var(--win-fill-press);
}
.tb-plus {
  padding: 0;
  width: 26px;
  justify-content: center;
}

/* 顶栏会话标签 */
.tb-tabs {
  display: flex;
  align-items: center;
  gap: 4px;
  flex: 0 1 auto;
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: none;
}
.tb-tabs::-webkit-scrollbar {
  display: none;
}
.tb-tab {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: none;
  max-width: 180px;
  height: 24px;
  padding: 0 8px;
  border-radius: 6px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text-2);
  font-size: 11.5px;
  cursor: default;
}
.tb-tab:hover {
  background: var(--win-fill-hover);
  border-color: var(--win-stroke-strong);
}
.tb-tab.on {
  background: var(--win-fill-press);
  border-color: var(--win-stroke-strong);
  color: var(--win-text);
}
.tb-tab-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tb-tab .x {
  flex: none;
  width: 14px;
  height: 14px;
  display: grid;
  place-items: center;
  border-radius: 4px;
  color: var(--win-text-3);
  font-size: 13px;
  line-height: 1;
}
.tb-tab .x:hover {
  background: var(--win-fill-press);
  color: var(--win-err);
}

/* 顶栏搜索浮层 */
.tb-search {
  position: relative;
  flex: none;
}
.search-pop {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  z-index: 20;
  width: 328px;
  display: flex;
  flex-direction: column;
  padding: 6px;
  border: 1px solid var(--win-stroke-strong);
  border-radius: 10px;
  background: var(--win-flyout);
  box-shadow: 0 16px 36px rgba(0, 0, 0, 0.5);
}
.sp-input {
  display: flex;
  align-items: center;
  flex: none;
  height: 32px;
  padding: 0 9px;
  border: 1px solid var(--win-stroke-strong);
  border-radius: 7px;
  background: var(--win-fill-press);
}
.sp-input input {
  flex: 1;
  min-width: 0;
  height: 28px;
  border: 0;
  background: transparent;
  color: var(--win-text);
  font-size: 12.5px;
  outline: none;
}
.sp-input input::placeholder {
  color: var(--win-text-3);
}
.sp-list {
  max-height: 300px;
  overflow: auto;
  margin-top: 4px;
}
.sp-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 9px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--win-text);
  text-align: left;
  cursor: default;
}
.sp-item:hover {
  background: var(--win-fill-hover);
}
.sp-name {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 12.5px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sp-meta {
  flex: none;
  max-width: 62%;
  color: var(--win-text-3);
  font-size: 11px;
  font-family: var(--win-mono);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sp-empty {
  margin: 0;
  padding: 14px 10px;
  color: var(--win-text-3);
  font-size: 12px;
  text-align: center;
}
.tb-drag {
  flex: 1;
  align-self: stretch;
  min-width: 20px;
}
.tb-controls {
  display: flex;
  align-self: stretch;
  flex: none;
}
.win-btn {
  width: 46px;
  border: 0;
  background: transparent;
  color: var(--win-text-2);
  display: grid;
  place-items: center;
  cursor: default;
}
.win-btn svg {
  width: 10px;
  height: 10px;
  stroke: currentColor;
  stroke-width: 1;
  fill: none;
}
.win-btn:hover {
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.win-btn:active {
  background: var(--win-fill-press);
}
.win-btn.close:hover {
  background: var(--win-close);
  color: #fff;
}

/* ------------------------------------------------------------- 第二行 */
.home-bar {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
  flex: none;
  padding: 10px 18px;
  border-bottom: 1px solid var(--win-stroke);
  background: var(--win-bg);
}
.home-bar input {
  align-self: stretch;
  min-width: 0;
  height: 34px;
  padding: 0 13px;
  border-radius: 9px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 13px;
  outline: none;
  transition: border-color 0.12s ease, box-shadow 0.12s ease;
}
.home-bar input::placeholder {
  color: var(--win-text-3);
}
.home-bar input:hover {
  background: var(--win-fill-hover);
}
.home-bar input:focus {
  border-color: var(--win-accent);
  box-shadow: 0 0 0 3px var(--win-accent-soft);
}
.new-btn {
  height: 32px;
  padding: 0 14px;
}

/* ------------------------------------------------------------- 第三行 */
.content {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.list-view {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 14px 18px 18px;
}
.list-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
  flex: none;
  padding-bottom: 12px;
}
.lh-text h2 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}
/* 分组 / 最近打开 切换 */
.seg {
  display: flex;
  gap: 2px;
  flex: none;
  padding: 2px;
  border: 1px solid var(--win-stroke);
  border-radius: 9px;
  background: var(--win-fill);
}
.seg button {
  height: 24px;
  padding: 0 12px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--win-text-2);
  font-size: 12px;
  cursor: default;
}
.seg button:hover {
  color: var(--win-text);
}
.seg button.on {
  background: var(--win-fill-press);
  color: var(--win-text);
}
.cards {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
.group + .group {
  margin-top: 18px;
}
.group-head {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding-bottom: 8px;
}
.group-name {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--win-text-2);
}
.group-count {
  color: var(--win-text-3);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.group-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  align-content: start;
  gap: 12px;
}
.card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 14px;
  border-radius: 12px;
  border: 1px solid var(--win-stroke);
  background: var(--win-card);
  transition: border-color 0.12s ease, background 0.12s ease, transform 0.12s ease;
}
.card:hover {
  border-color: var(--win-stroke-strong);
  background: var(--win-fill-hover);
  z-index: 2;
}
.card-top {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.avatar {
  flex: none;
  width: 36px;
  height: 36px;
  border-radius: 9px;
  display: grid;
  place-items: center;
  color: rgba(8, 16, 24, 0.86);
  font-size: 15px;
  font-weight: 700;
  text-transform: uppercase;
}
.who {
  flex: 1;
  min-width: 0;
}
.who strong {
  display: block;
  font-size: 13.5px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.who small {
  display: block;
  margin-top: 2px;
  color: var(--win-text-3);
  font-size: 11.5px;
  font-family: var(--win-mono);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.tag {
  padding: 2px 8px;
  border-radius: 6px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text-3);
  font-size: 10.5px;
}
.more {
  position: relative;
  flex: none;
}
.more-btn {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: var(--win-text-3);
  opacity: 0;
  transition: opacity 0.12s ease, background 0.12s ease;
  cursor: default;
}
.card:hover .more-btn,
.more.open .more-btn {
  opacity: 1;
}
.more-btn:hover,
.more.open .more-btn {
  background: var(--win-fill-hover);
  border-color: var(--win-stroke);
  color: var(--win-text);
}
.more-btn svg {
  width: 15px;
  height: 15px;
  fill: currentColor;
}
.menu {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  z-index: 10;
  min-width: 128px;
  display: flex;
  flex-direction: column;
  padding: 4px;
  border: 1px solid var(--win-stroke-strong);
  border-radius: 8px;
  background: var(--win-flyout);
  box-shadow: 0 12px 28px rgba(0, 0, 0, 0.45);
}
.menu button {
  padding: 7px 10px;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--win-text);
  font-size: 12px;
  text-align: left;
  cursor: default;
}
.menu button:hover {
  background: var(--win-fill-hover);
}
.menu button.danger {
  color: var(--win-err);
}
.menu .sep {
  height: 1px;
  margin: 4px 6px;
  background: var(--win-stroke);
}

/* 通用按钮 */
.btn {
  height: 30px;
  padding: 0 12px;
  border-radius: 8px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12px;
  cursor: default;
  white-space: nowrap;
}
.btn:hover:not(:disabled) {
  background: var(--win-fill-hover);
}
.btn:active:not(:disabled) {
  background: var(--win-fill-press);
}
.btn:disabled {
  color: var(--win-text-disabled);
  cursor: not-allowed;
}
.btn.primary {
  background: var(--win-accent);
  border-color: transparent;
  color: var(--win-accent-text);
  font-weight: 600;
}
.btn.primary:hover:not(:disabled) {
  background: var(--win-accent-hover);
}

.empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  text-align: center;
  color: var(--win-text-3);
}
.empty-state h3 {
  margin: 0;
  color: var(--win-text-2);
  font-size: 15px;
  font-weight: 600;
}
.empty-state p {
  margin: 0 0 8px;
  font-size: 12px;
}

/* ------------------------------------------------------------- 终端视图 */
.term-view {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--win-text-3);
  flex: none;
}
.dot.connected {
  background: var(--win-ok);
}
.dot.connecting {
  background: var(--win-warn);
}
.dot.error {
  background: var(--win-err);
}
.terminals {
  flex: 1;
  min-height: 0;
  padding: 8px;
  overflow: auto;
}
.terminals > :deep(.pane) {
  height: 100%;
}
</style>
