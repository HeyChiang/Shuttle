<script setup lang="ts">
// Shuttle 主界面：顶部 SFTP / 主页 / 搜索，主体在主页、终端、SFTP 之间切换。
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { Window } from "@wailsio/runtime";
import * as SSH from "../bindings/shuttle/sshservice";
import type { HostConfig, SessionInfo } from "../bindings/shuttle/internal/sshcore/models";
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

/** 同一主机已有会话时用于展示状态 */
function sessionOf(h: HostConfig): SessionInfo | undefined {
  return sessions.value.find(
    (s) => s.host === h.host && s.port === h.port && s.user === h.user,
  );
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
  void connect(host);
}

function closeSession(id: string) {
  const rest = sessions.value.filter((s) => s.id !== id);
  void disconnect(id);
  if (!rest.length) page.value = "home";
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

      <button
        v-if="page !== 'home'"
        class="tb-btn"
        style="--wails-draggable: no-drag"
        title="返回主页"
        @click="page = 'home'"
      >
        <svg viewBox="0 0 20 20">
          <path d="M3.2 9.6L10 4l6.8 5.6" />
          <path d="M5.4 8.8v6.6a1 1 0 0 0 1 1h7.2a1 1 0 0 0 1-1V8.8" />
        </svg>
        主页
      </button>

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
            <svg class="s-ico" viewBox="0 0 16 16">
              <circle cx="7" cy="7" r="4.4" />
              <path d="M10.4 10.4L14 14" />
            </svg>
            <input
              ref="searchInput"
              v-model="query"
              placeholder="搜索已保存的服务器"
              @keydown.esc="searchOpen = false"
            />
          </div>
          <div class="sp-list">
            <button
              v-for="h in filteredHosts"
              :key="h.id"
              class="sp-item"
              @click="pickHost(h)"
            >
              <span
                class="avatar xs"
                :style="{ background: avatarColor(h.name || h.host) }"
              >
                {{ initialOf(h.name || h.host) }}
              </span>
              <span class="sp-who">
                <strong>{{ h.name || h.host }}</strong>
                <small>{{ h.user }}@{{ h.host }}:{{ h.port }}</small>
              </span>
            </button>
            <p v-if="!filteredHosts.length" class="sp-empty">没有匹配的服务器</p>
          </div>
          <button class="sp-new" @click="searchOpen = false; openNew()">
            + 新建服务器
          </button>
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

    <!-- 第二行：搜索（仅主页） -->
    <div v-if="page === 'home'" class="searchbar">
      <svg class="s-ico" viewBox="0 0 16 16">
        <circle cx="7" cy="7" r="4.4" />
        <path d="M10.4 10.4L14 14" />
      </svg>
      <input v-model="query" placeholder="搜索已保存的服务器：IP、备注名或分组" />
      <span class="count">{{ filteredHosts.length }} / {{ hosts.length }}</span>
    </div>

    <!-- 第三行：服务器列表 / 终端 / SFTP -->
    <main class="content">
      <section v-show="page === 'home'" class="list-view">
        <div class="list-head">
          <div class="lh-text">
            <h2>已保存的服务器</h2>
          </div>
          <div class="lh-tools">
            <button v-if="sessions.length" class="btn ghost" @click="page = 'term'">
              返回终端 · {{ sessions.length }}
            </button>
            <button class="btn primary" @click="openNew">新建服务器</button>
          </div>
        </div>

        <div class="cards">
          <article
            v-for="h in filteredHosts"
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
              <span
                v-if="sessionOf(h)"
                class="state"
                :class="sessionOf(h)!.status"
              >
                {{
                  sessionOf(h)!.status === "connected"
                    ? "已连接"
                    : sessionOf(h)!.status === "connecting"
                      ? "连接中"
                      : sessionOf(h)!.status === "error"
                        ? "失败"
                        : "已断开"
                }}
              </span>

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
              <span class="tag">{{ h.group || "默认分组" }}</span>
              <span class="tag">{{ authLabel(h) }}</span>
              <span class="tag">{{ h.encoding }}</span>
            </div>
          </article>
        </div>

        <div v-if="!hosts.length" class="empty-state">
          <h3>还没有保存的服务器</h3>
          <p>点击右上角「新建服务器」添加第一条 SSH 连接信息。</p>
          <button class="btn primary" @click="openNew">新建服务器</button>
        </div>
        <div v-else-if="!filteredHosts.length" class="empty-state">
          <h3>没有匹配的服务器</h3>
          <p>换个 IP 或备注名试试。</p>
        </div>
      </section>

      <section v-show="page === 'term'" class="term-view">
        <div class="term-bar">
          <div class="tabs">
            <button
              v-for="s in sessions"
              :key="s.id"
              class="tab"
              :class="{ on: s.id === activeId }"
              @click="activeId = s.id"
            >
              <span class="dot" :class="s.status" />
              {{ sessionLabel(s) }}
              <span class="x" title="断开" @click.stop="closeSession(s.id)">×</span>
            </button>
          </div>
        </div>

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
  gap: 7px;
  padding: 0 8px;
  flex: none;
}
.sp-input input {
  flex: 1;
  min-width: 0;
  height: 32px;
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
  gap: 9px;
  width: 100%;
  padding: 7px 8px;
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
.sp-who {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.sp-who strong {
  font-size: 12.5px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sp-who small {
  color: var(--win-text-3);
  font-size: 11px;
  font-family: var(--win-mono);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.avatar.xs {
  width: 24px;
  height: 24px;
  border-radius: 6px;
  font-size: 11px;
}
.sp-empty {
  margin: 0;
  padding: 14px 10px;
  color: var(--win-text-3);
  font-size: 12px;
  text-align: center;
}
.sp-new {
  flex: none;
  margin-top: 4px;
  padding: 8px 10px;
  border: 0;
  border-top: 1px solid var(--win-stroke);
  border-radius: 6px;
  background: transparent;
  color: var(--win-text-2);
  font-size: 12px;
  text-align: left;
  cursor: default;
}
.sp-new:hover {
  background: var(--win-fill-hover);
  color: var(--win-text);
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
.searchbar {
  display: flex;
  align-items: center;
  gap: 9px;
  flex: none;
  padding: 10px 18px;
  border-bottom: 1px solid var(--win-stroke);
  background: var(--win-bg);
}
.s-ico {
  flex: none;
  width: 15px;
  height: 15px;
  fill: none;
  stroke: var(--win-text-3);
  stroke-width: 1.3;
  stroke-linecap: round;
}
.searchbar input {
  flex: 1;
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
.searchbar input::placeholder {
  color: var(--win-text-3);
}
.searchbar input:hover {
  background: var(--win-fill-hover);
}
.searchbar input:focus {
  border-color: var(--win-accent);
  box-shadow: 0 0 0 3px var(--win-accent-soft);
}
.searchbar .count {
  flex: none;
  padding: 3px 9px;
  border-radius: var(--win-radius);
  background: var(--win-fill);
  border: 1px solid var(--win-stroke);
  color: var(--win-text-3);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
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
.lh-tools {
  display: flex;
  gap: 8px;
}
.cards {
  flex: 1;
  min-height: 0;
  overflow: auto;
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
.state {
  flex: none;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--win-fill);
  color: var(--win-text-3);
  font-size: 10.5px;
}
.state.connected {
  background: rgba(61, 220, 132, 0.16);
  color: var(--win-ok);
}
.state.connecting {
  background: rgba(255, 180, 84, 0.16);
  color: var(--win-warn);
}
.state.error {
  background: rgba(255, 153, 164, 0.16);
  color: var(--win-err);
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
.term-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: none;
  padding: 8px 12px;
  border-bottom: 1px solid var(--win-stroke);
}
.tabs {
  display: flex;
  align-items: center;
  gap: 4px;
  flex: 1;
  min-width: 0;
  overflow-x: auto;
}
.tab {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: none;
  height: 28px;
  padding: 0 9px;
  border-radius: 8px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text-2);
  font-size: 12px;
  cursor: default;
}
.tab:hover {
  background: var(--win-fill-hover);
}
.tab.on {
  border-color: var(--win-stroke-strong);
  background: var(--win-fill-press);
  color: var(--win-text);
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
.tab .x {
  width: 14px;
  height: 14px;
  display: grid;
  place-items: center;
  border-radius: 4px;
  color: var(--win-text-3);
  font-size: 13px;
  line-height: 1;
}
.tab .x:hover {
  background: var(--win-fill-press);
  color: var(--win-err);
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
