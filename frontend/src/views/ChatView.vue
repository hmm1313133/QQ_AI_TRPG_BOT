<template>
  <div class="chat-page">
    <header class="header">
      <div class="logo">AI <span>TRPG</span></div>
      <div class="spacer"></div>
      <div class="conn" :class="connClass">
        <div class="dot"></div>
        <span>{{ connText }}</span>
      </div>
      <button class="header-btn" @click="openSaves">存档</button>
      <router-link class="admin-link" to="/admin">管理后台 →</router-link>
    </header>

    <!-- 游玩存档 -->
    <el-dialog v-model="savesVisible" title="游玩存档" width="560px">
      <div v-if="savesNoWorld" class="empty">当前会话还没有进行中的世界（先加载剧本或创建世界）</div>
      <template v-else>
        <div class="save-create">
          <el-input v-model="saveName" size="small" placeholder="存档名称（必填）" style="width:180px" />
          <el-input v-model="saveNote" size="small" placeholder="备注（可选）" style="flex:1" />
          <el-button type="primary" size="small" :loading="saveCreating" @click="createSave">新建存档</el-button>
        </div>
        <div v-if="savesLoading" class="empty">加载中…</div>
        <el-table v-else :data="saves" size="small" empty-text="暂无存档">
          <el-table-column prop="name" label="名称" min-width="110">
            <template #default="{ row }">
              {{ row.name }}
              <el-tag v-if="row.auto" size="small" type="info" effect="plain" style="margin-left:4px">自动</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="round_count" label="轮次" width="60" />
          <el-table-column label="时间" width="145">
            <template #default="{ row }">{{ fmtSaveTime(row.created_at) }}</template>
          </el-table-column>
          <el-table-column prop="note" label="备注" min-width="90" show-overflow-tooltip />
          <el-table-column label="操作" width="80">
            <template #default="{ row }">
              <el-button size="small" @click="restoreSave(row)">恢复</el-button>
            </template>
          </el-table-column>
        </el-table>
      </template>
    </el-dialog>

    <div class="main">
      <div class="chat-wrap">
        <div class="messages" ref="messagesEl">
          <div v-for="(m, i) in messages" :key="i" class="msg" :class="m.type">
            <div class="bubble"><MarkdownText :text="m.text" /></div>
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
          <div class="side-title">回复长度</div>
          <div class="mode-list">
            <div
              v-for="l in lengths"
              :key="l.key"
              class="mode-item"
              :class="{ active: currentLength === l.key }"
              @click="switchLength(l.key)"
            >{{ l.label }}</div>
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
import { ElMessage, ElMessageBox } from 'element-plus'
import { playerSaveReq } from '../api/admin'
import MarkdownText from '../components/MarkdownText.vue'

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
const currentLength = ref('standard')

const modes = [
  { key: 'normal', label: '普通模式' },
  { key: 'trpg', label: '跑团模式' },
  { key: 'freechat', label: '自由聊天' },
]
const lengths = [
  { key: 'short', label: '简短（≤150字）' },
  { key: 'standard', label: '标准（~300字）' },
  { key: 'detailed', label: '详细（~600字）' },
]
const quickCmds = ['.help', '.script list', '.progress', '.timeline', '.r 1d100', '.log start', '.log end']

let ws = null
let token = localStorage.getItem('trpg_token') || ''
let destroyed = false

// 聊天访问令牌（部署方开启 ChatToken 时通过 ?auth= 带入，与 WS 鉴权参数一致）
const chatAuth = new URLSearchParams(location.search).get('auth') || ''

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

