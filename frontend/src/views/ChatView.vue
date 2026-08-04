<template>
  <div class="chat-page">
    <header class="header">
      <div class="logo">AI <span>TRPG</span></div>
      <div class="spacer"></div>
      <div class="conn" :class="connClass">
        <div class="dot"></div>
        <span>{{ connText }}</span>
      </div>
      <router-link class="admin-link" to="/admin">管理后台 →</router-link>
    </header>

    <div class="main">
      <div class="chat-wrap">
        <div class="messages" ref="messagesEl">
          <div v-for="(m, i) in messages" :key="i" class="msg" :class="m.type">
            <div class="bubble">{{ m.text }}</div>
          </div>
          <div class="msg thinking" :class="{ show: thinking }">
            <div class="bubble"><div class="dot"></div><div class="dot"></div><div class="dot"></div></div>
          </div>
        </div>

        <div class="input-bar">
          <button class="btn btn-icon" title="上传剧本文件" @click="fileInputEl.click()">📎</button>
          <div class="input-box">
            <div class="cmd-hint" :class="{ show: showCmdHint }">.help 帮助 · .mode trpg 跑团模式 · .script list 剧本列表 · .r 1d20 骰子 · .progress 进度</div>
            <textarea
              ref="inputEl"
              v-model="inputText"
              rows="1"
              placeholder="描述你的行动，或输入 . 开头的指令…"
              @keydown="onKeydown"
              @input="onInput"
            ></textarea>
          </div>
          <button class="btn btn-send" @click="send">发送</button>
        </div>
        <input ref="fileInputEl" type="file" hidden accept=".pdf,.docx,.txt,.md" @change="onFileChange">
      </div>

      <aside class="sidebar">
        <div class="side-block">
          <div class="side-title">会话模式</div>
          <div class="mode-list">
            <div
              v-for="m in modes"
              :key="m.key"
              class="mode-item"
              :class="{ active: currentMode === m.key }"
              @click="switchMode(m.key)"
            >{{ m.label }}</div>
          </div>
        </div>
        <div class="side-block">
          <div class="side-title">快捷指令</div>
          <div class="quick-cmds">
            <span v-for="c in quickCmds" :key="c" class="qcmd" @click="sendQuick(c)">{{ c }}</span>
          </div>
        </div>
        <div class="side-block">
          <div class="side-title">提示</div>
          <div class="side-note">上传剧本文件（PDF/Word）后发送 <b>.script upload 路径</b> 即可 AI 识别。剧本加载后切换到跑团模式开始游戏。</div>
        </div>
      </aside>
    </div>
  </div>
</template>

<script setup>
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'

const messagesEl = ref(null)
const inputEl = ref(null)
const fileInputEl = ref(null)

const messages = ref([])
const thinking = ref(false)
const connClass = ref('')
const connText = ref('连接中…')
const inputText = ref('')
const showCmdHint = ref(false)
const currentMode = ref('')

const modes = [
  { key: 'normal', label: '普通模式' },
  { key: 'trpg', label: '跑团模式' },
  { key: 'freechat', label: '自由聊天' },
]
const quickCmds = ['.help', '.script list', '.progress', '.timeline', '.r 1d100', '.log start', '.log end']

let ws = null
let token = localStorage.getItem('trpg_token') || ''
let destroyed = false

function setConn(on, text) {
  connClass.value = on ? 'on' : 'off'
  connText.value = text
}

function addMsg(type, text) {
  messages.value.push({ type, text })
  nextTick(() => {
    if (messagesEl.value) messagesEl.value.scrollTop = messagesEl.value.scrollHeight
  })
}

function setThinking(on) {
  thinking.value = on
  if (on) {
    nextTick(() => {
      if (messagesEl.value) messagesEl.value.scrollTop = messagesEl.value.scrollHeight
    })
  }
}

async function ensureToken() {
  if (token) return token
  const resp = await fetch('/api/session')
  const data = await resp.json()
  token = data.token
  localStorage.setItem('trpg_token', token)
  return token
}

