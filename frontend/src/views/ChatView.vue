<template>
  <div class="chat-page">
    <header class="header">
      <div class="logo">AI <span>世界引擎</span></div>
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
      <div v-if="savesNoWorld" class="empty">当前会话还没有进行中的世界（先在右侧进入一个世界）</div>
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

    <!-- 玩家人设 -->
    <el-dialog v-model="personaVisible" title="玩家人设" width="480px">
      <el-form label-width="70px" label-position="left">
        <el-form-item label="范围">
          <el-radio-group v-model="personaScope" @change="loadPersona">
            <el-radio value="global">全局默认</el-radio>
            <el-radio value="world" :disabled="!currentWorldId">仅本世界</el-radio>
          </el-radio-group>
          <div v-if="!currentWorldId" class="muted" style="margin-top:4px">进入世界后可设置仅该世界生效的人设</div>
        </el-form-item>
        <el-form-item label="名字">
          <el-input v-model="personaName" placeholder="你在游戏中扮演的角色名" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="personaDesc" type="textarea" :rows="4"
            placeholder="自由描述：性格/外貌/行事风格等" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button size="small" :disabled="!personaName && !personaDesc" @click="clearPersona">清除人设</el-button>
        <el-button size="small" @click="personaVisible = false">取消</el-button>
        <el-button type="primary" size="small" :loading="personaSaving" @click="applyPersona">确定</el-button>
      </template>
    </el-dialog>

    <!-- 进入世界 -->
    <el-dialog v-model="worldsVisible" title="进入世界" width="520px">
      <div v-if="currentWorldId" class="empty" style="padding:0 0 10px">
        当前正在世界「{{ currentWorldName || currentWorldId }}」中，进入新世界前会先退出当前世界（清除当前进度，进度可先在「存档」中备份）
      </div>
      <div v-if="worldsLoading" class="empty">加载中…</div>
      <div v-else-if="!worldList.length" class="empty">暂无可进入的世界（请先在管理后台创建）</div>
      <el-radio-group v-else v-model="selectedWorldId" class="world-list">
        <el-radio v-for="w in worldList" :key="w.id" :value="w.id" class="world-item">
          <b>{{ w.id }}</b> <span class="muted">[{{ w.mode }}]</span>
          {{ w.background || '（无描述）' }}
          <span class="muted">轮次 {{ w.round_count }} · 角色 {{ w.character_count }}</span>
        </el-radio>
      </el-radio-group>
      <template #footer>
        <el-button size="small" @click="worldsVisible = false">取消</el-button>
        <el-button type="primary" size="small" :disabled="!selectedWorldId" :loading="worldBusy" @click="enterSelectedWorld">进入</el-button>
      </template>
    </el-dialog>

    <!-- 帮助 -->
    <el-dialog v-model="helpVisible" title="使用帮助" width="520px">
      <div class="help-body">
        <div class="help-item">
          <b>会话模式</b>
          <p>普通对话：与 AI 自由聊天，不加载世界上下文；世界内扮演：在当前世界中扮演行动，AI 会结合世界状态回应（需先进入世界）。</p>
        </div>
        <div class="help-item">
          <b>回复长度</b>
          <p>控制 AI 回复的篇幅：简短 / 标准 / 详细，随时可切换，立即生效。</p>
        </div>
        <div class="help-item">
          <b>进入世界</b>
          <p>从管理后台创建的世界中选择一个进入（世界会实例化复制到本会话，互不影响）。进入新世界会退出并清除当前世界的进度，重要进度请先在「存档」中备份；也可随时「离开世界」回到无世界状态。</p>
        </div>
        <div class="help-item">
          <b>玩家人设</b>
          <p>告诉 AI「你是谁」：名字 + 性格 / 外貌 / 行事风格。支持「全局默认」与「仅本世界」两层，世界人设优先于全局人设。</p>
        </div>
        <div class="help-item">
          <b>存档</b>
          <p>顶栏「存档」可新建 / 恢复游玩存档；恢复前会自动备份当前进度。</p>
        </div>
        <div class="help-item">
          <b>管理后台</b>
          <p>右上角「管理后台 →」可创建与编辑世界、维护素材库与设定库。</p>
        </div>
      </div>
      <template #footer>
        <el-button type="primary" size="small" @click="helpVisible = false">知道了</el-button>
      </template>
    </el-dialog>

    <!-- 剧情摘要编辑 -->
    <PlayerSummaryPanel v-model="summaryVisible" />

    <div class="main">
      <div class="chat-wrap">
        <div class="messages" ref="messagesEl">
          <div v-for="(m, i) in messages" :key="m.id || 'tmp-' + i" class="msg" :class="m.type">
            <div class="msg-main">
              <div v-if="editingId && editingId === m.id" class="bubble edit-bubble">
                <el-input v-model="editText" type="textarea" :autosize="{ minRows: 3, maxRows: 14 }" />
                <div class="edit-actions">
                  <el-button size="small" @click="cancelEdit">取消</el-button>
                  <el-button type="primary" size="small" :loading="editSaving" @click="saveEdit(m)">保存</el-button>
                </div>
              </div>
              <template v-else>
                <div class="bubble"><MarkdownText :text="m.text" /></div>
                <div v-if="(m.type === 'kp' || m.type === 'user') && m.id" class="msg-ops">
                  <button
                    v-if="m.type === 'kp' && m.id === lastKpId"
                    class="op-btn"
                    :disabled="thinking || !!editingId"
                    title="重新生成该回复"
                    @click="regen(m)"
                  >⟳ 重试</button>
                  <button
                    class="op-btn"
                    :disabled="thinking"
                    :title="m.type === 'kp' ? '编辑该回复' : '编辑该发言'"
                    @click="startEdit(m)"
                  >✎ 编辑</button>
                  <button
                    class="op-btn op-danger"
                    :disabled="thinking || !!editingId"
                    title="删除该消息"
                    @click="del(m)"
                  >✕ 删除</button>
                  <span v-if="m.type === 'kp' && m.version_count > 1" class="ver-switch">
                    <button
                      class="op-btn ver-btn"
                      :disabled="m.version_no <= 1 || m.versionSwitching"
                      @click="switchVersion(m, -1)"
                    >‹</button>
                    <span class="ver-label">{{ m.version_no }}/{{ m.version_count }}</span>
                    <button
                      class="op-btn ver-btn"
                      :disabled="m.version_no >= m.version_count || m.versionSwitching"
                      @click="switchVersion(m, 1)"
                    >›</button>
                  </span>
                </div>
              </template>
            </div>
          </div>
          <div class="msg thinking" :class="{ show: thinking }">
            <div class="bubble"><div class="dot"></div><div class="dot"></div><div class="dot"></div></div>
          </div>
        </div>

        <div class="input-bar">
          <div class="input-box">
            <textarea
              ref="inputEl"
              v-model="inputText"
              rows="1"
              placeholder="描述你的行动…"
              @keydown="onKeydown"
              @input="onInput"
            ></textarea>
          </div>
          <button class="btn btn-send" :disabled="thinking" @click="send">发送</button>
        </div>
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
          <div class="side-title">世界</div>
          <div v-if="currentWorldId" class="world-now">
            <div class="world-now-name">当前：{{ currentWorldName || currentWorldId }}</div>
            <button class="header-btn persona-btn" @click="openWorlds">切换世界…</button>
            <button class="header-btn persona-btn" style="margin-top:6px" @click="goWorldState">世界状态…</button>
            <button class="header-btn persona-btn" style="margin-top:6px" @click="summaryVisible = true">剧情摘要…</button>
            <button class="header-btn persona-btn" style="margin-top:6px" @click="leaveCurrentWorld">离开世界</button>
          </div>
          <button v-else class="header-btn persona-btn" @click="openWorlds">进入世界…</button>
          <div class="side-note" style="margin-top:6px">进入管理后台创建的世界（实例化复制到本会话）。</div>
        </div>
        <div class="side-block">
          <div class="side-title">玩家人设</div>
          <button class="header-btn persona-btn" @click="openPersona">设置人设…</button>
          <div class="side-note" style="margin-top:6px">告诉 AI「你是谁」，支持全局默认或仅本世界生效。</div>
        </div>
        <div class="side-block">
          <div class="side-title">帮助</div>
          <button class="header-btn persona-btn" @click="helpVisible = true">使用说明…</button>
        </div>
      </aside>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { playerSaveReq } from '../api/admin'
