<script setup lang="ts">
// SFTP 单侧面板：可在「本地」与「远程服务器」之间切换，两侧互相独立。
// 拖拽投放由目标面板处理：本地 → 上传，远程 → 下载或服务器互传。
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
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

const host = computed(() => props.hosts.find((h) => h.id === props.hostId));
const hostLabel = computed(() => (host.value ? host.value.name || host.value.host : ""));

const token = computed(() =>
  props.kind === "remote" && props.hostId ? `sftp:${props.hostId}:${props.side}` : "",
);

const connected = ref(false);
const connecting = ref(false);
const error = ref("");
const notice = ref("");
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

async function ensure(t: string) {
  const h = host.value;
  if (!t || !h) return;
  const g = ++gen;
  connecting.value = true;
  error.value = "";
  try {
    await SSH.SftpOpen({ ...h, sessionId: t }, t);
    // 期间用户已经切换了目标，关闭这条过期的连接
    if (g !== gen) {
      void closeToken(t);
      return;
    }
    activeToken = t;
    connected.value = true;
  } catch (e) {
    if (g !== gen) return;
    error.value = `连接 ${hostLabel.value} 失败：${String(e)}`;
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
    notice.value = "";
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

async function run(okMessage: () => string, work: () => Promise<void>, after?: () => void) {
  if (busy.value) return;
  error.value = "";
  notice.value = "";
  busy.value = true;
  try {
    await work();
    after?.();
    notice.value = okMessage();
  } catch (e) {
    error.value = `传输失败：${String(e)}`;
  } finally {
    busy.value = false;
  }
}

async function onDropFiles(paths: string[], dir: string) {
  const src = sftpDrag.value;
  endSftpDrag();
  if (!src || !paths.length) return;

  if (props.kind === "local") {
    if (src.kind !== "remote") return;
    if (!dir) {
      error.value = "本地目录尚未就绪";
      return;
    }
    await run(
      () => `已下载 ${paths.length} 个文件到 ${dir}`,
      () => SSH.SftpDownload(src.token, paths, dir),
      () => {
        localRev.value += 1;
      },
    );
    return;
  }

  const t = token.value;
  if (!connected.value || !t || !dir) return;
  if (src.kind === "local") {
    await run(
      () => `已上传 ${paths.length} 个文件到 ${hostLabel.value}:${dir}`,
      () => SSH.SftpUpload(t, dir, paths),
      () => {
        rev.value += 1;
      },
    );
    return;
  }
  if (src.token === t) return;
  await run(
    () => `已传输 ${paths.length} 个文件到 ${hostLabel.value}:${dir}`,
    () => SSH.SftpTransfer(src.token, t, paths, dir),
    () => {
      rev.value += 1;
    },
  );
}

onBeforeUnmount(() => {
  gen += 1;
  if (activeToken) void closeToken(activeToken);
});
</script>

<template>
  <section class="side">
    <header class="side-head">
      <div class="kind-seg">
        <button :class="{ on: kind === 'local' }" @click="setKind('local')">本地</button>
        <button :class="{ on: kind === 'remote' }" @click="setKind('remote')">远程</button>
      </div>

      <template v-if="kind === 'remote'">
        <button v-if="host" class="host-chip" :title="`${hostLabel} · ${host.user}@${host.host}:${host.port}`" @click="openPicker">
          <span class="dot" :class="{ on: connected, wait: connecting }" />
          <span class="host-name">{{ hostLabel }}</span>
          <span class="host-change">更换</span>
        </button>
      </template>

      <span class="side-hint">{{ busy ? "传输中…" : "" }}</span>
    </header>

    <div class="side-body">
      <div v-if="kind === 'remote' && picker" class="picker">
        <div class="pk-search">
          <input
            ref="queryEl"
            v-model="query"
            placeholder="搜索服务器：名称、IP、分组"
            @keydown.esc="cancelPicker"
            @keydown.enter="pickFirst"
          />
          <button v-if="hostId" class="pk-cancel" title="关闭" @click="cancelPicker">×</button>
        </div>
        <div class="pk-list">
          <button v-for="h in filteredHosts" :key="h.id" class="pk-item" @click="chooseHost(h)">
            <span class="pk-name">{{ h.name || h.host }}</span>
            <span class="pk-meta">{{ h.user }}@{{ h.host }}:{{ h.port }}</span>
          </button>
          <p v-if="!filteredHosts.length" class="pk-empty">
            {{ hosts.length ? "没有匹配的服务器" : "还没有保存的服务器" }}
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
          <p>{{ connecting ? "正在连接服务器…" : error || "请选择服务器" }}</p>
          <button v-if="!connecting && host" class="mini" @click="ensure(token)">重试</button>
        </div>
      </template>
    </div>

    <p v-if="error && connected" class="banner err">{{ error }}</p>
    <p v-else-if="notice" class="banner ok">{{ notice }}</p>
  </section>
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
.banner.ok {
  color: var(--win-ok);
}
</style>
