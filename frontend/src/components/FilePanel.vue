<script setup lang="ts">
// SFTP 文件面板：浏览远端目录、上传、下载、新建、删除、重命名。
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import * as SSH from "../../bindings/shuttle/sshservice";
import type { RemoteFile } from "../../bindings/shuttle/internal/sshcore/models";
import { isConnected, sessionById, transfers } from "../store";

const props = defineProps<{ sessionId: string }>();

const { t } = useI18n();

const cwd = ref("");
const pathInput = ref("");
const entries = ref<RemoteFile[]>([]);
const picked = ref<string[]>([]);
const busy = ref(false);
const error = ref("");
const notice = ref("");

const session = computed(() => sessionById(props.sessionId));
const ready = computed(() => !!session.value && isConnected(session.value));
const busyText = computed(() => (busy.value ? t("files.busy") : ""));

const sessionTransfers = computed(() =>
  transfers.value.filter((t) => t.sessionId === props.sessionId).slice(0, 6),
);

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

function joinRemote(dir: string, name: string): string {
  if (dir === "/") return `/${name}`;
  return `${dir.replace(/\/+$/, "")}/${name}`;
}

function resetMessages() {
  error.value = "";
  notice.value = "";
}

async function run<T>(label: string, fn: () => Promise<T>): Promise<T | undefined> {
  resetMessages();
  busy.value = true;
  try {
    return await fn();
  } catch (e) {
    error.value = t("files.failed", { label, error: String(e) });
    return undefined;
  } finally {
    busy.value = false;
  }
}

async function open(dir: string) {
  if (!props.sessionId) return;
  await run(t("files.labelReadDir"), async () => {
    const list = (await SSH.ListRemote(props.sessionId, dir)) ?? [];
    list.sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
    entries.value = list;
    cwd.value = dir;
    pathInput.value = dir;
    picked.value = [];
  });
}

async function goHome() {
  const home = await run(t("files.labelHome"), () => SSH.RemoteHome(props.sessionId));
  if (home) await open(home);
}

function goParent() {
  void open(parentOf(cwd.value));
}

function enter(entry: RemoteFile) {
  if (entry.isDir) void open(entry.path);
}

function toggle(entry: RemoteFile) {
  const i = picked.value.indexOf(entry.path);
  if (i >= 0) picked.value.splice(i, 1);
  else picked.value.push(entry.path);
}

function isPicked(entry: RemoteFile) {
  return picked.value.includes(entry.path);
}

function pickedEntries(): RemoteFile[] {
  return entries.value.filter((e) => picked.value.includes(e.path));
}

async function upload() {
  if (!cwd.value) return;
  const paths = await run(t("files.labelPickFiles"), () =>
    SSH.PickFiles(t("files.pickUploadTitle")),
  );
  if (!paths || !paths.length) return;
  const ok = await run(t("files.labelUpload"), () =>
    SSH.Upload(props.sessionId, cwd.value, paths),
  );
  if (ok !== undefined) {
    notice.value = t("files.uploadedMsg", { n: paths.length, dir: cwd.value });
    await refresh();
  }
}

async function download() {
  const targets = pickedEntries();
  if (!targets.length) {
    error.value = t("files.needDownloadSelect");
    return;
  }
  const dir = await run(t("files.labelPickDir"), () =>
    SSH.PickDirectory(t("files.pickDirTitle")),
  );
  if (!dir) return;
  const ok = await run(t("files.labelDownload"), () =>
    SSH.Download(
      props.sessionId,
      targets.map((item) => item.path),
      dir,
    ),
  );
  if (ok !== undefined) notice.value = t("files.downloadedMsg", { n: targets.length, dir });
}

async function mkdir() {
  const name = window.prompt(t("files.newFolderPrompt"));
  if (!name) return;
  const ok = await run(t("files.labelMkdir"), () =>
    SSH.Mkdir(props.sessionId, joinRemote(cwd.value, name)),
  );
  if (ok !== undefined) {
    notice.value = t("files.created", { name });
    await refresh();
  }
}

