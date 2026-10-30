<script setup lang="ts">
// Shuttle 主界面：顶部 SFTP / 主页 / 搜索，主体在主页、终端、SFTP 之间切换。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
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
/** 顶栏已打开终端列表浮层 */
const sessionMenuOpen = ref(false);
const sessionQuery = ref("");
const sessionInput = ref<HTMLInputElement>();
/** 顶栏会话标签是否放不下（放不下的部分收进「更多」按钮） */
const tabsOverflow = ref(false);
/** 顶栏平铺展示的标签数量，超出部分收进「更多」浮层 */
const visibleTabCount = ref(Number.MAX_SAFE_INTEGER);
const titlebarEl = ref<HTMLElement>();
const sessionsEl = ref<HTMLElement>();
const dragEl = ref<HTMLElement>();
/** 隐藏的等宽副本，用于测量每个标签的自然宽度 */
const tabsGhostEl = ref<HTMLElement>();
/** 「更多」按钮，用于测量它自身占用的宽度 */
const moreBtnEl = ref<HTMLElement>();
let titlebarObserver: ResizeObserver | undefined;

/** 标签间距，需与 .tb-tabs / .tb-sessions 的 gap 保持一致 */
const TAB_GAP = 4;
/** 「更多」按钮未渲染时的宽度估算值 */
const MORE_FALLBACK_WIDTH = 74;

/** 平铺展示的标签：当前打开的会话若被收起，会占用最后一个平铺位，保证始终可见 */
const visibleTabs = computed(() => {
  const list = sessions.value;
  if (!tabsOverflow.value) return list.slice(0, visibleTabCount.value);
  const shown = list.slice(0, visibleTabCount.value);
  const active = list.find((s) => s.id === activeId.value);
  if (!active || shown.some((s) => s.id === active.id)) return shown;
  return shown.length ? [...shown.slice(0, -1), active] : [active];
});
/** 收进「更多」浮层的会话（不含当前打开的会话） */
const hiddenSessions = computed(() => {
  const shown = new Set(visibleTabs.value.map((s) => s.id));
  return sessions.value.filter((s) => !shown.has(s.id));
});

/** 计算能放下多少个标签，剩余部分收进「更多」按钮 */
function updateTabsOverflow() {
  const ghost = tabsGhostEl.value;
  if (!ghost) return;
  const widths = Array.from(ghost.children).map((el) => (el as HTMLElement).offsetWidth);
  if (!widths.length) {
    tabsOverflow.value = false;
    visibleTabCount.value = Number.MAX_SAFE_INTEGER;
    return;
  }
  // 可用宽度 = 当前会话区已占宽度 + 可被挤占的拖拽区宽度（保留 20px 最小拖拽区）
  const available =
    (sessionsEl.value?.offsetWidth ?? 0) + (dragEl.value?.offsetWidth ?? 0) - 20;
  const natural = widths.reduce((sum, w, i) => sum + w + (i ? TAB_GAP : 0), 0);
  const wasOverflow = tabsOverflow.value;

  if (natural <= available) {
    tabsOverflow.value = false;
    visibleTabCount.value = Number.MAX_SAFE_INTEGER;
    sessionMenuOpen.value = false;
    return;
  }

  // 逐个累加标签宽度；当前会话在收起列表里时，最后一个平铺位留给它
  const activeIdx = sessions.value.findIndex((s) => s.id === activeId.value);
  const moreWidth = moreBtnEl.value?.offsetWidth ?? MORE_FALLBACK_WIDTH;
  let prefix = 0;
  let count = 0;
  for (let k = 1; k <= widths.length; k++) {
    // 前 k 个标签的总宽度（当前会话尚未进入前 k 个时，末位换成它的宽度）
    const total = activeIdx >= k ? prefix + widths[activeIdx] : prefix + widths[k - 1];
    const used = total + (k - 1) * TAB_GAP + TAB_GAP + moreWidth;
    if (used > available) break;
    count = k;
    prefix += widths[k - 1];
  }
  // 空间极窄时也至少保留当前打开的会话
  if (!count && activeIdx >= 0) count = 1;
  tabsOverflow.value = true;
  if (visibleTabCount.value !== count) {
    visibleTabCount.value = count;
    sessionMenuOpen.value = false;
  }
  // 「更多」按钮刚由不显示变为显示，等它渲染后按真实宽度再校正一次
  if (!wasOverflow) void nextTick(updateTabsOverflow);
}

