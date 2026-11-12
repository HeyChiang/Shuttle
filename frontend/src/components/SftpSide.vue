<script setup lang="ts">
// SFTP 单侧面板：可在「本地」与「远程服务器」之间切换，两侧互相独立。
// 拖拽投放由目标面板处理：本地 → 上传，远程 → 下载或服务器互传。
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import * as SSH from "../../bindings/shuttle/sshservice";
import type { HostConfig } from "../../bindings/shuttle/internal/sshcore/models";
import { endSftpDrag, sftpDrag } from "../sftpDrag";
import LocalBrowser from "./LocalBrowser.vue";
import RemoteBrowser from "./RemoteBrowser.vue";

const props = defineProps<{
  /** L / R，用来给两侧分配独立的 SFTP 连接标识 */
  side: "L" | "R";
  hosts: HostConfig[];
  kind: "local" | "remote";
  hostId: string;
}>();

const emit = defineEmits<{
  (e: "update:kind", v: "local" | "remote"): void;
  (e: "update:hostId", v: string): void;
}>();

type Kind = "local" | "remote";

const { t } = useI18n();

const host = computed(() => props.hosts.find((h) => h.id === props.hostId));
const hostLabel = computed(() => (host.value ? host.value.name || host.value.host : ""));

const token = computed(() =>
  props.kind === "remote" && props.hostId ? `sftp:${props.hostId}:${props.side}` : "",
);

const connected = ref(false);
const connecting = ref(false);
const error = ref("");
const busy = ref(false);
const cwd = ref("");
const localDir = ref("");
/** 远端刷新计数 */
const rev = ref(0);
/** 本地刷新计数 */
const localRev = ref(0);
const picker = ref(false);
const query = ref("");
const queryEl = ref<HTMLInputElement>();
let activeToken = "";
/** 连接代次，避免快速切换时旧连接覆盖新状态 */
let gen = 0;

/** 覆盖确认弹窗：存在同名文件时等待用户选择，取消则中止传输 */
interface ConflictState {
  dir: string;
  files: string[];
  resolve: (ok: boolean) => void;
}

const conflict = ref<ConflictState | null>(null);
const conflictEl = ref<HTMLElement>();

function settleConflict(ok: boolean) {
  if (!conflict.value) return;
  const pending = conflict.value;
  conflict.value = null;
  pending.resolve(ok);
}

const filteredHosts = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) return props.hosts;
  return props.hosts.filter((h) =>
    [h.name, h.host, h.group, h.user].some((v) => (v || "").toLowerCase().includes(q)),
  );
});

async function closeToken(t: string) {
  try {
    await SSH.SftpClose(t);
  } catch {
    /* 连接可能已不存在 */
  }
}

async function ensure(tk: string) {
  const h = host.value;
  if (!tk || !h) return;
  const g = ++gen;
  connecting.value = true;
  error.value = "";
  try {
    await SSH.SftpOpen({ ...h, sessionId: tk }, tk);
    // 期间用户已经切换了目标，关闭这条过期的连接
    if (g !== gen) {
      void closeToken(tk);
      return;
    }
    activeToken = tk;
    connected.value = true;
  } catch (e) {
    if (g !== gen) return;
    error.value = t("sftp.connectFailed", { host: hostLabel.value, error: String(e) });
  } finally {
    if (g === gen) connecting.value = false;
  }
}

function setKind(kind: Kind) {
  if (kind === props.kind) return;
  emit("update:kind", kind);
  if (kind === "remote") {
    if (props.hostId) picker.value = false;
    else openPicker();
  } else {
    picker.value = false;
  }
}

function openPicker() {
  query.value = "";
  picker.value = true;
  void nextTick(() => queryEl.value?.focus());
}

function chooseHost(h: HostConfig) {
  picker.value = false;
  query.value = "";
  if (h.id !== props.hostId) {
    emit("update:hostId", h.id);
    return;
  }
  if (!connected.value) void ensure(token.value);
}

function pickFirst() {
  const first = filteredHosts.value[0];
  if (first) chooseHost(first);
}

function cancelPicker() {
  if (!props.hostId) return;
  picker.value = false;
}

watch(
  token,
  (t, old) => {
    gen += 1;
    if (old && old !== t) {
      if (activeToken === old) activeToken = "";
      void closeToken(old);
    }
    connected.value = false;
    connecting.value = false;
    cwd.value = "";
    error.value = "";
    settleConflict(false);
    if (t) {
      picker.value = false;
      void ensure(t);
      return;
    }
    picker.value = props.kind === "remote";
  },
  { immediate: true },
);

