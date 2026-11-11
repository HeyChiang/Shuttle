<template>
  <div class="batch-page">
    <header class="batch-head">
      <div class="lh-text">
        <h2>{{ t("batch.title") }}</h2>
        <p class="batch-desc">{{ t("batch.subtitle") }}</p>
      </div>
    </header>

    <div class="batch-grid">
      <aside class="host-panel">
        <div class="panel-head">
          <span class="panel-title">{{ t("batch.hostsTitle") }}</span>
          <span class="panel-count">{{ t("batch.selected", { n: selected.size }) }}</span>
        </div>
        <input
          v-model="query"
          class="host-search"
          :placeholder="t('batch.searchPlaceholder')"
        />
        <div class="host-tools">
          <button class="mini-btn" :disabled="!visibleHosts.length" @click="selectAll">
            {{ t("batch.selectAll") }}
          </button>
          <button class="mini-btn" :disabled="!selected.size" @click="clearAll">
            {{ t("batch.clearAll") }}
          </button>
        </div>

        <div v-if="hosts.length" class="host-list">
          <label
            v-for="h in visibleHosts"
            :key="h.id"
            class="host-row"
            :class="{ on: selected.has(h.id) }"
          >
            <input
              type="checkbox"
              :checked="selected.has(h.id)"
              @change="toggleHost(h)"
            />
            <span class="host-text">
              <strong>{{ h.name || h.host }}</strong>
              <small>{{ h.user }}@{{ h.host }}:{{ h.port }}</small>
            </span>
            <span v-if="h.group" class="host-group">{{ h.group }}</span>
          </label>
          <p v-if="!visibleHosts.length" class="host-empty">{{ t("batch.noMatch") }}</p>
        </div>
        <div v-else class="host-empty">
          <p>{{ t("batch.emptyHosts") }}</p>
          <p class="host-empty-hint">{{ t("batch.emptyHostsHint") }}</p>
        </div>
      </aside>

      <main class="run-panel">
        <section class="script-card">
          <div class="panel-head">
            <span class="panel-title">{{ t("batch.scriptTitle") }}</span>
            <select v-model="snippetId" class="script-select">
              <option value="">{{ t("batch.snippetPlaceholder") }}</option>
              <option v-for="s in snippets" :key="s.id" :value="s.id">
                {{ s.name || firstLine(s.code) }}
              </option>
            </select>
          </div>
          <div class="attach-row">
            <input
              v-model="targetDir"
              class="dir-input"
              :class="{ warn: files.length && !targetDir.trim() }"
              :placeholder="t('batch.targetDirPlaceholder')"
            />
            <button class="mini-btn" @click="pickFiles">{{ t("batch.addFiles") }}</button>
          </div>
          <div v-if="files.length" class="file-tags">
            <span
              v-for="(file, index) in files"
              :key="`${file}-${index}`"
              class="file-tag"
              :title="file"
            >
              <span class="file-tag-name">{{ fileName(file) }}</span>
              <button
                class="file-tag-x"
                :title="t('batch.removeFile')"
                @click="removeFile(index)"
              >
                ×
              </button>
            </span>
          </div>
          <p
            v-if="files.length"
            class="script-files"
            :class="{ warn: !targetDir.trim() }"
            :title="files.join('\n')"
          >
            {{
              t("batch.attachments", {
                n: files.length,
                dir: targetDir.trim() || t("batch.noDir"),
              })
            }}
          </p>
          <textarea
            v-model="script"
            class="script-input"
            spellcheck="false"
            :placeholder="t('batch.scriptPlaceholder')"
            @keydown.ctrl.enter.prevent="run"
            @keydown.meta.enter.prevent="run"
          />
          <div class="script-foot">
            <span class="script-hint">{{ t("batch.scriptHint") }}</span>
            <button class="btn primary" :disabled="!canRun" @click="run">
              {{ busy ? t("batch.running") : t("batch.run") }}
            </button>
          </div>
          <p v-if="error" class="run-error">{{ error }}</p>
        </section>

        <section class="result-card">
          <div class="panel-head">
            <span class="panel-title">{{ t("batch.resultsTitle") }}</span>
            <span class="panel-count">
              {{ t("batch.progress", { done: finished, total: resultRows.length }) }}
            </span>
          </div>

          <p v-if="!resultRows.length" class="result-empty">
            {{ t("batch.noResults") }}
          </p>
          <div v-else class="result-list">
            <article v-for="row in resultRows" :key="row.hostId" class="result-item">
              <header class="result-head" @click="toggleRow(row.hostId)">
                <span class="result-name">{{ row.name }}</span>
                <span class="status-chip" :class="[row.status, { warn: row.status === 'done' && row.exitCode > 0 }]">
                  {{ statusLabel(row) }}
                </span>
                <span class="caret" :class="{ open: expanded.has(row.hostId) }">›</span>
              </header>
              <pre v-if="expanded.has(row.hostId)" class="result-output">{{
                (row.output || t("batch.noOutput")) +
                (row.error ? "\n" + row.error : "")
              }}</pre>
            </article>
          </div>
        </section>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Events } from "@wailsio/runtime";