/** 会话增删或重命名后重新评估标签是否放得下 */
watch(
  () => sessions.value.map((s) => sessionLabel(s)).join("|"),
  () => void nextTick(updateTabsOverflow),
);

/** 切换当前终端后，需要重新分配平铺位置以保证它始终可见 */
watch(activeId, () => void nextTick(updateTabsOverflow));

/** 页面切换会增减顶栏按钮，可用宽度随之变化 */
watch(page, () => void nextTick(updateTabsOverflow));

/** 点击页面空白处收起浮层 */
function closePopups() {
  hostMenu.value = null;
  sessionMenuOpen.value = false;
  if (!searchOpen.value) return;
  searchOpen.value = false;
  // 顶栏搜索与主页搜索共用 query，退出顶栏搜索时清空，避免主页被残留过滤
  if (page.value !== "home") query.value = "";
}

function toggleSearch() {
  searchOpen.value = !searchOpen.value;
}

/** 会话列表浮层开关，与服务器搜索浮层互斥 */
function toggleSessionMenu() {
  searchOpen.value = false;
  sessionMenuOpen.value = !sessionMenuOpen.value;
}

watch(sessionMenuOpen, (open) => {
  if (open) void nextTick(() => sessionInput.value?.focus());
  else sessionQuery.value = "";
});

/** 已打开终端按关键词过滤：备注名、IP、用户名 */
const filteredSessions = computed(() => {
  const q = sessionQuery.value.trim().toLowerCase();
  if (!q) return sessions.value;
  return sessions.value.filter((s) =>
    [sessionLabel(s), s.host, s.user].some((v) => (v || "").toLowerCase().includes(q)),
  );
});

/** 从会话列表跳到对应终端 */
function pickSession(id: string) {
  sessionMenuOpen.value = false;
  openSession(id);
}

function pickHost(h: HostConfig) {
  searchOpen.value = false;
  query.value = "";
  connectHost(h);
}

/** 顶栏搜索框回车：打开列表第一条 */
function openFirstMatch() {
  const first = filteredHosts.value[0];
  if (first) pickHost(first);
}

watch(searchOpen, (open) => {
  if (open) void nextTick(() => searchInput.value?.focus());
});

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

/** 主页列表数据：按分组聚合 */
const homeSections = computed(() => groupedHosts.value.filter((g) => g.hosts.length));

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
  // 窗口尺寸变化时重新判断顶栏会话标签是否放得下
  if (typeof ResizeObserver !== "undefined" && titlebarEl.value) {
    titlebarObserver = new ResizeObserver(() => updateTabsOverflow());
    titlebarObserver.observe(titlebarEl.value);
  }
  void nextTick(updateTabsOverflow);
  try {
    encodings.value = (await SSH.Encodings()) ?? encodings.value;
  } catch {
    /* 使用默认列表 */
  }
  await loadHosts();
});

onBeforeUnmount(() => titlebarObserver?.disconnect());

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

/** 待删除的连接档案：非空时展示自定义删除确认弹窗 */
const pendingDelete = ref<HostConfig | null>(null);
const confirmMaskEl = ref<HTMLElement>();

async function removeHost(host: HostConfig) {
  pendingDelete.value = host;
  await nextTick();
  confirmMaskEl.value?.focus();
}

async function confirmDelete() {
  const host = pendingDelete.value;
  if (!host) return;
  pendingDelete.value = null;
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
  void connect(host);
}

/** 主页卡片操作菜单：当前主机 + 相对视口的坐标 */
const hostMenu = ref<{ host: HostConfig; left: number; top: number } | null>(null);
const hostMenuEl = ref<HTMLElement>();
/** 菜单宽度，用于「更多」按钮右对齐与首次定位 */
const HOST_MENU_WIDTH = 178;

/** 显示菜单并贴边修正坐标，避免超出窗口 */
async function showHostMenu(host: HostConfig, left: number, top: number) {
  hostMenu.value = { host, left, top };
  await nextTick();
  const el = hostMenuEl.value;
  if (!el || !hostMenu.value) return;
  const rect = el.getBoundingClientRect();
  hostMenu.value.left = Math.max(8, Math.min(left, window.innerWidth - rect.width - 8));
  hostMenu.value.top = Math.max(8, Math.min(top, window.innerHeight - rect.height - 8));
}