function connect() {
  ensureToken().then(() => {
    if (destroyed) return
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    ws = new WebSocket(`${proto}://${location.host}/ws/chat?token=${token}`)

    ws.onopen = () => setConn(true, '已连接')
    ws.onclose = () => {
      if (destroyed) return
      setConn(false, '已断开，重连中…')
      setTimeout(connect, 2000)
    }
    ws.onerror = () => setConn(false, '连接异常')
    ws.onmessage = (e) => {
      const msg = JSON.parse(e.data)
      switch (msg.type) {
        case 'reply': addMsg('kp', msg.text); break
        case 'push': addMsg('push', msg.text); break
        case 'session': addMsg('system', msg.text); break
        case 'progress': addMsg('system', msg.text); break
        case 'error': addMsg('system', '⚠ ' + msg.text); break
        case 'status': setThinking(msg.state === 'thinking'); break
      }
    }
  })
}

function send() {
  const text = inputText.value.trim()
  if (!text || !ws || ws.readyState !== WebSocket.OPEN) return
  addMsg('user', text)
  ws.send(JSON.stringify({ type: 'chat', text }))
  inputText.value = ''
  if (inputEl.value) inputEl.value.style.height = 'auto'
  showCmdHint.value = false
}

function onKeydown(e) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    send()
  }
}

function onInput() {
  const el = inputEl.value
  if (el) {
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 140) + 'px'
  }
  showCmdHint.value = inputText.value === '.'
}

function switchMode(mode) {
  currentMode.value = mode
  inputText.value = '.mode ' + mode
  send()
}

function sendQuick(cmd) {
  inputText.value = cmd
  send()
}

async function onFileChange() {
  const input = fileInputEl.value
  if (!input || !input.files.length) return
  const fd = new FormData()
  fd.append('file', input.files[0])
  addMsg('system', '正在上传: ' + input.files[0].name + ' …')
  try {
    const resp = await fetch('/api/upload', { method: 'POST', body: fd })
    const data = await resp.json()
    if (data.path) {
      inputText.value = '.script upload ' + data.path
      send()
    } else {
      addMsg('system', '⚠ 上传失败')
    }
  } catch (err) {
    addMsg('system', '⚠ 上传失败: ' + err.message)
  }
  input.value = ''
}

onMounted(connect)
onBeforeUnmount(() => {
  destroyed = true
  if (ws) ws.close()
})
</script>

<style scoped>
.chat-page { height: 100%; display: flex; flex-direction: column; }

