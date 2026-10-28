<script setup lang="ts">
// SFTP 整页工作区：本地 ⇄ 远程、远程 ⇄ 远程，文件通过拖拽传输。
import { computed, onBeforeUnmount, ref, watch } from "vue";
import * as SSH from "../../bindings/shuttle/sshservice";
import type { HostConfig } from "../../bindings/shuttle/internal/sshcore/models";
import { transfers } from "../store";
import { beginSftpDrag, endSftpDrag, sftpDrag } from "../sftpDrag";
import RemoteBrowser from "./RemoteBrowser.vue";

const props = defineProps<{
  hosts: HostConfig[];
  defaultHostId?: string;
}>();

type Mode = "upload" | "download" | "transfer";

const mode = ref<Mode>("upload");
const srcId = ref(props.defaultHostId || props.hosts[0]?.id || "");
const dstId = ref("");
const srcReady = ref(false);
const dstReady = ref(false);
const srcCwd = ref("");
const dstCwd = ref("");
/** 从远端刷新浏览器的版本号 */
const srcRev = ref(0);
const dstRev = ref(0);
const localFiles = ref<string[]>([]);
const localDir = ref("");
const busy = ref(false);
const error = ref("");
const notice = ref("");

/** 已建立的独立 SFTP 连接（token） */
const opened = new Set<string>();

const tokenOf = (hostId: string) => `sftp:${hostId}`;
const srcToken = computed(() => (srcId.value ? tokenOf(srcId.value) : ""));
const dstToken = computed(() => (dstId.value ? tokenOf(dstId.value) : ""));

function hostById(id: string): HostConfig | undefined {
  return props.hosts.find((h) => h.id === id);
}

function hostOfToken(token: string): HostConfig | undefined {
  return hostById(token.replace(/^sftp:/, ""));
}

function labelOf(id: string): string {
  const h = hostById(id);
  return h ? h.name || h.host : id;
}

function labelOfToken(token: string): string {
  const h = hostOfToken(token);
  return h ? h.name || h.host : token;
}

function baseName(p: string): string {
  const parts = p.split(/[\\/]/);
  return parts[parts.length - 1] || p;
}