import { useI18n } from "vue-i18n";
import * as SSH from "../../bindings/shuttle/sshservice";
import { SshBatchExec } from "../../bindings/events";
import type { BatchExecEvent, HostConfig } from "../../bindings/shuttle/internal/sshcore/models";
import { firstLine, snippets } from "../snippets";
import { hosts } from "../store";

interface ResultRow {
  hostId: string;
  name: string;
  status: "queued" | "uploading" | "running" | "done" | "error";
  output: string;
  exitCode: number;
  error: string;
}

const { t } = useI18n();
const query = ref("");
const selected = ref(new Set<string>());
const script = ref("");
const snippetId = ref("");
/** 选中代码块自带的附件与目标目录，执行前会上传到每台主机 */
const files = ref<string[]>([]);
const targetDir = ref("");
const error = ref("");
const runID = ref("");
const rows = ref<Record<string, ResultRow>>({});
const expanded = ref(new Set<string>());

const visibleHosts = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) return hosts.value;
  return hosts.value.filter((h) =>
    [h.name, h.host, h.user, h.group].some((v) => (v ?? "").toLowerCase().includes(q)),
  );
});

const resultRows = computed<ResultRow[]>(() => {
  const order = hosts.value.map((h) => h.id);
  return Object.values(rows.value).sort(
    (a, b) => order.indexOf(a.hostId) - order.indexOf(b.hostId),
  );
});

const finished = computed(
  () => resultRows.value.filter((r) => r.status === "done" || r.status === "error").length,
);

const busy = computed(() =>
  resultRows.value.some(
    (r) => r.status === "queued" || r.status === "uploading" || r.status === "running",
  ),
);

const canRun = computed(
  () => selected.value.size > 0 && script.value.trim().length > 0 && !busy.value,
);

watch(snippetId, (id) => {
  const snippet = snippets.value.find((s) => s.id === id);
  if (!snippet) {
    script.value = "";
    files.value = [];
    targetDir.value = "";
    return;
  }
  script.value = snippet.code;
  files.value = snippet.files ?? [];
  targetDir.value = snippet.targetDir ?? "";
});

/** 手动为批量脚本添加附件，选中代码块后仍可继续增删 */
async function pickFiles() {
  try {
    const paths = await SSH.PickFiles(t("batch.pickFilesTitle"));
    if (!paths?.length) return;
    files.value = [...new Set([...files.value, ...paths])];
  } catch (e) {
    error.value = String(e);
  }
}

function removeFile(index: number) {
  files.value.splice(index, 1);
}

function fileName(file: string): string {
  return file.split(/[\\/]/).pop() || file;
}

function toggleHost(host: HostConfig) {
  const next = new Set(selected.value);
  if (next.has(host.id)) next.delete(host.id);
  else next.add(host.id);
  selected.value = next;
}

function selectAll() {
  selected.value = new Set([...selected.value, ...visibleHosts.value.map((h) => h.id)]);
}

function clearAll() {
  selected.value = new Set();
}

function toggleRow(hostId: string) {
  const next = new Set(expanded.value);
  if (next.has(hostId)) next.delete(hostId);
  else next.add(hostId);
  expanded.value = next;
}

function statusLabel(row: ResultRow): string {
  if (row.status === "queued") return t("batch.queued");
  if (row.status === "uploading") return t("batch.uploading");
  if (row.status === "running") return t("batch.inProgress");
  if (row.status === "error") return t("batch.failed");
  return row.exitCode > 0 ? t("batch.exitCode", { code: row.exitCode }) : t("batch.success");
}