async function remove() {
  const targets = pickedEntries();
  if (!targets.length) {
    error.value = t("files.needDeleteSelect");
    return;
  }
  if (!window.confirm(t("files.confirmDelete", { n: targets.length }))) return;
  const ok = await run(t("files.labelRemove"), async () => {
    for (const item of targets) {
      await SSH.Remove(props.sessionId, item.path, item.isDir);
    }
  });
  if (ok !== undefined) {
    notice.value = t("files.deleted");
    await refresh();
  }
}

async function rename() {
  const targets = pickedEntries();
  if (targets.length !== 1) {
    error.value = t("files.needOneRename");
    return;
  }
  const target = targets[0];
  const name = window.prompt(t("files.renamePrompt"), target.name);
  if (!name || name === target.name) return;
  const ok = await run(t("files.labelRename"), () =>
    SSH.Rename(props.sessionId, target.path, joinRemote(cwd.value, name)),
  );
  if (ok !== undefined) {
    notice.value = t("files.renamed");
    await refresh();
  }
}

function refresh() {
  return open(cwd.value || ".");
}

onMounted(() => {
  if (ready.value) void goHome();
});

watch(
  () => props.sessionId,
  (id, old) => {
    if (id === old) return;
    entries.value = [];
    picked.value = [];
    cwd.value = "";
    pathInput.value = "";
    resetMessages();
    if (id && ready.value) void goHome();
  },
);

watch(ready, (v) => {
  if (v && !cwd.value) void goHome();
});
</script>

