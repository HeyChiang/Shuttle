<script setup lang="ts">
import { useI18n } from "vue-i18n";

defineProps<{
  x: number;
  y: number;
  /** 选中数量为 1 时才允许重命名 */
  canRename: boolean;
  /** 当前选中数量，用于批量删除提示 */
  count: number;
}>();

const emit = defineEmits<{
  (e: "rename"): void;
  (e: "delete"): void;
  (e: "refresh"): void;
  (e: "close"): void;
}>();

const { t } = useI18n();
</script>

<template>
  <Teleport to="body">
    <div class="ctx-backdrop" @pointerdown="emit('close')" @contextmenu.prevent="emit('close')" />
    <div
      class="ctx-menu"
      :style="{ left: `${x}px`, top: `${y}px` }"
      @pointerdown.stop
      @contextmenu.prevent
    >
      <button v-if="canRename" class="ctx-item" @click="emit('rename')">
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <path d="M3.1 12.9l.6-3.1 7.2-7.2 2.5 2.5-7.2 7.2z" />
          <path d="M9.7 3.9l2.5 2.5" />
        </svg>
        {{ t("common.rename") }}
      </button>
      <button class="ctx-item danger" @click="emit('delete')">
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <path d="M3.2 4.5h9.6M6.4 4.5V3h3.2v1.5" />
          <path d="M4.6 4.5l.5 8.2h5.8l.5-8.2" />
        </svg>
        {{ t("common.delete") }}
        <span v-if="count > 1" class="ctx-count">{{ count }}</span>
      </button>
      <div class="ctx-sep" />
      <button class="ctx-item" @click="emit('refresh')">
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <path d="M13.2 8a5.2 5.2 0 1 1-1.6-3.8" />
          <path d="M13.4 2.6v3h-3" />
        </svg>
        {{ t("common.refresh") }}
      </button>
    </div>
  </Teleport>
</template>

<style scoped>
.ctx-backdrop {
  position: fixed;
  inset: 0;
  z-index: 320;
}
.ctx-menu {
  position: fixed;
  z-index: 321;
  min-width: 170px;
  padding: 5px;
  border: 1px solid var(--win-stroke-strong);
  border-radius: 10px;
  background: var(--win-flyout);
  box-shadow: 0 18px 44px rgba(0, 0, 0, 0.5), 0 2px 8px rgba(0, 0, 0, 0.32);
  animation: ctx-in 0.1s ease-out;
}
.ctx-item {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  height: 30px;
  padding: 0 9px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--win-text);
  font-size: 12.5px;
  text-align: left;
  cursor: default;
}
.ctx-item svg {
  flex: none;
  width: 14px;
  height: 14px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.25;
  stroke-linecap: round;
  stroke-linejoin: round;
  opacity: 0.8;
}
.ctx-item:hover {
  background: var(--win-fill-hover);
}
.ctx-item.danger:hover {
  background: rgba(255, 107, 107, 0.14);
  color: var(--win-err);
}
.ctx-count {
  display: grid;
  place-items: center;
  min-width: 18px;
  height: 18px;
  margin-left: auto;
  padding: 0 5px;
  border-radius: 9px;
  background: var(--win-fill-press);
  color: var(--win-text-2);
  font-size: 10.5px;
  font-variant-numeric: tabular-nums;
}
.ctx-sep {
  height: 1px;
  margin: 4px 6px;
  background: var(--win-stroke);
}
@keyframes ctx-in {
  from {
    opacity: 0;
    transform: scale(0.97);
  }
}
</style>