/** 右键卡片：菜单跟随光标 */
function openHostMenuAtCursor(host: HostConfig, e: MouseEvent) {
  void showHostMenu(host, e.clientX + 2, e.clientY + 2);
}

/** 点击「更多」按钮：菜单右对齐展开在按钮下方 */
function openHostMenuFromButton(host: HostConfig, e: MouseEvent) {
  const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
  void showHostMenu(host, r.right - HOST_MENU_WIDTH, r.bottom + 6);
}

/** 执行菜单动作并收起菜单 */
function runHostAction(action: "connect" | "edit" | "duplicate" | "delete") {
  const host = hostMenu.value?.host;
  hostMenu.value = null;
  if (!host) return;
  if (action === "connect") connectHost(host);
  else if (action === "edit") openEdit(host);
  else if (action === "duplicate") duplicateHost(host);
  else void removeHost(host);
}

/** 点击顶部会话标签：切换焦点并回到终端视图 */
function openSession(id: string) {
  activeId.value = id;
  page.value = "term";
}

function closeSession(id: string) {
  const rest = sessions.value.filter((s) => s.id !== id);
  void disconnect(id);
  if (!rest.length) sessionMenuOpen.value = false;
  if (!rest.length && page.value === "term") page.value = "home";
}
</script>

<template>
  <div class="app" @contextmenu.prevent="closePopups" @click="closePopups">
    <!-- 第一行：品牌 + SFTP + 窗口控制 -->
    <header ref="titlebarEl" class="titlebar">
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

      <!-- 已打开的服务器会话：能放下多少就平铺多少，放不下的收进「更多」按钮 + 搜索浮层 -->
      <div
        v-if="sessions.length"
        ref="sessionsEl"
        class="tb-sessions"
        style="--wails-draggable: no-drag"
      >
        <div v-if="visibleTabs.length" class="tb-tabs">
          <button
            v-for="s in visibleTabs"
            :key="s.id"
            class="tb-tab"
            :class="{ on: page === 'term' && s.id === activeId }"
            :title="`${sessionLabel(s)} · ${s.user}@${s.host}:${s.port}`"
            @click="openSession(s.id)"
          >
            <span class="dot" :class="s.status" />
            <span class="tb-tab-label">{{ sessionLabel(s) }}</span>
            <span class="x" title="断开" @click.stop="closeSession(s.id)">×</span>
          </button>
        </div>

        <button
          v-if="tabsOverflow"
          ref="moreBtnEl"
          class="tb-btn tb-session-btn tb-more"
          :class="{ on: sessionMenuOpen }"
          :title="`还有 ${hiddenSessions.length} 个终端`"
          @click.stop="toggleSessionMenu"
        >
          <svg viewBox="0 0 20 20">
            <rect x="2.6" y="4" width="14.8" height="12" rx="1.8" />
            <path d="M5.8 8.2l2.3 2-2.3 2M10 12.4h4.2" />
          </svg>
          <span class="tb-count">{{ hiddenSessions.length }}</span>
        </button>

        <div v-if="tabsOverflow && sessionMenuOpen" class="search-pop session-pop" @click.stop>
          <div class="sp-input">
            <input
              ref="sessionInput"
              v-model="sessionQuery"
              placeholder="搜索已打开的终端"
              @keydown.esc="sessionMenuOpen = false"
            />
          </div>
          <div class="sp-list">
            <div
              v-for="s in filteredSessions"
              :key="s.id"
              class="sp-item sp-session"
              :class="{ on: page === 'term' && s.id === activeId }"
              :title="sessionLabel(s)"
              @click="pickSession(s.id)"
            >
              <span class="dot" :class="s.status" />
              <span class="sp-name">{{ sessionLabel(s) }}</span>
              <span class="x" title="断开" @click.stop="closeSession(s.id)">×</span>
            </div>
            <p v-if="!filteredSessions.length" class="sp-empty">
              {{ sessionQuery.trim() ? "没有匹配的终端" : "没有已打开的终端" }}
            </p>
          </div>
        </div>

        <!-- 隐藏副本：不参与布局，仅用于测量全部标签的自然宽度 -->
        <div ref="tabsGhostEl" class="tb-tabs tb-tabs-ghost" aria-hidden="true">
          <button v-for="s in sessions" :key="s.id" class="tb-tab" tabindex="-1">
            <span class="dot" :class="s.status" />
            <span class="tb-tab-label">{{ sessionLabel(s) }}</span>
            <span class="x">×</span>
          </button>
        </div>
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
              placeholder="搜索主机"
              @keydown.esc="searchOpen = false"
              @keydown.enter="openFirstMatch"
            />
          </div>
          <!-- 输入关键词搜索服务器，未输入时展示全部 -->
          <div class="sp-list">
            <button
              v-for="h in filteredHosts"
              :key="h.id"
              class="sp-item"
              @click="pickHost(h)"
            >
              <span class="sp-name">{{ h.name || h.host }}</span>
              <span class="sp-meta">{{ h.user }}@{{ h.host }}:{{ h.port }}</span>
            </button>
            <p v-if="!filteredHosts.length" class="sp-empty">
              {{ query.trim() ? "没有匹配的服务器" : "还没有保存的服务器" }}
            </p>
          </div>
        </div>
      </div>

      <div ref="dragEl" class="tb-drag" style="--wails-draggable: drag" />

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
      <input v-model="query" placeholder="搜索主机：IP、备注名或分组" />
      <button class="btn primary new-btn" @click="openNew">新增主机</button>
    </div>

    <!-- 第三行：服务器列表 / 终端 / SFTP -->
    <main class="content">
      <section v-show="page === 'home'" class="list-view">
        <div class="list-head">
          <div class="lh-text">
            <h2>主机</h2>
          </div>
        </div>

        <div v-if="homeSections.length" class="cards">
          <section v-for="g in homeSections" :key="g.name" class="group">
            <header class="group-head">
              <span class="group-name">{{ g.name }}</span>
              <span class="group-count">{{ g.hosts.length }} 台服务器</span>
            </header>

            <div class="group-cards">
              <article
                v-for="h in g.hosts"
                :key="h.id"
                class="card"
                @dblclick="connectHost(h)"
                @contextmenu.prevent.stop="openHostMenuAtCursor(h, $event)"
              >
                <div class="card-top">
                  <span class="avatar" :style="{ background: avatarColor(h.name || h.host) }">
                    {{ initialOf(h.name || h.host) }}
                  </span>
                  <div class="who">
                    <strong>{{ h.name || h.host }}</strong>
                    <small>{{ h.user }}@{{ h.host }}:{{ h.port }}</small>
                  </div>

                  <div class="more">
                    <button
                      class="more-btn"
                      title="更多操作"
                      @click.stop="openHostMenuFromButton(h, $event)"
                    >
                      <svg viewBox="0 0 16 16">
                        <circle cx="3.4" cy="8" r="1.3" />
                        <circle cx="8" cy="8" r="1.3" />
                        <circle cx="12.6" cy="8" r="1.3" />
                      </svg>
                    </button>
                  </div>
                </div>

                <div class="tags">
                  <span class="tag">{{ authLabel(h) }}</span>
                  <span class="tag">{{ h.encoding }}</span>
                </div>
              </article>
            </div>
          </section>
        </div>

        <div v-if="!hosts.length" class="empty-state">
          <h3>还没有保存主机连接</h3>
          <p>点击「新增主机」添加第一条 SSH 连接信息。</p>
          <button class="btn primary" @click="openNew">新增主机</button>
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

      <SftpView v-if="page === 'sftp'" :hosts="hosts" />
    </main>

    <HostDialog
      v-if="dialogOpen"
      :host="editing"
      @submit="submitHost"
      @cancel="dialogOpen = false"
    />

    <!-- 删除确认：自定义弹窗，替代浏览器原生 confirm -->
    <Teleport to="body">
      <div
        v-if="pendingDelete"
        ref="confirmMaskEl"
        class="confirm-mask"
        tabindex="-1"
        @click.self="pendingDelete = null"
        @keydown.esc="pendingDelete = null"
      >
        <div class="confirm-dialog" role="alertdialog" aria-modal="true">
          <div class="confirm-head">
            <span class="confirm-icon">
              <svg viewBox="0 0 16 16" aria-hidden="true">
                <path
                  d="M2.8 4.4h10.4M6.4 4.4V2.9h3.2v1.5M4.4 4.4l.6 8.5a1 1 0 0 0 1 .9h3.9a1 1 0 0 0 1-.9l.6-8.5"
                />
              </svg>
            </span>
            <div class="confirm-text">
              <h3>删除连接</h3>
              <p>
                确定要删除「<strong>{{ pendingDelete.name || pendingDelete.host }}</strong
                >」吗？
              </p>
            </div>
          </div>
          <div class="confirm-actions">
            <button class="btn" @click="pendingDelete = null">取消</button>
            <button class="btn danger" @click="confirmDelete">删除</button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 主页卡片操作菜单：跟随光标或「更多」按钮，统一弹在 body 上避免被卡片裁剪 -->
    <Teleport to="body">
      <div
        v-if="hostMenu"
        ref="hostMenuEl"
        class="host-menu"
        :style="{ left: `${hostMenu.left}px`, top: `${hostMenu.top}px` }"
        @click.stop
        @contextmenu.prevent
      >
        <button class="host-menu-item" @click="runHostAction('connect')">
          <svg viewBox="0 0 16 16">
            <path d="M5.5 2v3.2M10.5 2v3.2M3.6 5.2h8.8v1.9a4.4 4.4 0 0 1-8.8 0V5.2ZM8 11.5V14" />
          </svg>
          连接
        </button>
        <button class="host-menu-item" @click="runHostAction('edit')">
          <svg viewBox="0 0 16 16">
            <path d="M11.3 2.7l2 2-7.4 7.4-2.7.7.7-2.7 7.4-7.4Z" />
          </svg>
          编辑
        </button>
        <button class="host-menu-item" @click="runHostAction('duplicate')">
          <svg viewBox="0 0 16 16">
            <rect x="5.6" y="5.6" width="8" height="8" rx="1.6" />
            <path d="M10.4 3.2H4.2a1.6 1.6 0 0 0-1.6 1.6v6.2" />
          </svg>
          复制
        </button>
        <div class="host-menu-sep" />
        <button class="host-menu-item danger" @click="runHostAction('delete')">
          <svg viewBox="0 0 16 16">
            <path d="M2.8 4.4h10.4M6.4 4.4V2.9h3.2v1.5M4.4 4.4l.6 8.5a1 1 0 0 0 1 .9h3.9a1 1 0 0 0 1-.9l.6-8.5" />
          </svg>
          删除
        </button>
      </div>
    </Teleport>
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
  height: 38px;
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
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.2px;
}
.logo {
  display: grid;
  place-items: center;
  width: 18px;
  height: 18px;
  border-radius: 4px;
  background: var(--win-accent);
  color: #06121f;
  font-size: 12px;
  font-weight: 700;
}
.tb-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 13px;
  border: 1px solid var(--win-stroke);
  border-radius: 7px;
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12.5px;
  font-weight: 600;
  cursor: default;
}
.tb-btn svg {
  width: 15px;
  height: 15px;
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
  width: 30px;
  justify-content: center;
}