<template>
  <div class="files">
    <div class="bar">
      <button
        class="btn icon"
        :disabled="!ready || busy"
        :title="t('browser.parent')"
        @click="goParent"
      >
        <svg viewBox="0 0 16 16"><path d="M13 8H3.5M7.5 4L3.5 8l4 4" /></svg>
      </button>
      <button
        class="btn icon"
        :disabled="!ready || busy"
        :title="t('browser.home')"
        @click="goHome"
      >
        <svg viewBox="0 0 16 16">
          <path d="M2.5 7.2L8 3l5.5 4.2v5.6a.7.7 0 0 1-.7.7H3.2a.7.7 0 0 1-.7-.7z" />
          <path d="M6.4 13.5V9.8h3.2v3.7" />
        </svg>
      </button>
      <button
        class="btn icon"
        :disabled="!ready || busy"
        :title="t('browser.refresh')"
        @click="refresh"
      >
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
      <button class="btn primary" :disabled="!ready || busy" @click="upload">
        {{ t("common.upload") }}
      </button>
      <button class="btn" :disabled="!ready || busy" @click="download">
        {{ t("common.download") }}
      </button>
      <button class="btn" :disabled="!ready || busy" @click="mkdir">
        {{ t("common.newFolder") }}
      </button>
      <button class="btn" :disabled="!ready || busy || picked.length !== 1" @click="rename">
        {{ t("common.rename") }}
      </button>
      <button class="btn danger" :disabled="!ready || busy || !picked.length" @click="remove">
        {{ t("common.delete") }}
      </button>
    </div>

    <p v-if="!ready" class="hint">{{ t("browser.notConnected") }}</p>
    <p v-else-if="error" class="error">{{ error }}</p>
    <p v-else-if="notice" class="notice">{{ notice }}</p>

    <div class="list">
      <div class="row head">
        <span class="c-check" />
        <span class="c-name">{{ t("common.name") }}</span>
        <span class="c-size">{{ t("common.size") }}</span>
        <span class="c-mode">{{ t("common.permissions") }}</span>
        <span class="c-time">{{ t("common.modified") }}</span>
      </div>
      <div
        v-for="entry in entries"
        :key="entry.path"
        class="row"
        :class="{ picked: isPicked(entry) }"
        @click="toggle(entry)"
        @dblclick="enter(entry)"
      >
        <span class="c-check">
          <input type="checkbox" :checked="isPicked(entry)" @click.stop="toggle(entry)" />
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
          <span v-if="entry.isSymlink" class="link">→ {{ entry.linkTarget }}</span>
        </span>
        <span class="c-size">{{ entry.isDir ? "-" : human(entry.size) }}</span>
        <span class="c-mode">{{ entry.mode }}</span>
        <span class="c-time">{{ stamp(entry.modTime) }}</span>
      </div>
      <p v-if="ready && !entries.length && !busy" class="empty">{{ t("common.emptyDir") }}</p>
      <p v-if="busy" class="empty">{{ busyText }}</p>
    </div>

    <div v-if="sessionTransfers.length" class="progress">
      <div
        v-for="task in sessionTransfers"
        :key="task.sessionId + task.name + task.transferred"
        class="task"
      >
        <span class="tname">
          <svg v-if="task.direction === 'upload'" class="tico" viewBox="0 0 16 16">
            <path d="M8 13.2V3.6M4.2 7.4L8 3.6l3.8 3.8" />
          </svg>
          <svg v-else class="tico" viewBox="0 0 16 16">
            <path d="M8 2.8v9.6M4.2 8.6L8 12.4l3.8-3.8" />
          </svg>
          <span class="tnm">{{ task.name }}</span>
        </span>
        <div class="track">
          <div
            class="fill"
            :class="{ fail: !!task.error }"
            :style="{
              width: task.total
                ? Math.min(100, (task.transferred / task.total) * 100) + '%'
                : '100%',
            }"
          />
        </div>
        <span class="tstate">
          {{
            task.error
              ? t("common.failed")
              : task.done
                ? t("common.done")
                : `${human(task.transferred)} / ${human(task.total)}`
          }}
        </span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.files {
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100%;
}
.bar {
  display: flex;
  gap: 6px;
  align-items: center;
  padding: 8px 10px;
  border-bottom: 1px solid var(--win-stroke);
  flex: none;
}
.path {
  flex: 1;
  min-width: 0;
  height: 30px;
  padding: 0 11px;
  border-radius: 8px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12px;
  outline: none;
  font-family: var(--win-mono);
  transition: border-color 0.12s ease, box-shadow 0.12s ease;
}
.path::placeholder {
  color: var(--win-text-3);
}
.path:hover:not(:disabled) {
  background: var(--win-fill-hover);
}
.path:focus {
  border-color: var(--win-accent);
  box-shadow: 0 0 0 3px var(--win-accent-soft);
}
.btn {
  height: 30px;
  padding: 0 11px;
  border-radius: 8px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12px;
  cursor: default;
  white-space: nowrap;
}
.btn.icon {
  width: 32px;
  padding: 0;
  display: grid;
  place-items: center;
}
.btn.icon svg {
  width: 14px;
  height: 14px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.3;
  stroke-linecap: round;
  stroke-linejoin: round;
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
.btn.danger {
  color: var(--win-err);
}
.hint,
.error,
.notice,
.empty {
  margin: 0;
  padding: 8px 12px;
  font-size: 12px;
}
.hint,
.empty {
  color: var(--win-text-3);
}
.error {
  color: var(--win-err);
}
.notice {
  color: var(--win-ok);
}
.list {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
.row {
  display: grid;
  grid-template-columns: 34px minmax(0, 1fr) 90px 110px 150px;
  align-items: center;
  gap: 8px;
  padding: 0 12px;
  height: 28px;
  font-size: 12px;
  color: var(--win-text-2);
  cursor: default;
  border-bottom: 1px solid var(--win-stroke);
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
  background: var(--win-card);
  color: var(--win-text-3);
  font-size: 11px;
  border-bottom: 1px solid var(--win-stroke-strong);
  z-index: 1;
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
  cursor: default;
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
.link {
  color: var(--win-text-3);
  font-size: 11px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.c-size,
.c-mode,
.c-time {
  color: var(--win-text-3);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.progress {
  flex: none;
  max-height: 150px;
  overflow: auto;
  border-top: 1px solid var(--win-stroke);
  padding: 6px 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.task {
  display: grid;
  grid-template-columns: minmax(0, 180px) 1fr 130px;
  gap: 8px;
  align-items: center;
  font-size: 11px;
  color: var(--win-text-3);
}
.tname {
  display: flex;
  align-items: center;
  gap: 5px;
  min-width: 0;
}
.tico {
  flex: none;
  width: 12px;
  height: 12px;
  fill: none;
  stroke: var(--win-accent);
  stroke-width: 1.4;
  stroke-linecap: round;
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