/* ===== 顶栏 ===== */
.header {
  height: 56px; flex: none;
  display: flex; align-items: center; gap: 12px;
  padding: 0 20px;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
}
.logo { font-size: 17px; font-weight: 700; letter-spacing: .5px; }
.logo span { color: var(--primary); }
.header .spacer { flex: 1; }
.conn {
  display: flex; align-items: center; gap: 6px;
  font-size: 12px; color: var(--text-secondary);
}
.conn .dot { width: 8px; height: 8px; border-radius: 50%; background: #d1d5db; transition: background .3s; }
.conn.on .dot { background: #22c55e; }
.conn.off .dot { background: #ef4444; }
.admin-link {
  font-size: 13px; color: var(--text-secondary); text-decoration: none;
  padding: 6px 12px; border-radius: 8px; transition: all .2s;
}
.admin-link:hover { background: var(--bg); color: var(--text); }

/* ===== 主体 ===== */
.main { flex: 1; display: flex; min-height: 0; }

.chat-wrap { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.messages {
  flex: 1; overflow-y: auto; padding: 24px 20px 12px;
  display: flex; flex-direction: column; gap: 14px;
  scroll-behavior: smooth;
}
.msg { display: flex; max-width: 78%; animation: fadeUp .25s ease; }
@keyframes fadeUp { from { opacity: 0; transform: translateY(6px); } to { opacity: 1; transform: none; } }
.msg .bubble {
  padding: 12px 16px; border-radius: var(--radius);
  line-height: 1.75; font-size: 14.5px;
  white-space: pre-wrap; word-break: break-word;
  box-shadow: var(--shadow);
}
.msg.kp { align-self: flex-start; }
.msg.kp .bubble { background: var(--surface); border: 1px solid var(--border); border-top-left-radius: 4px; }
.msg.user { align-self: flex-end; }
.msg.user .bubble { background: var(--primary); color: #fff; border-top-right-radius: 4px; }
.msg.system { align-self: center; max-width: 92%; }
.msg.system .bubble {
  background: var(--primary-soft); color: #41527a; box-shadow: none;
  font-size: 13px; border-radius: 10px;
}
.msg.push { align-self: center; max-width: 92%; }
.msg.push .bubble {
  background: #fff8e6; color: #92660a; box-shadow: none;
  font-size: 13px; border-radius: 10px; border: 1px solid #f5e3b3;
}

/* 思考动画 */
.thinking { display: none; align-self: flex-start; }
.thinking.show { display: flex; }
.thinking .bubble { display: flex; gap: 5px; padding: 14px 18px; background: var(--surface); border: 1px solid var(--border); }
.thinking .dot { width: 7px; height: 7px; border-radius: 50%; background: #b6bcc8; animation: bounce 1.2s infinite; }
.thinking .dot:nth-child(2) { animation-delay: .15s; }
.thinking .dot:nth-child(3) { animation-delay: .3s; }
@keyframes bounce { 0%, 60%, 100% { transform: none; opacity: .5; } 30% { transform: translateY(-5px); opacity: 1; } }

/* 输入区 */
.input-bar {
  flex: none; padding: 14px 20px 18px;
  background: var(--surface); border-top: 1px solid var(--border);
  display: flex; gap: 10px; align-items: flex-end;
}
.input-box {
  flex: 1; position: relative;
  background: var(--bg); border: 1px solid var(--border); border-radius: 12px;
  transition: border-color .2s;
}
.input-box:focus-within { border-color: var(--primary); }
textarea {
  width: 100%; border: none; outline: none; resize: none;
  background: transparent; padding: 12px 14px;
  font: inherit; font-size: 14.5px; line-height: 1.6;
  max-height: 140px; display: block;
}
.cmd-hint {
  position: absolute; bottom: 100%; left: 0; margin-bottom: 6px;
  background: var(--surface); border: 1px solid var(--border);
  border-radius: 10px; box-shadow: var(--shadow);
  font-size: 12.5px; color: var(--text-secondary);
  padding: 8px 12px; display: none; white-space: pre-line;
}
.cmd-hint.show { display: block; }
.btn {
  flex: none; height: 42px; padding: 0 18px;
  border: none; border-radius: 12px; cursor: pointer;
  font: inherit; font-size: 14px; font-weight: 600;
  transition: all .2s;
}
.btn-send { background: var(--primary); color: #fff; }
.btn-send:hover { background: #2f5cd8; }
.btn-send:disabled { background: #c3cfe8; cursor: not-allowed; }
.btn-icon { background: var(--bg); border: 1px solid var(--border); color: var(--text-secondary); padding: 0 13px; }
.btn-icon:hover { color: var(--text); border-color: #c9cdd4; }

/* 侧栏 */
.sidebar {
  width: 264px; flex: none;
  background: var(--surface); border-left: 1px solid var(--border);
  padding: 18px 16px; overflow-y: auto;
  display: flex; flex-direction: column; gap: 18px;
}
.side-block .side-title {
  font-size: 12px; font-weight: 600; color: var(--text-secondary);
  text-transform: uppercase; letter-spacing: 1px; margin-bottom: 10px;
}
.mode-list { display: flex; flex-direction: column; gap: 6px; }
.mode-item {
  padding: 9px 12px; border-radius: 9px; font-size: 13.5px; cursor: pointer;
  border: 1px solid var(--border); transition: all .15s;
  display: flex; justify-content: space-between; align-items: center;
}
.mode-item:hover { border-color: #c9cdd4; }
.mode-item.active { background: var(--primary-soft); border-color: var(--primary); color: var(--primary); font-weight: 600; }
.quick-cmds { display: flex; flex-wrap: wrap; gap: 6px; }
.qcmd {
  font-size: 12px; padding: 5px 10px; border-radius: 999px;
  background: var(--bg); border: 1px solid var(--border);
  cursor: pointer; color: var(--text-secondary); transition: all .15s;
}
.qcmd:hover { color: var(--primary); border-color: var(--primary); }
.side-note { font-size: 12px; color: var(--text-secondary); line-height: 1.7; }

@media (max-width: 860px) {
  .sidebar { display: none; }
  .msg { max-width: 92%; }
}
</style>