async function run() {
  if (!canRun.value) return;
  if (files.value.length && !targetDir.value.trim()) {
    error.value = t("batch.needTargetDir");
    return;
  }
  const targets = hosts.value.filter((h) => selected.value.has(h.id));
  const id = `batch-${Date.now()}-${Math.random().toString(36).slice(2)}`;
  runID.value = id;
  error.value = "";
  expanded.value = new Set();
  rows.value = Object.fromEntries(
    targets.map((h) => [
      h.id,
      {
        hostId: h.id,
        name: h.name || h.host,
        status: "queued" as const,
        output: "",
        exitCode: 0,
        error: "",
      },
    ]),
  );
  try {
    await SSH.BatchExec(targets, script.value, files.value, targetDir.value.trim(), id);
  } catch (e) {
    error.value = String(e);
  }
}

function onBatchEvent(event: { data: BatchExecEvent }) {
  const payload = event.data;
  if (!payload || payload.runId !== runID.value) return;
  const prev = rows.value[payload.hostId];
  if (!prev) return;
  rows.value = {
    ...rows.value,
    [payload.hostId]: {
      hostId: payload.hostId,
      name: payload.name || prev.name,
      status: payload.status as ResultRow["status"],
      output: payload.output ?? "",
      exitCode: payload.exitCode ?? 0,
      error: payload.error ?? "",
    },
  };
}

let stopListening: (() => void) | undefined;
onMounted(() => {
  stopListening = Events.On(SshBatchExec, onBatchEvent);
});
onBeforeUnmount(() => stopListening?.());
</script>