/* 顶栏已打开终端：能放下的标签平铺，放不下的收进「更多」按钮 + 数量角标 */
.tb-sessions {
  position: relative;
  display: flex;
  align-items: center;
  gap: 4px;
  flex: 0 1 auto;
  min-width: 0;
}
.tb-tabs {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
  overflow: hidden;
}
/* 隐藏副本：绝对定位、不参与布局，用于测量标签总宽度 */
.tb-tabs-ghost {
  position: absolute;
  left: 0;
  top: 0;
  width: max-content;
  overflow: visible;
  visibility: hidden;
  pointer-events: none;
}
.tb-tab {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: none;
  max-width: 190px;
  height: 28px;
  padding: 0 9px;
  border-radius: 7px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text-2);
  font-size: 12.5px;
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
  width: 16px;
  height: 16px;
  display: grid;
  place-items: center;
  border-radius: 4px;
  color: var(--win-text-3);
  font-size: 14px;
  line-height: 1;
}
.tb-tab .x:hover {
  background: var(--win-fill-press);
  color: var(--win-err);
}
.tb-session-btn {
  gap: 7px;
}
/* 「更多」按钮：只保留图标 + 收起数量，尽量少占宽度 */
.tb-more {
  flex: none;
  gap: 5px;
  padding: 0 8px;
}
.tb-count {
  display: grid;
  place-items: center;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--win-accent-soft);
  color: var(--win-accent);
  font-size: 11px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