// 加载历史消息（后端按时间升序返回最近 200 条）
async function loadHistory() {
  const t = await ensureToken()
  let url = '/api/chat/history?token=' + encodeURIComponent(t)
  if (chatAuth) url += '&auth=' + encodeURIComponent(chatAuth)
  const resp = await fetch(url)
  if (!resp.ok) throw new Error(await resp.text())
  const list = await resp.json()
  const typeMap = { reply: 'kp', user: 'user', push: 'push' }
  messages.value = (list || [])
    .filter((m) => typeMap[m.type])
    .map((m) => ({ type: typeMap[m.type], text: m.text }))
  nextTick(() => {
    if (messagesEl.value) messagesEl.value.scrollTop = messagesEl.value.scrollHeight
  })
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

// 回复长度复用指令链路（.length xxx），无需新 API
function switchLength(key) {
  currentLength.value = key
  inputText.value = '.length ' + key
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

// 先恢复历史再连 WS：历史请求在 connect 之前 await，天然无竞态
onMounted(async () => {
  try {
    await loadHistory()
  } catch (err) {
    console.warn('加载历史消息失败:', err)
  }
  connect()
})
onBeforeUnmount(() => {
  destroyed = true
  if (ws) ws.close()
})

// ---------- 游玩存档 ----------

const savesVisible = ref(false)
const saves = ref([])
const savesLoading = ref(false)
const savesNoWorld = ref(false)
const saveName = ref('')
const saveNote = ref('')
const saveCreating = ref(false)

function fmtSaveTime(s) {
  return (s || '').replace('T', ' ').slice(0, 16) || '-'
}

async function openSaves() {
  savesVisible.value = true
  await loadSaves()
}

async function loadSaves() {
  savesLoading.value = true
  savesNoWorld.value = false
  try {
    const t = await ensureToken()
    saves.value = (await playerSaveReq('/api/saves', { token: t, auth: chatAuth })) || []
  } catch (e) {
    if (e.status === 404) {
      savesNoWorld.value = true
      saves.value = []
    } else {
      ElMessage.error('加载存档失败：' + e.message)
    }
  } finally {
    savesLoading.value = false
  }
}

async function createSave() {
  const name = saveName.value.trim()
  if (!name) { ElMessage.warning('请填写存档名称'); return }
  saveCreating.value = true
  try {
    const t = await ensureToken()
    await playerSaveReq('/api/saves', {
      token: t,
      auth: chatAuth,
      method: 'POST',
      body: JSON.stringify({ name, note: saveNote.value.trim() }),
    })
    ElMessage.success('存档已创建')
    saveName.value = ''
    saveNote.value = ''
    await loadSaves()
  } catch (e) {
    if (e.status === 404) {
      savesNoWorld.value = true
    } else {
      ElMessage.error('创建存档失败：' + e.message)
    }
  } finally {
    saveCreating.value = false
  }
}

async function restoreSave(row) {
  try {
    await ElMessageBox.confirm(
      `恢复到存档「${row.name}」（第 ${row.round_count} 轮）？当前进度会先自动备份。`,
      '恢复存档',
      { type: 'warning', confirmButtonText: '恢复', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    const t = await ensureToken()
    const r = await playerSaveReq(`/api/saves/${row.id}/restore`, { token: t, auth: chatAuth, method: 'POST' })
    ElMessage.success(r?.message || '已恢复')
    savesVisible.value = false
    // 重新加载历史：存档带对话快照时服务端已回放，聊天窗口显示存档时刻的记录
    try {
      await loadHistory()
    } catch (err) {
      console.warn('恢复后加载历史失败:', err)
    }
    addMsg('system', `📦 ${r?.message || '已恢复存档'}`)
  } catch (e) {
    ElMessage.error('恢复失败：' + e.message)
  }
}
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
.header-btn {
  font: inherit; font-size: 13px; color: var(--text-secondary);
  padding: 6px 12px; border-radius: 8px; border: none;
  background: transparent; cursor: pointer; transition: all .2s;
}
.header-btn:hover { background: var(--bg); color: var(--text); }
.save-create { display: flex; gap: 8px; margin-bottom: 12px; }
.empty { padding: 24px 0; text-align: center; color: var(--text-secondary); font-size: 13px; }

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
  word-break: break-word;
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

/* ===== 气泡内 markdown 内容 ===== */
.msg .bubble :deep(.md-body) > :first-child { margin-top: 0; }
.msg .bubble :deep(.md-body) > :last-child { margin-bottom: 0; }
.msg .bubble :deep(.md-body p) { margin: 6px 0; }
.msg .bubble :deep(.md-body h1),
.msg .bubble :deep(.md-body h2),
.msg .bubble :deep(.md-body h3),
.msg .bubble :deep(.md-body h4) {
  margin: 12px 0 6px; line-height: 1.4; font-weight: 700;
}
.msg .bubble :deep(.md-body h1) { font-size: 1.3em; }
.msg .bubble :deep(.md-body h2) { font-size: 1.2em; }
.msg .bubble :deep(.md-body h3) { font-size: 1.1em; }
.msg .bubble :deep(.md-body h4) { font-size: 1em; }
.msg .bubble :deep(.md-body ul),
.msg .bubble :deep(.md-body ol) { margin: 6px 0; padding-left: 22px; }
.msg .bubble :deep(.md-body li) { margin: 2px 0; }
.msg .bubble :deep(.md-body code) {
  font-family: ui-monospace, "Cascadia Code", Consolas, monospace; font-size: .9em;
  background: var(--primary-soft); color: var(--primary);
  padding: 1px 6px; border-radius: 6px;
}
.msg .bubble :deep(.md-body pre) {
  margin: 8px 0; padding: 10px 14px; overflow-x: auto;
  background: var(--bg); border: 1px solid var(--border); border-radius: 10px;
}
.msg .bubble :deep(.md-body pre code) { background: none; color: var(--text); padding: 0; }
.msg .bubble :deep(.md-body blockquote) {
  margin: 8px 0; padding: 2px 12px;
  border-left: 3px solid var(--primary); color: var(--text-secondary);
}
.msg .bubble :deep(.md-body table) { margin: 8px 0; border-collapse: collapse; font-size: .95em; }
.msg .bubble :deep(.md-body th),
.msg .bubble :deep(.md-body td) { border: 1px solid var(--border); padding: 4px 10px; }
.msg .bubble :deep(.md-body th) { background: var(--bg); }
.msg .bubble :deep(.md-body tr:nth-child(even) td) { background: var(--bg); }
.msg .bubble :deep(.md-body a) { color: var(--primary); text-decoration: none; }
.msg .bubble :deep(.md-body a:hover) { text-decoration: underline; }
.msg .bubble :deep(.md-body hr) { border: none; border-top: 1px solid var(--border); margin: 10px 0; }
.msg .bubble :deep(.md-body img) { max-width: 100%; border-radius: 8px; }

/* user 气泡为深色底，内部元素适配白字 */
.msg.user .bubble :deep(.md-body code) { background: rgba(255, 255, 255, .18); color: #fff; }
.msg.user .bubble :deep(.md-body pre) { background: rgba(0, 0, 0, .18); border-color: rgba(255, 255, 255, .25); }
.msg.user .bubble :deep(.md-body pre code) { background: none; color: #fff; }
.msg.user .bubble :deep(.md-body blockquote) { border-left-color: rgba(255, 255, 255, .6); color: rgba(255, 255, 255, .85); }
.msg.user .bubble :deep(.md-body a) { color: #fff; text-decoration: underline; }
.msg.user .bubble :deep(.md-body th),
.msg.user .bubble :deep(.md-body td) { border-color: rgba(255, 255, 255, .35); }
.msg.user .bubble :deep(.md-body th),
.msg.user .bubble :deep(.md-body tr:nth-child(even) td) { background: rgba(255, 255, 255, .1); }
.msg.user .bubble :deep(.md-body hr) { border-top-color: rgba(255, 255, 255, .35); }

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
