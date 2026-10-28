<script setup lang="ts">
// 连接档案编辑弹窗。传入 host 时打开；保存后通过 submit 事件回传副本。
import { reactive, ref, watch } from "vue";
import * as SSH from "../../bindings/shuttle/sshservice";
import type { HostConfig } from "../../bindings/shuttle/internal/sshcore/models";
import { blankHost, encodings } from "../store";

const props = defineProps<{ host: HostConfig | null }>();
const emit = defineEmits<{
  (e: "submit", host: HostConfig): void;
  (e: "cancel"): void;
}>();

const form = ref<HostConfig>(blankHost());
const error = ref("");

watch(
  () => props.host,
  (h) => {
    error.value = "";
    form.value = h ? { ...h } : blankHost();
  },
  { immediate: true },
);

async function pickKey() {
  const p = await SSH.PickFile("选择私钥文件");
  if (p) form.value.privateKeyPath = p;
}

function submit() {
  const f = form.value;
  if (!f.host.trim()) {
    error.value = "请填写主机地址";
    return;
  }
  if (!f.user.trim()) {
    error.value = "请填写登录用户";
    return;
  }
  if (!f.name.trim()) {
    f.name = `${f.user}@${f.host}`;
  }
  emit("submit", { ...f });
}
</script>

<template>
  <div class="mask" @click.self="emit('cancel')">
    <div class="dialog">
      <header>
        <h3>{{ host ? "编辑连接" : "新建连接" }}</h3>
        <button class="close" title="关闭" @click="emit('cancel')">
          <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
            <path
              d="M3.5 3.5l9 9M12.5 3.5l-9 9"
              fill="none"
              stroke="currentColor"
              stroke-width="1.2"
              stroke-linecap="round"
            />
          </svg>
        </button>
      </header>

      <div class="form">
        <label class="span2">
          <span>名称</span>
          <input v-model="form.name" placeholder="例如：生产环境-Web01" />
        </label>

        <label>
          <span>分组</span>
          <input v-model="form.group" placeholder="默认分组" />
        </label>

        <label>
          <span>主机地址</span>
          <input v-model="form.host" placeholder="192.168.1.10 或 example.com" />
        </label>

        <label>
          <span>端口</span>
          <input v-model.number="form.port" type="number" min="1" max="65535" />
        </label>

        <label>
          <span>登录用户</span>
          <input v-model="form.user" placeholder="root" />
        </label>

        <label class="span2">
          <span>认证方式</span>
          <select v-model="form.authMethod">
            <option value="password">密码</option>
            <option value="key">私钥文件</option>
            <option value="agent">SSH Agent / Pageant</option>
          </select>
        </label>

        <label v-if="form.authMethod === 'password'" class="span2">
          <span>密码</span>
          <input v-model="form.password" type="password" placeholder="登录密码" />
        </label>

        <template v-if="form.authMethod === 'key'">
          <label class="span2">
            <span>私钥文件</span>
            <div class="row">
              <input v-model="form.privateKeyPath" placeholder="C:\\Users\\me\\.ssh\\id_rsa" />
              <button type="button" @click="pickKey">浏览…</button>
            </div>
          </label>
          <label class="span2">
            <span>私钥口令（可选）</span>
            <input v-model="form.keyPassphrase" type="password" placeholder="留空表示私钥无口令" />
          </label>
        </template>

        <label>
          <span>字符编码</span>
          <select v-model="form.encoding">
            <option v-for="e in encodings" :key="e" :value="e">{{ e }}</option>
          </select>
        </label>

        <label>
          <span>终端类型</span>
          <input v-model="form.termType" placeholder="xterm-256color" />
        </label>

        <label>
          <span>初始列数</span>
          <input v-model.number="form.cols" type="number" min="20" max="500" />
        </label>

        <label>
          <span>初始行数</span>
          <input v-model.number="form.rows" type="number" min="10" max="200" />
        </label>

        <label class="span2">
          <span>连接超时（秒）</span>
          <input v-model.number="form.timeoutSec" type="number" min="3" max="300" />
        </label>
      </div>

      <p v-if="error" class="error">{{ error }}</p>

      <footer>
        <button class="ghost" @click="emit('cancel')">取消</button>
        <button class="primary" @click="submit">{{ host ? "保存" : "创建并保存" }}</button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.mask {
  position: fixed;
  inset: 0;
  background: rgba(8, 10, 14, 0.62);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 50;
}
.dialog {
  width: min(620px, 92vw);
  max-height: 88vh;
  display: flex;
  flex-direction: column;
  background: var(--win-flyout);
  border: 1px solid var(--win-stroke-strong);
  border-radius: 12px;
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.55);
  overflow: hidden;
}
header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px;
  border-bottom: 1px solid var(--win-divider);
}
h3 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--win-text);
}
.close {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border: 0;
  border-radius: var(--win-radius);
  background: transparent;
  color: var(--win-text-2);
  cursor: default;
  font-size: 12px;
}
.close:hover {
  background: var(--win-fill-hover);
  color: var(--win-text);
}
.close:active {
  background: var(--win-fill-press);
}
.form {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px 16px;
  padding: 18px;
  overflow: auto;
}
.span2 {
  grid-column: span 2;
}
label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  color: var(--win-text-3);
}
input,
select {
  height: 32px;
  padding: 0 10px;
  border-radius: 8px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text);
  font-size: 12px;
  outline: none;
  transition: border-color 0.12s ease, box-shadow 0.12s ease;
}
input::placeholder {
  color: var(--win-text-disabled);
}
input:hover:not(:focus),
select:hover:not(:focus) {
  background: var(--win-fill-hover);
}
input:focus,
select:focus {
  border-color: var(--win-accent);
  box-shadow: 0 0 0 3px var(--win-accent-soft);
}
select option {
  background: var(--win-flyout);
  color: var(--win-text);
}
.row {
  display: flex;
  gap: 8px;
}
.row input {
  flex: 1;
  min-width: 0;
}
.row button {
  height: 32px;
  padding: 0 12px;
  border-radius: 8px;
  border: 1px solid var(--win-stroke);
  background: var(--win-fill);
  color: var(--win-text);
  cursor: default;
  font-size: 12px;
  white-space: nowrap;
}
.row button:hover {
  background: var(--win-fill-hover);
}
.row button:active {
  background: var(--win-fill-press);
}
.error {
  margin: 0;
  padding: 0 18px 10px;
  color: var(--win-err);
  font-size: 12px;
}
footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 14px 18px;
  border-top: 1px solid var(--win-divider);
}
footer button {
  height: 32px;
  padding: 0 18px;
  border-radius: 8px;
  font-size: 13px;
  cursor: default;
  border: 1px solid transparent;
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
</style>