/* 「更多」收起列表：弹出在页面正中间 */
.search-pop.session-pop {
  position: fixed;
  top: 50%;
  left: 50%;
  right: auto;
  width: 420px;
  max-width: calc(100vw - 48px);
  transform: translate(-50%, -50%);
}
.sp-session {
  cursor: default;
}
.sp-session.on {
  background: var(--win-fill-press);
}
.sp-session .x {
  flex: none;
  width: 18px;
  height: 18px;
  display: grid;
  place-items: center;
  border-radius: 4px;
  color: var(--win-text-3);
  font-size: 14px;
  line-height: 1;
}
.sp-session .x:hover {
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
.card:hover .more-btn {
  opacity: 1;
}
.more-btn:hover {
  background: var(--win-fill-hover);
  border-color: var(--win-stroke);
  color: var(--win-text);
}
.more-btn svg {
  width: 15px;
  height: 15px;
  fill: currentColor;
}

/* 卡片操作菜单：右键与「更多」按钮共用，跟随界面深色风格 */
.host-menu {
  position: fixed;
  z-index: 120;
  min-width: 178px;
  padding: 5px;
  border: 1px solid var(--win-stroke-strong);
  border-radius: 10px;
  background: var(--win-flyout);
  box-shadow: 0 18px 42px rgba(0, 0, 0, 0.55), 0 2px 8px rgba(0, 0, 0, 0.35);
  animation: host-menu-in 0.11s ease-out;
}
@keyframes host-menu-in {
  from {
    opacity: 0;
    transform: translateY(-3px) scale(0.98);
  }
}
.host-menu-item {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  padding: 7px 9px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--win-text);
  font-size: 12.5px;
  text-align: left;
  cursor: default;
  transition: background 0.1s ease, color 0.1s ease;
}
.host-menu-item svg {
  flex: none;
  width: 14px;
  height: 14px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.3;
  stroke-linecap: round;
  stroke-linejoin: round;
  color: var(--win-text-3);
  transition: color 0.1s ease;
}
.host-menu-item:hover {
  background: var(--win-fill-hover);
}
.host-menu-item:hover svg {
  color: var(--win-text-2);
}
.host-menu-item.danger {
  color: var(--win-err);
}
.host-menu-item.danger svg {
  color: var(--win-err);
  opacity: 0.8;
}
.host-menu-item.danger:hover {
  background: rgba(255, 107, 107, 0.1);
}
.host-menu-sep {
  height: 1px;
  margin: 4px 6px;
  background: var(--win-stroke);
}