import {
  getChatState,
  updateChatState,
  getPersona,
  setPersona,
  enterWorld,
  leaveWorld,
  editChatMessage,
  switchChatMessageVersion,
  deleteChatMessage,
} from '../api/chat'
import MarkdownText from '../components/MarkdownText.vue'
import PlayerSummaryPanel from '../components/PlayerSummaryPanel.vue'

const router = useRouter()

const messagesEl = ref(null)
const inputEl = ref(null)

const messages = ref([])
const thinking = ref(false)
const connClass = ref('')
const connText = ref('连接中…')
const inputText = ref('')
const currentMode = ref('')
const currentLength = ref('standard')

const modes = [
  { key: 'normal', label: '普通对话' },
  { key: 'freechat', label: '世界内扮演' },
]
const lengths = [
  { key: 'short', label: '简短（≤150字）' },
  { key: 'standard', label: '标准（~300字）' },
  { key: 'detailed', label: '详细（~600字）' },
]

const helpVisible = ref(false)

let ws = null
let token = localStorage.getItem('trpg_token') || ''
let destroyed = false

// 聊天访问令牌（部署方开启 ChatToken 时通过 ?auth= 带入，与 WS 鉴权参数一致）
const chatAuth = new URLSearchParams(location.search).get('auth') || ''

