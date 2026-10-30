<script setup lang="ts">
// SFTP 工作区：左右两侧各自独立，可分别选择本地或远程服务器。
// 默认左侧本地、右侧服务器列表（搜索选择），文件通过拖拽在两侧之间传输。
import { computed, onBeforeUnmount, ref } from "vue";
import * as SSH from "../../bindings/shuttle/sshservice";
import type { HostConfig } from "../../bindings/shuttle/internal/sshcore/models";
import { transfers } from "../store";
import SftpSide from "./SftpSide.vue";

const props = defineProps<{
  hosts: HostConfig[];
}>();

type Kind = "local" | "remote";

const leftKind = ref<Kind>("local");
const leftHost = ref("");
const rightKind = ref<Kind>("remote");
const rightHost = ref("");

function swap() {
  const kind = leftKind.value;
  const host = leftHost.value;
  leftKind.value = rightKind.value;
  leftHost.value = rightHost.value;
  rightKind.value = kind;
  rightHost.value = host;
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

onBeforeUnmount(() => {
  try {
    void SSH.SftpCloseAll();
  } catch {
    /* 忽略 */
  }
});
</script>

<template>
  <section class="sftp-view">
    <header class="sf-bar">
      <span class="hint">把文件拖到另一侧面板即可传输</span>
      <button class="swap" title="交换左右两侧" @click="swap">⇄ 左右互换</button>
    </header>

    <div class="sf-panes">
      <SftpSide
        side="L"
        :hosts="props.hosts"
        v-model:kind="leftKind"
        v-model:host-id="leftHost"
      />
      <SftpSide
        side="R"
        :hosts="props.hosts"
        v-model:kind="rightKind"
        v-model:host-id="rightHost"
      />
    </div>

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
}
.hint {
  color: var(--win-text-3);
  font-size: 11.5px;
  white-space: nowrap;
}
.swap {
  margin-left: auto;
  flex: none;
  height: 28px;
  padding: 0 12px;
  border-radius: 8px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text-2);
  font-size: 12px;
  cursor: default;
}
.swap:hover {
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.sf-panes {
  flex: 1;
  min-height: 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 10px;
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