watch(
  () => props.kind,
  (kind) => {
    if (kind === "local") picker.value = false;
    else if (!props.hostId) openPicker();
  },
);

watch(picker, (open) => {
  if (open) void nextTick(() => queryEl.value?.focus());
});

// ------------------------------------------------------------- 传输动作

/** 执行一次传输；成功时只刷新目录，不再弹出完成提示 */
async function run(work: () => Promise<void>, after?: () => void) {
  if (busy.value) return;
  error.value = "";
  busy.value = true;
  try {
    await work();
    after?.();
  } catch (e) {
    error.value = t("sftp.transferFailed", { error: String(e) });
  } finally {
    busy.value = false;
  }
}

function baseName(p: string): string {
  const parts = p.split(/[\\/]/);
  return parts[parts.length - 1] || p;
}

/** 目标目录中与本次拖拽文件同名的条目 */
async function conflictsIn(paths: string[], dir: string): Promise<string[]> {
  let names: string[] = [];
  if (props.kind === "local") {
    const list = (await SSH.ListLocal(dir)) ?? [];
    names = list.map((e) => e.name);
  } else {
    const t = token.value;
    if (!t) return [];
    const list = (await SSH.SftpList(t, dir)) ?? [];
    names = list.map((e) => e.name);
  }
  const exist = new Set(names);
  return [...new Set(paths.map(baseName))].filter((n) => exist.has(n));
}

async function confirmOverwrite(paths: string[], dir: string): Promise<boolean> {
  let hits: string[] = [];
  try {
    hits = await conflictsIn(paths, dir);
  } catch {
    // 预检失败不阻塞传输，交给实际传输报错
    return true;
  }
  if (!hits.length) return true;
  return new Promise<boolean>((resolve) => {
    conflict.value = { dir, files: hits, resolve };
    void nextTick(() => conflictEl.value?.focus());
  });
}

async function onDropFiles(paths: string[], dir: string) {
  const src = sftpDrag.value;
  endSftpDrag();
  if (!src || !paths.length) return;

  if (props.kind === "local") {
    if (src.kind !== "remote") return;
    if (!dir) {
      error.value = t("sftp.localNotReady");
      return;
    }
    if (!(await confirmOverwrite(paths, dir))) return;
    await run(
      () => SSH.SftpDownload(src.token, paths, dir),
      () => {
        localRev.value += 1;
      },
    );
    return;
  }

  const tk = token.value;
  if (!connected.value || !tk || !dir) return;
  if (src.kind === "local") {
    if (!(await confirmOverwrite(paths, dir))) return;
    await run(
      () => SSH.SftpUpload(tk, dir, paths),
      () => {
        rev.value += 1;
      },
    );
    return;
  }
  if (src.token === tk) return;
  if (!(await confirmOverwrite(paths, dir))) return;
  await run(
    () => SSH.SftpTransfer(src.token, tk, paths, dir),
    () => {
      rev.value += 1;
    },
  );
}

onBeforeUnmount(() => {
  gen += 1;
  settleConflict(false);
  if (activeToken) void closeToken(activeToken);
});
</script>