function setConn(on, text) {
  connClass.value = on ? 'on' : 'off'
  connText.value = text
}

function scrollToBottom() {
  nextTick(() => {
    if (messagesEl.value) messagesEl.value.scrollTop = messagesEl.value.scrollHeight
  })
}

function addMsg(type, text, extra = {}) {
  messages.value.push({ type, text, id: 0, version_no: 0, version_count: 0, versionsCache: {}, ...extra })
  scrollToBottom()
}

function setThinking(on) {
  thinking.value = on
  if (on) scrollToBottom()
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
    .map((m) => ({
      type: typeMap[m.type],
      text: m.text,
      id: m.id || 0,
      version_no: m.version_no || 0,
      version_count: m.version_count || 0,
      // 历史返回的 text 即当前激活版本，先放入本地版本缓存
      versionsCache: m.version_no > 0 ? { [m.version_no]: m.text } : {},
    }))
  scrollToBottom()
}

function connect() {
  ensureToken().then(() => {
    if (destroyed) return
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    let wsUrl = `${proto}://${location.host}/ws/chat?token=${encodeURIComponent(token)}`
    if (chatAuth) wsUrl += '&auth=' + encodeURIComponent(chatAuth)
    ws = new WebSocket(wsUrl)

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
        case 'reply':
          if (msg.ref_id) {
            // 重试生成的新版本：就地更新目标消息，不追加新气泡
            applyRegenReply(msg)
          } else {
            addMsg('kp', msg.text, { id: msg.id || 0 })
          }
          break
        case 'push': addMsg('push', msg.text); break
        case 'ack':
          // 玩家消息落库回执：回填行 id（该气泡才能编辑/删除）
          applyAck(msg)
          break
        case 'session': addMsg('system', msg.text); break
        case 'progress': addMsg('system', msg.text); break
        case 'error':
          addMsg('system', '⚠ ' + msg.text)
          setThinking(false)
          break
        case 'status': setThinking(msg.state === 'thinking'); break
      }
    }
  })
}

// 重试回复帧：{ type:'reply', ref_id, text, version_no, version_count }
function applyRegenReply(msg) {
  const target = messages.value.find((x) => x.id === msg.ref_id)
  if (!target) {
    addMsg('kp', msg.text)
    return
  }
  target.text = msg.text
  if (msg.version_no) target.version_no = msg.version_no
  if (msg.version_count) target.version_count = msg.version_count
  if (target.version_no > 0) target.versionsCache[target.version_no] = msg.text
}

// 玩家消息落库回执帧：{ type:'ack', id, text }
// 从后往前找最近一条还没拿到 id 的本地 user 气泡回填（连续发多条时按序对应）
function applyAck(msg) {
  if (!msg.id) return
  for (let i = messages.value.length - 1; i >= 0; i--) {
    const m = messages.value[i]
    if (m.type === 'user' && !m.id) {
      m.id = msg.id
      return
    }
  }
}

function send() {
  const text = inputText.value.trim()
  if (!text || thinking.value || !ws || ws.readyState !== WebSocket.OPEN) return
  addMsg('user', text)
  ws.send(JSON.stringify({ type: 'chat', text }))
  inputText.value = ''
  if (inputEl.value) inputEl.value.style.height = 'auto'
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
}