<style scoped>
.batch-page {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 16px;
  min-height: 0;
}
.batch-head .lh-text h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  color: var(--win-text);
}
.batch-desc {
  margin: 4px 0 0;
  color: var(--win-text-3);
  font-size: 12px;
}
.batch-grid {
  display: grid;
  grid-template-columns: minmax(220px, 300px) minmax(0, 1fr);
  gap: 14px;
  flex: 1;
  min-height: 0;
}
.host-panel,
.script-card,
.result-card {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border: 1px solid var(--win-stroke);
  border-radius: var(--win-radius-lg);
  background: var(--win-card);
}
.host-panel {
  padding: 12px;
  gap: 10px;
}
.run-panel {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-height: 0;
}
.script-card {
  flex: none;
  padding: 12px;
  gap: 10px;
}
.result-card {
  flex: 1;
  padding: 12px;
  gap: 10px;
}
.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.panel-title {
  color: var(--win-text);
  font-size: 13px;
  font-weight: 600;
}
.panel-count {
  color: var(--win-text-3);
  font-size: 11px;
}
.host-search,
.script-select,
.script-input {
  border: 1px solid var(--win-stroke);
  border-radius: 8px;
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12px;
  outline: none;
}
.host-search {
  height: 30px;
  padding: 0 10px;
}
.script-files {
  margin: -2px 0 0;
  overflow: hidden;
  color: var(--win-text-3);
  font-size: 11.5px;
  line-height: 1.5;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.script-files.warn {
  color: var(--win-warn);
}
.attach-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.dir-input {
  flex: 1;
  min-width: 0;
  height: 26px;
  padding: 0 9px;
  border: 1px solid var(--win-stroke);
  border-radius: 8px;
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 11.5px;
  outline: none;
}
.dir-input.warn {
  border-color: var(--win-warn);
}
.dir-input:focus {
  border-color: var(--win-accent);
}
.dir-input::placeholder {
  color: var(--win-text-disabled);
}
.file-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.file-tag {
  display: flex;
  align-items: center;
  gap: 4px;
  max-width: 100%;
  padding: 2px 4px 2px 8px;
  border: 1px solid var(--win-stroke);
  border-radius: 6px;
  background: var(--win-fill);
  color: var(--win-text-2);
  font-size: 11px;
}
.file-tag-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.file-tag-x {
  width: 16px;
  height: 16px;
  padding: 0;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--win-text-3);
  font-size: 12px;
  line-height: 1;
  cursor: default;
}
.file-tag-x:hover {
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.host-search::placeholder,
.script-input::placeholder {
  color: var(--win-text-disabled);
}
.host-search:focus,
.script-select:focus,
.script-input:focus {
  border-color: var(--win-accent);
}
.host-tools {
  display: flex;
  gap: 8px;
}
.mini-btn {
  height: 24px;
  padding: 0 10px;
  border: 1px solid var(--win-stroke);
  border-radius: 6px;
  background: var(--win-fill);
  color: var(--win-text-2);
  font-size: 11px;
  cursor: default;
}
.mini-btn:hover:not(:disabled) {
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.mini-btn:disabled {
  opacity: 0.45;
}
.host-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
  overflow-y: auto;
  min-height: 0;
  padding-right: 2px;
}
.host-row {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 7px 9px;
  border: 1px solid transparent;
  border-radius: 8px;
  cursor: default;
}
.host-row:hover {
  background: var(--win-fill-hover);
}
.host-row.on {
  border-color: var(--win-accent);
  background: var(--win-accent-soft);
}
.host-row input {
  flex: none;
  accent-color: var(--win-accent);
}
.host-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.host-text strong {
  overflow: hidden;
  color: var(--win-text);
  font-size: 12.5px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.host-text small {
  overflow: hidden;
  color: var(--win-text-3);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.host-group {
  flex: none;
  margin-left: auto;
  padding: 1px 6px;
  border-radius: 5px;
  background: var(--win-fill);
  color: var(--win-text-3);
  font-size: 10px;
}
.host-empty {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin: 0;
  padding: 16px 10px;
  color: var(--win-text-3);
  font-size: 12px;
  text-align: center;
}
.host-empty-hint {
  margin: 0;
  color: var(--win-text-disabled);
  font-size: 11px;
}
.script-select {
  height: 26px;
  max-width: 180px;
  padding: 0 6px;
  font-size: 11px;
}
.script-input {
  min-height: 148px;
  padding: 10px;
  font-family: var(--win-mono);
  font-size: 12.5px;
  line-height: 1.6;
  resize: vertical;
}
.script-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.script-hint {
  color: var(--win-text-3);
  font-size: 11px;
}
.run-error {
  margin: 0;
  color: var(--win-err);
  font-size: 12px;
}
.btn {
  height: 30px;
  padding: 0 16px;
  border: 1px solid var(--win-stroke);
  border-radius: 8px;
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12px;
  cursor: default;
}
.btn.primary {
  border-color: transparent;
  background: var(--win-accent);
  color: var(--win-accent-text);
  font-weight: 600;
}
.btn.primary:hover:not(:disabled) {
  background: var(--win-accent-hover);
}
.btn:disabled {
  opacity: 0.45;
}
.result-empty {
  margin: 0;
  padding: 28px 12px;
  color: var(--win-text-3);
  font-size: 12px;
  text-align: center;
}
.result-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  overflow-y: auto;
  min-height: 0;
}
.result-item {
  border: 1px solid var(--win-stroke);
  border-radius: 8px;
  background: var(--win-fill);
  overflow: hidden;
}
.result-head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  cursor: default;
}
.result-head:hover {
  background: var(--win-fill-hover);
}
.result-name {
  flex: 1;
  overflow: hidden;
  color: var(--win-text);
  font-size: 12.5px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.status-chip {
  flex: none;
  padding: 1px 8px;
  border-radius: 999px;
  background: var(--win-fill-press);
  color: var(--win-text-2);
  font-size: 10.5px;
}
.status-chip.running,
.status-chip.queued,
.status-chip.uploading {
  color: var(--win-text-3);
}
.status-chip.done {
  background: var(--win-accent-soft);
  color: var(--win-accent);
}
.status-chip.error {
  background: color-mix(in srgb, var(--win-err) 14%, transparent);
  color: var(--win-err);
}
.status-chip.warn {
  background: color-mix(in srgb, var(--win-warn) 14%, transparent);
  color: var(--win-warn);
}
.caret {
  flex: none;
  color: var(--win-text-3);
  font-size: 14px;
  transform: rotate(90deg);
  transition: transform 0.12s ease;
}
.caret.open {
  transform: rotate(-90deg);
}
.result-output {
  margin: 0;
  padding: 10px;
  border-top: 1px solid var(--win-divider);
  background: var(--win-term);
  color: var(--win-text-2);
  font-family: var(--win-mono);
  font-size: 11.5px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 220px;
  overflow: auto;
}
</style>