/* 删除确认弹窗：沿用深色浮层风格，避免浏览器原生 confirm 破坏观感 */
.confirm-mask {
  position: fixed;
  inset: 0;
  z-index: 200;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(8, 10, 14, 0.62);
  backdrop-filter: blur(2px);
  outline: none;
  animation: confirm-fade 0.12s ease-out;
}
.confirm-dialog {
  width: min(400px, 92vw);
  padding: 20px;
  border: 1px solid var(--win-stroke-strong);
  border-radius: 14px;
  background: var(--win-flyout);
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.55), 0 2px 8px rgba(0, 0, 0, 0.35);
  animation: confirm-pop 0.14s ease-out;
}
.confirm-head {
  display: flex;
  gap: 12px;
  align-items: flex-start;
}
.confirm-icon {
  flex: none;
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: 10px;
  background: rgba(255, 107, 107, 0.12);
  color: var(--win-err);
}
.confirm-icon svg {
  width: 17px;
  height: 17px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.3;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.confirm-text {
  min-width: 0;
}
.confirm-text h3 {
  margin: 1px 0 6px;
  font-size: 14px;
  font-weight: 600;
  color: var(--win-text);
}
.confirm-text p {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.6;
  color: var(--win-text-2);
  overflow-wrap: anywhere;
}
.confirm-text strong {
  color: var(--win-text);
  font-weight: 600;
}
.confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 18px;
}
.confirm-actions .btn {
  height: 32px;
  padding: 0 18px;
  font-size: 13px;
}
.btn.danger {
  background: var(--win-close);
  border-color: transparent;
  color: #fff;
  font-weight: 600;
}
.btn.danger:hover:not(:disabled) {
  background: #f05256;
}
.btn.danger:active:not(:disabled) {
  background: #cf3b40;
}
@keyframes confirm-fade {
  from {
    opacity: 0;
  }
}
@keyframes confirm-pop {
  from {
    opacity: 0;
    transform: translateY(6px) scale(0.97);
  }
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
  overflow: hidden;
}
.terminals > :deep(.pane) {
  height: 100%;
}
</style>