// ---------- 消息操作：重试 / 编辑 / 删除 / 版本切换 ----------

// 仅最后一条 kp 消息允许重试（后端同样校验）
const lastKpId = computed(() => {
  for (let i = messages.value.length - 1; i >= 0; i--) {
    const m = messages.value[i]
    if (m.type === 'kp') return m.id || null
  }
  return null
})

function regen(m) {
  if (!m.id || thinking.value || editingId.value) return
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    ElMessage.error('连接已断开，无法重试')
    return
  }
  // 后端在校验失败时只回 error 帧；error 帧处理里会复位 thinking
  ws.send(JSON.stringify({ type: 'regen', ref_id: m.id }))
  setThinking(true)
}

const editingId = ref(null)
const editText = ref('')
const editSaving = ref(false)

function startEdit(m) {
  if (!m.id || thinking.value) return
  editingId.value = m.id
  editText.value = m.text
}

function cancelEdit() {
  editingId.value = null
  editText.value = ''
}

async function saveEdit(m) {
  const text = editText.value.trim()
  if (!text) {
    ElMessage.warning('内容不能为空')
    return
  }
  if (text === m.text) {
    cancelEdit()
    return
  }
  editSaving.value = true
  try {
    const t = await ensureToken()
    await editChatMessage(t, chatAuth, m.id, text)
    m.text = text
    if (m.version_no > 0) m.versionsCache[m.version_no] = text
    ElMessage.success('已保存')
    cancelEdit()
  } catch (e) {
    ElMessage.error('保存失败：' + e.message)
  } finally {
    editSaving.value = false
  }
}