<template>
  <section class="side">
    <header class="side-head">
      <div class="kind-seg">
        <button :class="{ on: kind === 'local' }" @click="setKind('local')">
          {{ t("common.local") }}
        </button>
        <button :class="{ on: kind === 'remote' }" @click="setKind('remote')">
          {{ t("common.remote") }}
        </button>
      </div>

      <template v-if="kind === 'remote'">
        <button v-if="host" class="host-chip" :title="`${hostLabel} · ${host.user}@${host.host}:${host.port}`" @click="openPicker">
          <span class="dot" :class="{ on: connected, wait: connecting }" />
          <span class="host-name">{{ hostLabel }}</span>
          <span class="host-change">{{ t("sftp.change") }}</span>
        </button>
      </template>

      <span class="side-hint">{{ busy ? t("sftp.busy") : "" }}</span>
    </header>

    <div class="side-body">
      <div v-if="kind === 'remote' && picker" class="picker">
        <div class="pk-search">
          <input
            ref="queryEl"
            v-model="query"
            :placeholder="t('sftp.pickerPlaceholder')"
            @keydown.esc="cancelPicker"
            @keydown.enter="pickFirst"
          />
          <button
            v-if="hostId"
            class="pk-cancel"
            :title="t('common.close')"
            @click="cancelPicker"
          >
            ×
          </button>
        </div>
        <div class="pk-list">
          <button v-for="h in filteredHosts" :key="h.id" class="pk-item" @click="chooseHost(h)">
            <span class="pk-name">{{ h.name || h.host }}</span>
            <span class="pk-meta">{{ h.user }}@{{ h.host }}:{{ h.port }}</span>
          </button>
          <p v-if="!filteredHosts.length" class="pk-empty">
            {{ hosts.length ? t("search.noMatch") : t("search.noSaved") }}
          </p>
        </div>
      </div>

      <LocalBrowser
        v-else-if="kind === 'local'"
        :initial-dir="localDir"
        :refresh-key="localRev"
        @update:cwd="localDir = $event"
        @drop-files="onDropFiles"
      />

      <template v-else>
        <RemoteBrowser
          v-if="connected && token"
          :key="token + '-' + rev"
          :token="token"
          :initial-dir="cwd"
          @update:cwd="cwd = $event"
          @drop-files="onDropFiles"
        />
        <div v-else class="pane-state">
          <p>{{ connecting ? t("sftp.connectingServer") : error || t("sftp.selectServer") }}</p>
          <button v-if="!connecting && host" class="mini" @click="ensure(token)">
            {{ t("common.retry") }}
          </button>
        </div>
      </template>
    </div>

    <p v-if="error && connected" class="banner err">{{ error }}</p>
  </section>

  <!-- 覆盖确认：自定义弹窗，替代浏览器原生 confirm -->
  <Teleport to="body">
    <div
      v-if="conflict"
      ref="conflictEl"
      class="conflict-mask"
      tabindex="-1"
      @click.self="settleConflict(false)"
      @keydown.esc="settleConflict(false)"
    >
      <div class="conflict-dialog" role="alertdialog" aria-modal="true">
        <div class="conflict-head">
          <span class="conflict-icon">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="M12 3.4L2.7 19.5a1.4 1.4 0 0 0 1.2 2.1h18.2a1.4 1.4 0 0 0 1.2-2.1L13.2 3.4a1.4 1.4 0 0 0-2.4 0z" />
              <path d="M12 9.6v4.6M12 17.9v.1" />
            </svg>
          </span>
          <div class="conflict-text">
            <h3>{{ t("sftp.overwriteTitle") }}</h3>
            <p>{{ t("sftp.overwriteDesc", { dir: conflict.dir }) }}</p>
          </div>
        </div>

        <ul class="conflict-files">
          <li v-for="name in conflict.files.slice(0, 6)" :key="name" :title="name">
            <svg viewBox="0 0 16 16" aria-hidden="true">
              <path d="M4 1.8h5l3 3v10.4H4z" />
              <path d="M9 1.8v3h3" />
            </svg>
            <span>{{ name }}</span>
          </li>
          <li v-if="conflict.files.length > 6" class="conflict-more">
            {{ t("sftp.overwriteMore", { n: conflict.files.length - 6 }) }}
          </li>
        </ul>

        <div class="conflict-actions">
          <button class="ghost" @click="settleConflict(false)">
            {{ t("common.cancel") }}
          </button>
          <button class="primary" @click="settleConflict(true)">
            {{ t("sftp.overwriteConfirm") }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.side {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-height: 0;
  min-width: 0;
}
.side-head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: none;
  min-width: 0;
}
.kind-seg {
  display: flex;
  gap: 2px;
  flex: none;
  padding: 2px;
  border: 1px solid var(--win-stroke);
  border-radius: 9px;
  background: var(--win-fill);
}
.kind-seg button {
  height: 24px;
  padding: 0 12px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--win-text-2);
  font-size: 12px;
  cursor: default;
}
.kind-seg button:hover {
  color: var(--win-text);
}
.kind-seg button.on {
  background: var(--win-fill-press);
  color: var(--win-text);
  font-weight: 600;
}
.host-chip {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  height: 28px;
  padding: 0 10px;
  border: 1px solid var(--win-stroke);
  border-radius: 8px;
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12px;
  cursor: default;
}
.host-chip:hover {
  background: var(--win-fill-hover);
  border-color: var(--win-stroke-strong);
}
.host-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.host-change {
  flex: none;
  color: var(--win-text-3);
  font-size: 11px;
}
.side-hint {
  margin-left: auto;
  color: var(--win-text-3);
  font-size: 11.5px;
  white-space: nowrap;
}
.dot {
  flex: none;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--win-text-3);
}
.dot.on {
  background: var(--win-ok);
}
.dot.wait {
  background: var(--win-warn);
}
.side-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.side-body > :deep(.browser) {
  flex: 1;
}
.picker {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--win-stroke);
  border-radius: var(--win-radius-lg);
  background: var(--win-card);
  overflow: hidden;
}
.pk-search {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px;
  border-bottom: 1px solid var(--win-stroke);
  flex: none;
}
.pk-search input {
  flex: 1;
  min-width: 0;
  height: 32px;
  padding: 0 11px;
  border-radius: 8px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12.5px;
  outline: none;
}
.pk-search input:focus {
  border-color: var(--win-accent);
  box-shadow: 0 0 0 3px var(--win-accent-soft);
}
.pk-cancel {
  flex: none;
  width: 26px;
  height: 26px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--win-text-3);
  font-size: 15px;
  line-height: 1;
  cursor: default;
}
.pk-cancel:hover {
  background: var(--win-fill-press);
  color: var(--win-text);
}
.pk-list {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 4px;
}
.pk-item {
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
.pk-item:hover {
  background: var(--win-fill-hover);
}
.pk-name {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 12.5px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.pk-meta {
  flex: none;
  max-width: 62%;
  color: var(--win-text-3);
  font-size: 11px;
  font-family: var(--win-mono);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.pk-empty {
  margin: 0;
  padding: 14px 10px;
  color: var(--win-text-3);
  font-size: 12px;
  text-align: center;
}
.pane-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  border: 1px dashed var(--win-stroke);
  border-radius: var(--win-radius-lg);
  color: var(--win-text-3);
  font-size: 12px;
}
.pane-state p {
  margin: 0;
  padding: 0 16px;
  text-align: center;
  line-height: 1.6;
}
.mini {
  height: 26px;
  padding: 0 12px;
  border-radius: var(--win-radius);
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12px;
  cursor: default;
}
.mini:hover {
  background: var(--win-fill-hover);
}
.banner {
  margin: 0;
  flex: none;
  font-size: 12px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.banner.err {
  color: var(--win-err);
}
/* 覆盖确认弹窗：与全局删除确认弹窗保持一致的深色浮层风格 */
.conflict-mask {
  position: fixed;
  inset: 0;
  z-index: 300;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(8, 10, 14, 0.62);
  backdrop-filter: blur(2px);
  outline: none;
  animation: conflict-fade 0.12s ease-out;
}
.conflict-dialog {
  width: min(420px, 92vw);
  padding: 20px;
  border: 1px solid var(--win-stroke-strong);
  border-radius: 14px;
  background: var(--win-flyout);
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.55), 0 2px 8px rgba(0, 0, 0, 0.35);
  animation: conflict-pop 0.14s ease-out;
}
.conflict-head {
  display: flex;
  gap: 12px;
  align-items: flex-start;
}
.conflict-icon {
  flex: none;
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: 10px;
  background: rgba(255, 180, 84, 0.14);
  color: var(--win-warn);
}
.conflict-icon svg {
  width: 18px;
  height: 18px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.4;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.conflict-text {
  min-width: 0;
}
.conflict-text h3 {
  margin: 1px 0 6px;
  font-size: 14px;
  font-weight: 600;
  color: var(--win-text);
}
.conflict-text p {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.6;
  color: var(--win-text-2);
  overflow-wrap: anywhere;
}
.conflict-files {
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-height: 168px;
  margin: 14px 0 0;
  padding: 8px;
  overflow: auto;
  border: 1px solid var(--win-stroke);
  border-radius: 10px;
  background: var(--win-fill);
  list-style: none;
}
.conflict-files li {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  padding: 5px 6px;
  color: var(--win-text);
  font-size: 12px;
}
.conflict-files li svg {
  flex: none;
  width: 14px;
  height: 14px;
  fill: none;
  stroke: var(--win-text-3);
  stroke-width: 1.2;
  stroke-linejoin: round;
}
.conflict-files li span {
  min-width: 0;
  overflow: hidden;
  font-family: var(--win-mono);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.conflict-files li.conflict-more {
  color: var(--win-text-3);
  font-size: 11.5px;
}
.conflict-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 18px;
}
.conflict-actions button {
  height: 32px;
  padding: 0 18px;
  border: 1px solid transparent;
  border-radius: 8px;
  font-size: 13px;
  cursor: default;
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
@keyframes conflict-fade {
  from {
    opacity: 0;
  }
}
@keyframes conflict-pop {
  from {
    opacity: 0;
    transform: translateY(6px) scale(0.97);
  }
}
</style>