function human(size: number): string {
  if (!size) return "0 B";
  if (size < 1024) return `${size} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = size / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  return `${v.toFixed(v >= 100 ? 0 : 1)} ${units[i]}`;
}

const tasks = computed(() =>
  transfers.value.filter((t) => t.sessionId.startsWith("sftp:")).slice(0, 4),
);

const srcLabel = computed(() =>
  mode.value === "upload" ? "目标服务器" : "源服务器",
);

// ------------------------------------------------------------- 连接管理

async function ensure(hostId: string) {
  const token = tokenOf(hostId);
  if (opened.has(token)) return;
  const host = hostById(hostId);
  if (!host) throw new Error("未找到服务器档案");
  await SSH.SftpOpen({ ...host, sessionId: token }, token);
  opened.add(token);
}

async function openSrc() {
  if (!srcId.value) {
    srcReady.value = false;
    return;
  }
  srcReady.value = false;
  try {
    await ensure(srcId.value);
    srcReady.value = true;
  } catch (e) {
    error.value = `连接 ${labelOf(srcId.value)} 失败：${String(e)}`;
  }
}

async function openDst() {
  if (!dstId.value) {
    dstReady.value = false;
    return;
  }
  dstReady.value = false;
  try {
    await ensure(dstId.value);
    dstReady.value = true;
  } catch (e) {
    error.value = `连接 ${labelOf(dstId.value)} 失败：${String(e)}`;
  }
}

// ------------------------------------------------------------- 本地文件

async function pickLocalFiles() {
  error.value = "";
  try {
    const paths = (await SSH.PickFiles("选择要传输的文件")) ?? [];
    if (paths.length) localFiles.value = [...new Set([...localFiles.value, ...paths])];
  } catch (e) {
    error.value = String(e);
  }
}

function removeLocalFile(p: string) {
  localFiles.value = localFiles.value.filter((x) => x !== p);
}

async function pickLocalDir() {
  error.value = "";
  try {
    const dir = await SSH.PickDirectory("选择本地保存目录");
    if (dir) localDir.value = dir;
  } catch (e) {
    error.value = String(e);
  }
}

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

async function uploadFiles(token: string, dir: string, paths: string[]) {
  if (!paths.length || !dir) return;
  await run(
    () => `已上传 ${paths.length} 个文件到 ${labelOfToken(token)}:${dir}`,
    () => SSH.SftpUpload(token, dir, paths),
    () => {
      localFiles.value = localFiles.value.filter((p) => !paths.includes(p));
      if (token === srcToken.value) srcRev.value += 1;
      if (token === dstToken.value) dstRev.value += 1;
    },
  );
}

async function downloadFiles(token: string, paths: string[]) {
  if (!paths.length) return;
  let dir = localDir.value;
  if (!dir) {
    try {
      dir = (await SSH.PickDirectory("选择本地保存目录")) || "";
    } catch (e) {
      error.value = String(e);
      return;
    }
    if (!dir) return;
    localDir.value = dir;
  }
  await run(
    () => `已下载 ${paths.length} 个文件到 ${dir}`,
    () => SSH.SftpDownload(token, paths, dir),
  );
}

async function transferFiles(from: string, to: string, paths: string[], dir: string) {
  if (!paths.length || !dir) return;
  await run(
    () => `已将 ${paths.length} 个文件传到 ${labelOfToken(to)}:${dir}`,
    () => SSH.SftpTransfer(from, to, paths, dir),
    () => {
      dstRev.value += 1;
    },
  );
}

// ------------------------------------------------------------- 拖拽交互

const draggingRemote = computed(() => sftpDrag.value?.kind === "remote");
const dropOverLocal = ref(false);
let localDepth = 0;

function onLocalDragStart(path: string, ev: DragEvent) {
  beginSftpDrag({ kind: "local", paths: [path] });
  if (ev.dataTransfer) {
    ev.dataTransfer.effectAllowed = "copy";
    ev.dataTransfer.setData("text/plain", path);
  }
}

function onLocalDragEnter() {
  if (!draggingRemote.value) return;
  localDepth += 1;
  dropOverLocal.value = true;
}

function onLocalDragOver(ev: DragEvent) {
  if (!draggingRemote.value) return;
  if (ev.dataTransfer) ev.dataTransfer.dropEffect = "copy";
}

function onLocalDragLeave() {
  if (!draggingRemote.value) return;
  localDepth -= 1;
  if (localDepth <= 0) {
    localDepth = 0;
    dropOverLocal.value = false;
  }
}

function onLocalDrop() {
  localDepth = 0;
  dropOverLocal.value = false;
  const src = sftpDrag.value;
  if (!src || src.kind !== "remote") return;
  const paths = [...src.paths];
  endSftpDrag();
  void downloadFiles(src.token, paths);
}

/** 远端面板接收投放：本地文件 → 上传；另一个服务器的文件 → 互传 */
function onRemoteDrop(targetToken: string, targetDir: string, paths: string[]) {
  const src = sftpDrag.value;
  endSftpDrag();
  if (!src) return;
  if (src.kind === "local") {
    void uploadFiles(targetToken, targetDir, src.paths);
    return;
  }
  if (src.token === targetToken) return;
  void transferFiles(src.token, targetToken, src.paths, targetDir);
}

// ------------------------------------------------------------- 生命周期

onBeforeUnmount(() => {
  try {
    void SSH.SftpCloseAll();
  } catch {
    /* 忽略 */
  }
});

watch(
  [srcId, mode],
  () => {
    error.value = "";
    notice.value = "";
    void openSrc();
  },
  { immediate: true },
);

watch([dstId, mode], () => {
  error.value = "";
  if (mode.value !== "transfer" || !dstId.value) {
    dstReady.value = false;
    return;
  }
  void openDst();
});
</script>

<template>
  <section class="sftp-view">
    <header class="sf-bar">
      <nav class="modes">
        <button :class="{ on: mode === 'upload' }" @click="mode = 'upload'">
          上传到远程
        </button>
        <button :class="{ on: mode === 'download' }" @click="mode = 'download'">
          下载到本地
        </button>
        <button :class="{ on: mode === 'transfer' }" @click="mode = 'transfer'">
          服务器互传
        </button>
      </nav>

      <div class="servers">
        <span class="srv-label">{{ srcLabel }}</span>
        <select v-model="srcId">
          <option v-for="h in hosts" :key="h.id" :value="h.id">
            {{ h.name || h.host }} · {{ h.user }}@{{ h.host }}:{{ h.port }}
          </option>
        </select>
        <template v-if="mode === 'transfer'">
          <span class="srv-arrow">→</span>
          <select v-model="dstId">
            <option value="">目标服务器…</option>
            <option v-for="h in hosts" :key="h.id" :value="h.id">
              {{ h.name || h.host }} · {{ h.user }}@{{ h.host }}:{{ h.port }}
            </option>
          </select>
        </template>
      </div>

      <span class="hint">
        {{ busy ? "传输中…" : "把文件拖到另一侧面板即可传输" }}
      </span>
    </header>

    <div class="sf-panes" :class="'mode-' + mode">
      <!-- 本地文件 -->
      <div
        v-if="mode !== 'transfer'"
        class="local-pane"
        :class="{ 'can-drop': draggingRemote, over: dropOverLocal }"
        @dragenter.prevent="onLocalDragEnter"
        @dragover.prevent="onLocalDragOver"
        @dragleave="onLocalDragLeave"
        @drop.prevent="onLocalDrop"
      >
        <div class="lp-head">
          <strong>本地</strong>
          <span class="lp-dir" :title="localDir">
            {{ localDir || "未设置下载目录" }}
          </span>
          <button class="mini" @click="pickLocalFiles">添加文件</button>
          <button class="mini" @click="pickLocalDir">下载目录</button>
        </div>
        <div class="lp-list">
          <div
            v-for="f in localFiles"
            :key="f"
            class="lfile"
            draggable="true"
            :title="f"
            @dragstart="onLocalDragStart(f, $event)"
            @dragend="endSftpDrag"
          >
            <svg class="ico" viewBox="0 0 16 16">
              <path d="M4.2 2.6h4.4l3.2 3.2v7.6H4.2z" />
              <path d="M8.6 2.6v3.2h3.2" />
            </svg>
            <span class="nm">{{ baseName(f) }}</span>
            <button class="rm" title="移除" @click.stop="removeLocalFile(f)">×</button>
          </div>
          <p v-if="!localFiles.length" class="tip">
            点击「添加文件」挑选本地文件，再拖到远程面板上传；把远程文件拖到这里即可下载。
          </p>
        </div>
      </div>

      <!-- 远程：源 / 目标 -->
      <div class="remote-slot">
        <p v-if="mode === 'transfer'" class="slot-head">
          源 · {{ labelOf(srcId) }} · {{ srcCwd || "读取中…" }}
        </p>
        <RemoteBrowser
          v-if="srcReady && srcToken"
          :key="srcToken + '-' + srcRev"
          :token="srcToken"
          :initial-dir="srcCwd"
          select="files"
          @update:cwd="srcCwd = $event"
          @drop-files="(paths, dir) => onRemoteDrop(srcToken, dir, paths)"
        />
        <div v-else class="placeholder">正在连接服务器…</div>
      </div>

      <div v-if="mode === 'transfer'" class="remote-slot">
        <p class="slot-head">
          目标 · {{ dstId ? labelOf(dstId) : "未选择" }} · {{ dstCwd || "选择后浏览" }}
        </p>
        <RemoteBrowser
          v-if="dstReady && dstToken"
          :key="dstToken + '-' + dstRev"
          :token="dstToken"
          :initial-dir="dstCwd"
          select="none"
          @update:cwd="dstCwd = $event"
          @drop-files="(paths, dir) => onRemoteDrop(dstToken, dir, paths)"
        />
        <div v-else class="placeholder">请选择目标服务器</div>
      </div>
    </div>

    <p v-if="error" class="banner err">{{ error }}</p>
    <p v-else-if="notice" class="banner ok">{{ notice }}</p>

    <div v-if="tasks.length" class="progress">
      <div
        v-for="t in tasks"
        :key="t.sessionId + t.name + t.transferred + t.done"
        class="task"
      >
        <span class="tnm" :title="t.name">{{ t.name }}</span>
        <div class="track">
          <div
            class="fill"
            :class="{ fail: !!t.error }"
            :style="{
              width: t.total ? Math.min(100, (t.transferred / t.total) * 100) + '%' : '100%',
            }"
          />
        </div>
        <span class="tstate">
          {{ t.error ? "失败" : t.done ? "完成" : `${human(t.transferred)} / ${human(t.total)}` }}
        </span>
      </div>
    </div>
  </section>
</template>

<style scoped>
.sftp-view {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px 16px 14px;
}
.sf-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: none;
  flex-wrap: wrap;
}
.modes {
  display: flex;
  gap: 4px;
  flex: none;
}
.modes button {
  height: 30px;
  padding: 0 14px;
  border: 1px solid var(--win-stroke);
  border-radius: 8px;
  background: var(--win-fill);
  color: var(--win-text-2);
  font-size: 12px;
  cursor: default;
}
.modes button:hover:not(.on) {
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.modes button.on {
  background: var(--win-accent);
  border-color: transparent;
  color: var(--win-accent-text);
  font-weight: 600;
}
.servers {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.srv-label {
  flex: none;
  color: var(--win-text-3);
  font-size: 12px;
}
.srv-arrow {
  flex: none;
  color: var(--win-text-3);
}
.servers select {
  height: 30px;
  max-width: 260px;
  padding: 0 10px;
  border-radius: 8px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12px;
  outline: none;
}
.servers select:focus {
  border-color: var(--win-accent);
  box-shadow: 0 0 0 3px var(--win-accent-soft);
}
.servers select option {
  background: var(--win-flyout);
  color: var(--win-text);
}
.hint {
  margin-left: auto;
  color: var(--win-text-3);
  font-size: 11.5px;
  white-space: nowrap;
}
.sf-panes {
  flex: 1;
  min-height: 0;
  display: grid;
  gap: 10px;
}
.sf-panes.mode-upload {
  grid-template-columns: minmax(240px, 0.8fr) minmax(0, 1.5fr);
}
.sf-panes.mode-download {
  grid-template-columns: minmax(0, 1.5fr) minmax(240px, 0.8fr);
}
.sf-panes.mode-download .local-pane {
  order: 2;
}
.sf-panes.mode-download .remote-slot {
  order: 1;
}
.sf-panes.mode-transfer {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
}
.remote-slot {
  min-height: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.slot-head {
  margin: 0;
  color: var(--win-text-3);
  font-size: 11px;
  font-family: var(--win-mono);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.remote-slot > :deep(.browser) {
  flex: 1;
}
.local-pane {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border: 1px solid var(--win-stroke);
  border-radius: var(--win-radius-lg);
  background: var(--win-card);
  overflow: hidden;
  transition: border-color 0.12s ease, box-shadow 0.12s ease;
}
.local-pane.can-drop {
  border-style: dashed;
  border-color: var(--win-stroke-strong);
}
.local-pane.over {
  border-style: solid;
  border-color: var(--win-accent);
  box-shadow: inset 0 0 0 1px var(--win-accent-soft);
}
.lp-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--win-stroke);
  flex: none;
}
.lp-head strong {
  flex: none;
  font-size: 12px;
  font-weight: 600;
}
.lp-dir {
  flex: 1;
  min-width: 0;
  color: var(--win-text-3);
  font-size: 11px;
  font-family: var(--win-mono);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.mini {
  flex: none;
  height: 24px;
  padding: 0 10px;
  border-radius: var(--win-radius);
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 11px;
  cursor: default;
}
.mini:hover {
  background: var(--win-fill-hover);
}
.lp-list {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 4px;
}
.lfile {
  display: flex;
  align-items: center;
  gap: 7px;
  height: 28px;
  padding: 0 8px;
  border-radius: 6px;
  font-size: 12px;
  color: var(--win-text-2);
  cursor: grab;
}
.lfile:hover {
  background: var(--win-fill-hover);
}
.lfile:active {
  cursor: grabbing;
}
.lfile .ico {
  flex: none;
  width: 14px;
  height: 14px;
  fill: none;
  stroke: var(--win-text-2);
  stroke-width: 1.2;
  stroke-linejoin: round;
}
.lfile .nm {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.rm {
  flex: none;
  width: 18px;
  height: 18px;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--win-text-3);
  cursor: default;
  line-height: 1;
}
.rm:hover {
  background: var(--win-fill-press);
  color: var(--win-err);
}
.tip {
  margin: 0;
  padding: 10px;
  color: var(--win-text-3);
  font-size: 12px;
  line-height: 1.6;
}
.placeholder {
  flex: 1;
  display: grid;
  place-items: center;
  border: 1px dashed var(--win-stroke);
  border-radius: var(--win-radius-lg);
  color: var(--win-text-3);
  font-size: 12px;
}
.banner {
  margin: 0;
  font-size: 12px;
  flex: none;
}
.banner.err {
  color: var(--win-err);
}
.banner.ok {
  color: var(--win-ok);
}
.progress {
  flex: none;
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 84px;
  overflow: auto;
}
.task {
  display: grid;
  grid-template-columns: minmax(0, 200px) 1fr 140px;
  gap: 8px;
  align-items: center;
  font-size: 11px;
  color: var(--win-text-3);
}
.tnm {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.track {
  height: 4px;
  border-radius: 999px;
  background: var(--win-fill);
  overflow: hidden;
}
.fill {
  height: 100%;
  border-radius: 999px;
  background: var(--win-accent);
  transition: width 0.15s linear;
}
.fill.fail {
  background: var(--win-err);
}
.tstate {
  text-align: right;
  font-variant-numeric: tabular-nums;
}
</style>