// 删除消息（user / kp）：确认后调接口并从本地列表移除
async function del(m) {
  if (!m.id || thinking.value || editingId.value) return
  try {
    await ElMessageBox.confirm(
      m.type === 'kp' ? '删除这条 AI 回复？（其全部版本一并删除）' : '删除这条发言？',
      '删除消息',
      { confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning' },
    )
  } catch {
    return // 用户取消
  }
  try {
    const t = await ensureToken()
    await deleteChatMessage(t, chatAuth, m.id)
    const i = messages.value.indexOf(m)
    if (i >= 0) messages.value.splice(i, 1)
    ElMessage.success('已删除')
  } catch (e) {
    ElMessage.error('删除失败：' + e.message)
  }
}

// 切换版本：本地缓存有目标版本文本则先切换显示，再调接口持久化；
// 无缓存则等接口返回文本。失败时回滚到切换前的显示。
async function switchVersion(m, dir) {
  const target = m.version_no + dir
  if (m.versionSwitching || target < 1 || target > m.version_count) return
  m.versionSwitching = true
  const prevNo = m.version_no
  const prevText = m.text
  try {
    const t = await ensureToken()
    if (m.versionsCache[target] != null) {
      m.version_no = target
      m.text = m.versionsCache[target]
    }
    const r = await switchChatMessageVersion(t, chatAuth, m.id, target)
    m.text = r.text
    m.version_no = r.version_no
    if (r.version_count) m.version_count = r.version_count
    m.versionsCache[r.version_no] = r.text
  } catch (e) {
    m.version_no = prevNo
    m.text = prevText
    ElMessage.error('切换版本失败：' + e.message)
  } finally {
    m.versionSwitching = false
  }
}

// ---------- 会话状态（模式 / 回复长度 / 所在世界） ----------

const currentWorldId = ref('')
const currentWorldName = ref('')

function applyState(s) {
  if (!s) return
  currentMode.value = s.mode || 'normal'
  currentLength.value = s.reply_length || 'standard'
  currentWorldId.value = s.world_id || ''
  currentWorldName.value = s.world_name || ''
}

async function loadChatState() {
  const t = await ensureToken()
  applyState(await getChatState(t, chatAuth))
}

async function switchMode(mode) {
  if (mode === currentMode.value) return
  try {
    const t = await ensureToken()
    applyState(await updateChatState(t, chatAuth, { mode }))
    ElMessage.success('已切换到「' + (modes.find((m) => m.key === mode)?.label || mode) + '」')
  } catch (e) {
    ElMessage.error('切换模式失败：' + e.message)
    loadChatState().catch(() => {})
  }
}

async function switchLength(key) {
  if (key === currentLength.value) return
  try {
    const t = await ensureToken()
    applyState(await updateChatState(t, chatAuth, { reply_length: key }))
  } catch (e) {
    ElMessage.error('设置回复长度失败：' + e.message)
    loadChatState().catch(() => {})
  }
}

// ---------- 玩家人设 ----------

const personaVisible = ref(false)
const personaName = ref('')
const personaDesc = ref('')
const personaScope = ref('global')
const personaSaving = ref(false)

function personaWorld() {
  return personaScope.value === 'world' ? currentWorldId.value : ''
}

async function loadPersona() {
  try {
    const t = await ensureToken()
    const p = await getPersona(t, chatAuth, personaWorld())
    personaName.value = p?.name || ''
    personaDesc.value = p?.description || ''
  } catch (e) {
    ElMessage.error('加载人设失败：' + e.message)
  }
}

function openPersona() {
  personaVisible.value = true
  if (personaScope.value === 'world' && !currentWorldId.value) personaScope.value = 'global'
  loadPersona()
}

async function applyPersona() {
  const name = personaName.value.trim()
  const desc = personaDesc.value.trim()
  if (!name || !desc) { ElMessage.warning('名字和描述都不能为空（要移除人设请点「清除人设」）'); return }
  personaSaving.value = true
  try {
    const t = await ensureToken()
    await setPersona(t, chatAuth, { world: personaWorld(), name, description: desc })
    ElMessage.success('人设已保存')
    personaVisible.value = false
  } catch (e) {
    ElMessage.error('保存人设失败：' + e.message)
  } finally {
    personaSaving.value = false
  }
}

async function clearPersona() {
  personaSaving.value = true
  try {
    const t = await ensureToken()
    await setPersona(t, chatAuth, { world: personaWorld(), name: '', description: '' })
    personaName.value = ''
    personaDesc.value = ''
    ElMessage.success('人设已清除')
    personaVisible.value = false
  } catch (e) {
    ElMessage.error('清除人设失败：' + e.message)
  } finally {
    personaSaving.value = false
  }
}

// ---------- 进入 / 离开世界 ----------

const worldsVisible = ref(false)
const worldsLoading = ref(false)
const worldBusy = ref(false)
const worldList = ref([])
const selectedWorldId = ref('')

async function openWorlds() {
  worldsVisible.value = true
  worldsLoading.value = true
  selectedWorldId.value = ''
  try {
    const t = await ensureToken()
    const data = await playerSaveReq('/api/worlds', { token: t, auth: chatAuth })
    worldList.value = data?.worlds || []
  } catch (e) {
    ElMessage.error('加载世界列表失败：' + e.message)
    worldList.value = []
  } finally {
    worldsLoading.value = false
  }
}

// 确认进入：已有进行中世界时二次确认，先离开再进入（会清除当前进度）
async function enterSelectedWorld() {
  const id = selectedWorldId.value
  if (!id) return
  if (currentWorldId.value) {
    try {
      await ElMessageBox.confirm(
        '进入新世界会先退出当前世界并清除当前进度（可在「存档」中提前备份）。继续？',
        '进入世界',
        { type: 'warning', confirmButtonText: '退出并进入', cancelButtonText: '取消' }
      )
    } catch {
      return
    }
  }
  worldBusy.value = true
  try {
    const t = await ensureToken()
    if (currentWorldId.value) {
      await leaveWorld(t, chatAuth)
    }
    const r = await enterWorld(t, chatAuth, id)
    await loadChatState()
    worldsVisible.value = false
    addMsg('system', `🌍 已进入世界「${r?.world_name || id}」`)
  } catch (e) {
    ElMessage.error('进入世界失败：' + e.message)
    loadChatState().catch(() => {})
  } finally {
    worldBusy.value = false
  }
}

async function leaveCurrentWorld() {
  try {
    await ElMessageBox.confirm(
      '离开世界会清除本会话在该世界的进度（可在「存档」中提前备份）。继续？',
      '离开世界',
      { type: 'warning', confirmButtonText: '离开世界', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    const t = await ensureToken()
    await leaveWorld(t, chatAuth)
    await loadChatState()
    addMsg('system', '已离开世界')
  } catch (e) {
    ElMessage.error('离开世界失败：' + e.message)
  }
}

// ---------- 世界状态 / 剧情摘要编辑入口 ----------

const summaryVisible = ref(false)

function goWorldState() {
  // 带上 ?auth=，保证开启 ChatToken 的部署往返后仍可取到聊天访问令牌
  router.push({ path: '/world', query: chatAuth ? { auth: chatAuth } : {} })
}

// 先恢复历史与会话状态再连 WS：历史请求在 connect 之前 await，天然无竞态
onMounted(async () => {
  try {
    await loadHistory()
  } catch (err) {
    console.warn('加载历史消息失败:', err)
  }
  try {
    await loadChatState()
  } catch (err) {
    console.warn('加载会话状态失败:', err)
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
    loadChatState().catch(() => {})
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
.persona-btn { width: 100%; border: 1px solid var(--border); text-align: center; }
.world-now-name { font-size: 13px; font-weight: 600; margin-bottom: 8px; }
.world-list { display: flex; flex-direction: column; gap: 8px; width: 100%; }
.world-item { width: 100%; margin-right: 0 !important; padding: 8px 10px; border: 1px solid var(--border); border-radius: 8px; }
.world-item :deep(.el-radio__label) { white-space: normal; line-height: 1.6; }
.save-create { display: flex; gap: 8px; margin-bottom: 12px; }
.empty { padding: 24px 0; text-align: center; color: var(--text-secondary); font-size: 13px; }
.help-body { display: flex; flex-direction: column; gap: 12px; }
.help-item p { margin: 4px 0 0; font-size: 13px; color: var(--text-secondary); line-height: 1.7; }

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

/* ===== 气泡操作区（重试 / 编辑 / 删除 / 版本切换） ===== */
.msg-main { display: flex; flex-direction: column; min-width: 0; }
.msg.user .msg-main { align-items: flex-end; }
.msg-ops { display: flex; align-items: center; gap: 6px; margin-top: 4px; padding: 0 4px; }
.op-btn {
  font: inherit; font-size: 12px; line-height: 1.4;
  color: var(--text-secondary); background: transparent;
  border: none; border-radius: 6px; padding: 2px 6px; cursor: pointer;
  opacity: 0; transition: opacity .15s, background .15s, color .15s;
}
.msg:hover .op-btn { opacity: 1; }
.op-btn:hover:not(:disabled) { background: var(--bg); color: var(--text); }
.op-btn.op-danger:hover:not(:disabled) { background: #fdecec; color: #d43030; }
.op-btn:disabled { cursor: not-allowed; color: #c3c9d4; }
/* 版本切换器常显（让用户可发现多版本），其余按钮 hover 显示 */
.ver-switch { display: flex; align-items: center; gap: 2px; }
.ver-switch .op-btn { opacity: 1; padding: 2px 4px; font-size: 14px; }
.ver-label { font-size: 12px; color: var(--text-secondary); min-width: 30px; text-align: center; }

/* 编辑态气泡：编辑时把消息行撑到正常气泡的最大宽度（.msg 的 max-width 生效），
   祖先有确定宽度后 autosize 才能正确计算行高 */
.msg:has(.edit-bubble) { width: 100%; }
.msg:has(.edit-bubble) .msg-main { width: 100%; }
.msg .bubble.edit-bubble { width: 100%; }
.edit-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px; }

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
/* 输入区原生 textarea（scoped 裸 textarea 选择器打不到 el-input 内部，
   限定到 .input-box 避免误导；编辑框限高由 el-input 的 maxRows 负责） */
.input-box textarea {
  width: 100%; border: none; outline: none; resize: none;
  background: transparent; padding: 12px 14px;
  font: inherit; font-size: 14.5px; line-height: 1.6;
  max-height: 140px; display: block;
}
.btn {
  flex: none; height: 42px; padding: 0 18px;
  border: none; border-radius: 12px; cursor: pointer;
  font: inherit; font-size: 14px; font-weight: 600;
  transition: all .2s;
}
.btn-send { background: var(--primary); color: #fff; }
.btn-send:hover { background: #2f5cd8; }
.btn-send:disabled { background: #c3cfe8; cursor: not-allowed; }

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
.side-note { font-size: 12px; color: var(--text-secondary); line-height: 1.7; }

@media (max-width: 860px) {
  .sidebar { display: none; }
  .msg { max-width: 92%; }
}
</style>
