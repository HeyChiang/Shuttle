// SFTP 页面内部跨面板拖拽的共享状态。
// HTML5 拖拽的 dataTransfer 在 dragover 阶段无法读取数据，因此用模块级状态传递来源。
import { ref } from "vue";

export type SftpDragPayload =
  | { kind: "local"; paths: string[] }
  | { kind: "remote"; token: string; paths: string[] };

export const sftpDrag = ref<SftpDragPayload | null>(null);

export function beginSftpDrag(payload: SftpDragPayload) {
  sftpDrag.value = payload;
}

export function endSftpDrag() {
  sftpDrag.value = null;
}
