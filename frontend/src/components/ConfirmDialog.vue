<script setup lang="ts">
import { nextTick, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

withDefaults(
  defineProps<{
    title: string;
    message: string;
    confirmText: string;
    danger?: boolean;
  }>(),
  { danger: true },
);

const emit = defineEmits<{
  (e: "confirm"): void;
  (e: "cancel"): void;
}>();

const { t } = useI18n();
const root = ref<HTMLElement>();

onMounted(() => {
  void nextTick(() => root.value?.focus());
});
</script>

<template>
  <Teleport to="body">
    <div
      ref="root"
      class="dlg-mask"
      tabindex="-1"
      @click.self="emit('cancel')"
      @keydown.esc="emit('cancel')"
    >
      <div class="dlg" role="alertdialog" aria-modal="true">
        <div class="dlg-head">
          <span class="dlg-icon" :class="{ danger }">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M12 3.4L2.7 19.5a1.4 1.4 0 0 0 1.2 2.1h18.2a1.4 1.4 0 0 0 1.2-2.1L13.2 3.4a1.4 1.4 0 0 0-2.4 0z"
              />
              <path d="M12 9.6v4.6M12 17.9v.1" />
            </svg>
          </span>
          <h3>{{ title }}</h3>
        </div>
        <p>{{ message }}</p>
        <div class="dlg-actions">
          <button class="ghost" @click="emit('cancel')">{{ t("common.cancel") }}</button>
          <button :class="danger ? 'danger' : 'primary'" @click="emit('confirm')">
            {{ confirmText }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.dlg-mask {
  position: fixed;
  inset: 0;
  z-index: 330;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(8, 10, 14, 0.62);
  backdrop-filter: blur(2px);
  outline: none;
  animation: dlg-fade 0.12s ease-out;
}
.dlg {
  width: min(400px, 92vw);
  padding: 20px;
  border: 1px solid var(--win-stroke-strong);
  border-radius: 14px;
  background: var(--win-flyout);
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.55), 0 2px 8px rgba(0, 0, 0, 0.35);
  animation: dlg-pop 0.14s ease-out;
}
.dlg-head {
  display: flex;
  align-items: center;
  gap: 11px;
}
.dlg-icon {
  flex: none;
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: 9px;
  background: rgba(255, 180, 84, 0.14);
  color: var(--win-warn);
}
.dlg-icon.danger {
  background: rgba(255, 107, 107, 0.14);
  color: var(--win-err);
}
.dlg-icon svg {
  width: 17px;
  height: 17px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.4;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.dlg h3 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--win-text);
}
.dlg p {
  margin: 12px 0 0;
  color: var(--win-text-2);
  font-size: 12.5px;
  line-height: 1.6;
  overflow-wrap: anywhere;
}
.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 18px;
}
.dlg-actions button {
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
.danger {
  background: var(--win-err);
  color: #fff;
}
.danger:hover {
  filter: brightness(1.08);
}
.primary {
  background: var(--win-accent);
  color: var(--win-accent-text);
}
@keyframes dlg-fade {
  from {
    opacity: 0;
  }
}
@keyframes dlg-pop {
  from {
    opacity: 0;
    transform: translateY(6px) scale(0.97);
  }
}
</style>
