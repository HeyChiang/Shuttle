<script setup lang="ts">
// 独立的远端目录浏览器：基于 SFTP token 工作，不依赖终端会话。
import { computed, onMounted, ref, watch } from "vue";
import * as SSH from "../../bindings/shuttle/sshservice";
import type { RemoteFile } from "../../bindings/shuttle/internal/sshcore/models";
import { beginSftpDrag, endSftpDrag, sftpDrag } from "../sftpDrag";

const props = defineProps<{
  token: string;
  /** files：多选文件；dir：单击进入目录；none：仅浏览 */
  select?: "none" | "files" | "dir";
  /** 首次加载后进入的目录，缺省为登录家目录 */
  initialDir?: string;
}>();

const emit = defineEmits<{
  (e: "update:cwd", v: string): void;
  (e: "update:picked", v: string[]): void;
  (e: "drop-files", paths: string[], dir: string): void;
}>();

const cwd = ref("");
const pathInput = ref("");
const entries = ref<RemoteFile[]>([]);
const picked = ref<string[]>([]);
const busy = ref(false);
const error = ref("");

const ready = computed(() => !!props.token);
const selectMode = computed(() => props.select ?? "none");

function human(size: number): string {
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

function stamp(ms: number): string {
  if (!ms) return "-";
  const d = new Date(ms);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(
    d.getHours(),
  )}:${p(d.getMinutes())}`;
}

function parentOf(p: string): string {
  if (!p || p === "/") return "/";
  const trimmed = p.replace(/\/+$/, "");
  const idx = trimmed.lastIndexOf("/");
  if (idx <= 0) return "/";
  return trimmed.slice(0, idx);
}

async function open(dir: string) {
  if (!props.token || !dir) return;
  busy.value = true;
  error.value = "";
  try {
    const list = (await SSH.SftpList(props.token, dir)) ?? [];
    list.sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
    entries.value = list;
    cwd.value = dir;
    pathInput.value = dir;
    picked.value = [];
    emit("update:cwd", dir);
    emit("update:picked", []);
  } catch (e) {
    error.value = `读取目录失败：${String(e)}`;
  } finally {
    busy.value = false;
  }
}

async function goHome() {
  if (!props.token) return;
  try {
    const home = await SSH.SftpHome(props.token);
    await open(home || "/");
  } catch (e) {
    error.value = `获取家目录失败：${String(e)}`;
  }
}

function goParent() {
  void open(parentOf(cwd.value || "/"));
}

function refresh() {
  void open(cwd.value || "/");
}

function toggle(entry: RemoteFile) {
  if (entry.isDir) return;
  const i = picked.value.indexOf(entry.path);
  if (i >= 0) picked.value.splice(i, 1);
  else picked.value.push(entry.path);
  emit("update:picked", [...picked.value]);
}

function onRowClick(entry: RemoteFile) {
  if (entry.isDir) {
    if (selectMode.value !== "files") void open(entry.path);
    return;
  }
  if (selectMode.value === "files") toggle(entry);
}

function onRowDblClick(entry: RemoteFile) {
  if (entry.isDir) void open(entry.path);
}

function isPicked(entry: RemoteFile) {
  return picked.value.includes(entry.path);
}

// ------------------------------------------------------------- 拖拽传输
const dropOver = ref(false);
let dragDepth = 0;

/** 当前拖拽是否可投放到本面板（同主机的远端文件不能自己传自己） */
const canAccept = computed(() => {
  const src = sftpDrag.value;
  if (!src) return false;
  return !(src.kind === "remote" && src.token === props.token);
});

function onDragStart(entry: RemoteFile, ev: DragEvent) {
  if (entry.isDir) {
    ev.preventDefault();
    return;
  }
  // 拖动已勾选的文件时，一次带走全部勾选项
  const paths =
    isPicked(entry) && picked.value.length ? [...picked.value] : [entry.path];
  beginSftpDrag({ kind: "remote", token: props.token, paths });
  if (ev.dataTransfer) {
    ev.dataTransfer.effectAllowed = "copy";
    ev.dataTransfer.setData("text/plain", paths.join("\n"));
  }
}

function onDragEnter() {
  if (!canAccept.value) return;
  dragDepth += 1;
  dropOver.value = true;
}

function onDragOver(ev: DragEvent) {
  if (!canAccept.value) return;
  if (ev.dataTransfer) ev.dataTransfer.dropEffect = "copy";
}

function onDragLeave() {
  if (!canAccept.value) return;
  dragDepth -= 1;
  if (dragDepth <= 0) {
    dragDepth = 0;
    dropOver.value = false;
  }
}

function onDrop() {
  dragDepth = 0;
  dropOver.value = false;
  const src = sftpDrag.value;
  if (!src || !canAccept.value) {
    endSftpDrag();
    return;
  }
  const paths = [...src.paths];
  // 先通知父组件，让它读取当前拖拽来源后再清理
  emit("drop-files", paths, cwd.value);
  endSftpDrag();
}

onMounted(() => {
  if (!props.initialDir) {
    void goHome();
    return;
  }
  void open(props.initialDir).then(() => {
    if (!error.value) return;
    // 目标目录不存在（例如切换了主机）时回退到登录家目录
    error.value = "";
    void goHome();
  });
});

watch(
  () => props.token,
  () => {
    entries.value = [];
    picked.value = [];
    cwd.value = "";
    pathInput.value = "";
    error.value = "";
    if (props.token) void goHome();
  },
);
</script>

<template>
  <div
    class="browser"
    :class="{ 'drop-over': dropOver }"
    @dragenter.prevent="onDragEnter"
    @dragover.prevent="onDragOver"
    @dragleave="onDragLeave"
    @drop.prevent="onDrop"
  >
    <div class="bar">
      <button class="icon" :disabled="!ready || busy" title="上级目录" @click="goParent">
        <svg viewBox="0 0 16 16"><path d="M13 8H3.5M7.5 4L3.5 8l4 4" /></svg>
      </button>
      <button class="icon" :disabled="!ready || busy" title="家目录" @click="goHome">
        <svg viewBox="0 0 16 16">
          <path d="M2.5 7.2L8 3l5.5 4.2v5.6a.7.7 0 0 1-.7.7H3.2a.7.7 0 0 1-.7-.7z" />
          <path d="M6.4 13.5V9.8h3.2v3.7" />
        </svg>
      </button>
      <button class="icon" :disabled="!ready || busy" title="刷新" @click="refresh">
        <svg viewBox="0 0 16 16">
          <path d="M13.2 8a5.2 5.2 0 1 1-1.6-3.8" />
          <path d="M13.4 2.6v3h-3" />
        </svg>
      </button>
      <input
        v-model="pathInput"
        class="path"
        :disabled="!ready || busy"
        placeholder="/home/user"
        @keyup.enter="open(pathInput)"
      />
    </div>

    <p v-if="!ready" class="hint">请先选择服务器</p>
    <p v-else-if="error" class="err">{{ error }}</p>

    <div v-else class="list">
      <div class="row head">
        <span v-if="selectMode === 'files'" class="c-check" />
        <span class="c-name">名称</span>
        <span class="c-size">大小</span>
        <span class="c-time">修改时间</span>
      </div>

      <div
        v-for="entry in entries"
        :key="entry.path"
        class="row"
        :class="{ picked: isPicked(entry), draggable: !entry.isDir }"
        :draggable="!entry.isDir"
        @click="onRowClick(entry)"
        @dblclick="onRowDblClick(entry)"
        @dragstart="onDragStart(entry, $event)"
        @dragend="endSftpDrag"
      >
        <span v-if="selectMode === 'files'" class="c-check">
          <input
            v-if="!entry.isDir"
            type="checkbox"
            :checked="isPicked(entry)"
            @click.stop="toggle(entry)"
          />
        </span>
        <span class="c-name" :title="entry.isSymlink ? '→ ' + entry.linkTarget : entry.path">
          <svg v-if="entry.isDir" class="ico dir" viewBox="0 0 16 16">
            <path
              d="M2.2 4.6a.8.8 0 0 1 .8-.8h2.9l1.4 1.6h5.7a.8.8 0 0 1 .8.8v6.4a.8.8 0 0 1-.8.8H3a.8.8 0 0 1-.8-.8z"
            />
          </svg>
          <svg v-else class="ico file" viewBox="0 0 16 16">
            <path d="M4.2 2.6h4.4l3.2 3.2v7.6H4.2z" />
            <path d="M8.6 2.6v3.2h3.2" />
          </svg>
          <span class="nm">{{ entry.name }}</span>
        </span>
        <span class="c-size">{{ entry.isDir ? "-" : human(entry.size) }}</span>
        <span class="c-time">{{ stamp(entry.modTime) }}</span>
      </div>

      <p v-if="!entries.length && !busy" class="empty">目录为空</p>
      <p v-if="busy" class="empty">正在读取…</p>
    </div>
  </div>
</template>

<style scoped>
.browser {
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100%;
  border: 1px solid var(--win-stroke);
  border-radius: var(--win-radius-lg);
  overflow: hidden;
  background: var(--win-card);
  transition: border-color 0.12s ease, box-shadow 0.12s ease;
}
.browser.drop-over {
  border-color: var(--win-accent);
  box-shadow: inset 0 0 0 1px var(--win-accent-soft);
}
.bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 7px 8px;
  border-bottom: 1px solid var(--win-stroke);
  flex: none;
}
.icon {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border: 1px solid var(--win-stroke);
  border-radius: 8px;
  background: var(--win-fill);
  color: var(--win-text-2);
  cursor: default;
}
.icon svg {
  width: 14px;
  height: 14px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.3;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.icon:hover:not(:disabled) {
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.icon:disabled {
  color: var(--win-text-disabled);
  cursor: not-allowed;
}
.path {
  flex: 1;
  min-width: 0;
  height: 30px;
  padding: 0 10px;
  border-radius: 8px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12px;
  font-family: var(--win-mono);
  outline: none;
}
.path:focus {
  border-color: var(--win-accent);
  box-shadow: 0 0 0 3px var(--win-accent-soft);
}
.hint,
.empty {
  margin: 0;
  padding: 10px 12px;
  color: var(--win-text-3);
  font-size: 12px;
}
.err {
  margin: 0;
  padding: 10px 12px;
  color: var(--win-err);
  font-size: 12px;
}
.list {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
.row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 90px 130px;
  align-items: center;
  gap: 8px;
  padding: 0 10px;
  height: 28px;
  font-size: 12px;
  color: var(--win-text-2);
  border-bottom: 1px solid var(--win-stroke);
  cursor: default;
}
.row:has(.c-check) {
  grid-template-columns: 26px minmax(0, 1fr) 90px 130px;
}
.row:not(.head):hover {
  background: var(--win-fill-hover);
}
.row.picked {
  background: var(--win-accent-soft);
}
.row.head {
  position: sticky;
  top: 0;
  z-index: 1;
  height: 26px;
  background: var(--win-card);
  color: var(--win-text-3);
  font-size: 11px;
  border-bottom: 1px solid var(--win-stroke-strong);
}
.c-check {
  display: flex;
  align-items: center;
}
.c-check input {
  accent-color: var(--win-accent);
  cursor: default;
}
.c-name {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
}
.ico {
  flex: none;
  width: 14px;
  height: 14px;
  fill: none;
  stroke-width: 1.2;
  stroke-linejoin: round;
}
.ico.dir {
  stroke: #7eb0ff;
  fill: rgba(126, 176, 255, 0.18);
}
.ico.file {
  stroke: var(--win-text-2);
}
.nm {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.c-size,
.c-time {
  color: var(--win-text-3);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
