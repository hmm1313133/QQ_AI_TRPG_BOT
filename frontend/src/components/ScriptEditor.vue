<template>
  <el-dialog
    :model-value="visible"
    :title="isEdit ? '编辑剧本' : '手动创建剧本'"
    fullscreen
    :close-on-click-modal="false"
    @update:model-value="$emit('update:visible', $event)"
    @open="onOpen"
  >
    <el-tabs v-model="activeTab" :before-leave="beforeTabLeave">
      <!-- 基本信息 -->
      <el-tab-pane label="基本信息" name="basic">
        <el-form label-width="110px" class="pane">
          <el-form-item label="名称 name" required>
            <el-input v-model="doc.name" :disabled="isEdit" placeholder="简短名称（用于生成 ID，编辑时不可改）" style="max-width:360px" />
          </el-form-item>
          <el-form-item label="标题 title" required>
            <el-input v-model="doc.title" style="max-width:480px" />
          </el-form-item>
          <el-form-item label="规则集 system" required>
            <el-select v-model="doc.system" style="width:180px">
              <el-option label="coc7" value="coc7" />
              <el-option label="dnd5e" value="dnd5e" />
            </el-select>
          </el-form-item>
          <el-divider content-position="left">故事背景 background</el-divider>
          <el-form-item label="设定 setting">
            <el-input v-model="doc.background.setting" type="textarea" :rows="2" placeholder="时代/地点/世界观概述" />
          </el-form-item>
          <el-form-item label="时代 era">
            <el-input v-model="doc.background.era" style="max-width:360px" placeholder="如 1920 年代" />
          </el-form-item>
          <el-form-item label="地点 location">
            <el-input v-model="doc.background.location" style="max-width:360px" />
          </el-form-item>
          <el-form-item label="氛围 atmosphere">
            <el-input v-model="doc.background.atmosphere" style="max-width:480px" />
          </el-form-item>
          <el-form-item label="梗概 synopsis">
            <el-input v-model="doc.background.synopsis" type="textarea" :rows="4" />
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <!-- 时间轴 -->
      <el-tab-pane :label="`时间轴（${doc.timeline.length}）`" name="timeline">
        <div class="pane">
          <div v-for="(node, i) in doc.timeline" :key="i" class="node-card">
            <div class="node-head">
              <span class="node-order">#{{ i + 1 }}</span>
              <el-input v-model="node.id" placeholder="节点 id" size="small" style="width:160px" />
              <el-input v-model="node.name" placeholder="节点名称" size="small" style="width:220px" />
              <el-select v-model="node.type" size="small" style="width:110px">
                <el-option label="act 幕" value="act" />
                <el-option label="scene 场景" value="scene" />
                <el-option label="event 事件" value="event" />
              </el-select>
              <el-checkbox v-model="node.is_key_node" size="small">关键节点</el-checkbox>
              <span class="spacer"></span>
              <el-button size="small" circle :disabled="i === 0" @click="moveNode(i, -1)">↑</el-button>
              <el-button size="small" circle :disabled="i === doc.timeline.length - 1" @click="moveNode(i, 1)">↓</el-button>
              <el-button size="small" type="danger" plain circle @click="doc.timeline.splice(i, 1)">×</el-button>
            </div>
            <el-input v-model="node.description" type="textarea" :rows="2" placeholder="节点描述（场景、事件、可发生的事情）" />
            <div class="node-grid">
              <el-input v-model="node._triggersText" size="small" placeholder="触发条件 triggers（逗号分隔）" />
              <el-input v-model="node._npcsText" size="small" placeholder="涉及 NPC（逗号分隔）" />
            </div>
            <el-input v-model="node.kp_notes" size="small" placeholder="KP 备注 kp_notes（可选）" style="margin-top:8px" />
          </div>
          <el-button plain @click="addNode">+ 添加节点</el-button>
        </div>
      </el-tab-pane>

      <!-- 角色 -->
      <el-tab-pane :label="`角色（${doc.characters.length}）`" name="characters">
        <div class="pane">
          <div v-for="(ch, i) in doc.characters" :key="i" class="node-card">
            <div class="node-head">
              <el-input v-model="ch.name" placeholder="角色名" size="small" style="width:200px" />
              <el-select v-model="ch.role" size="small" style="width:150px">
                <el-option label="protagonist 主角" value="protagonist" />
                <el-option label="antagonist 反派" value="antagonist" />
                <el-option label="npc" value="npc" />
              </el-select>
              <span class="spacer"></span>
              <el-button size="small" type="danger" plain circle @click="doc.characters.splice(i, 1)">×</el-button>
            </div>
            <div class="node-grid">
              <el-input v-model="ch.personality" size="small" placeholder="性格 personality" />
              <el-input v-model="ch.motivation" size="small" placeholder="动机 motivation" />
            </div>
            <el-input v-model="ch.background" type="textarea" :rows="2" placeholder="背景 background" style="margin-top:8px" />
            <el-input v-model="ch.dialogue_style" size="small" placeholder="对话风格 dialogue_style" style="margin-top:8px" />
          </div>
          <el-button plain @click="addCharacter">+ 添加角色</el-button>
        </div>
      </el-tab-pane>

      <!-- JSON -->
      <el-tab-pane label="JSON" name="json">
        <div class="pane">
          <div class="muted" style="margin-bottom:8px">
            全量 JSON（高级编辑，覆盖结构化表单未包含的字段）。切出本页签时校验并回写表单。
          </div>
          <el-input v-model="jsonText" type="textarea" :rows="22" class="json-area" @blur="validateJson" />
          <div v-if="jsonError" class="json-error">{{ jsonError }}</div>
        </div>
      </el-tab-pane>
    </el-tabs>

    <template #footer>
      <el-button @click="$emit('update:visible', false)">取消</el-button>
      <el-button type="primary" :loading="saving" @click="submit">{{ isEdit ? '保存' : '创建' }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { adminReq } from '../api/admin'

// 剧本编辑器：基本信息 / 时间轴 / 角色 / JSON 四页签，JSON 为兜底全量编辑
const props = defineProps({
  visible: { type: Boolean, default: false },
  scriptId: { type: String, default: '' }, // 空 = 新建
})
const emit = defineEmits(['update:visible', 'saved'])

const activeTab = ref('basic')
const doc = ref(emptyDoc())
const jsonText = ref('')
const jsonError = ref('')
const saving = ref(false)

const isEdit = computed(() => !!props.scriptId)

function emptyDoc() {
  return {
    name: '', title: '', system: 'coc7',
    background: { setting: '', era: '', location: '', atmosphere: '', synopsis: '' },
    timeline: [newNode(1)],
    characters: [],
    scenes: [],
  }
}

function newNode(order) {
  return {
    id: `node_${order}`, name: '', type: 'scene', order,
    description: '', triggers: [], npcs: [], is_key_node: false, kp_notes: '',
    _triggersText: '', _npcsText: '',
  }
}

function newCharacter() {
  return { name: '', role: 'npc', personality: '', background: '', motivation: '', dialogue_style: '' }
}

// 载入剧本到表单模型（补辅助文本字段；未覆盖字段原样保留，随提交回传）
function loadDoc(scr) {
  const d = JSON.parse(JSON.stringify(scr || {}))
  d.background = d.background || { setting: '', era: '', location: '', atmosphere: '', synopsis: '' }
  d.timeline = (d.timeline || []).map((n, i) => ({
    ...n,
    order: i + 1,
    _triggersText: (n.triggers || []).join('，'),
    _npcsText: (n.npcs || []).join('，'),
  }))
  if (d.timeline.length === 0) d.timeline = [newNode(1)]
  d.characters = d.characters || []
  d.scenes = d.scenes || []
  doc.value = d
}

function splitList(text) {
  return String(text || '')
    .split(/[,，]/)
    .map((s) => s.trim())
    .filter(Boolean)
}

// 表单模型 → 提交用 Script 对象（字段名严格对应 internal/script/types.go 的 JSON tag）
function buildScript() {
  const d = JSON.parse(JSON.stringify(doc.value))
  d.timeline = (d.timeline || []).map((n, i) => {
    delete n._triggersText
    delete n._npcsText
    return {
      ...n,
      id: (n.id || '').trim() || `node_${i + 1}`,
      order: i + 1, // 顺序自动重排
      triggers: splitList(doc.value.timeline[i]._triggersText),
      npcs: splitList(doc.value.timeline[i]._npcsText),
    }
  })
  return d
}

function addNode() {
  doc.value.timeline.push(newNode(doc.value.timeline.length + 1))
}

function moveNode(i, dir) {
  const j = i + dir
  if (j < 0 || j >= doc.value.timeline.length) return
  const arr = doc.value.timeline
  ;[arr[i], arr[j]] = [arr[j], arr[i]]
}

function addCharacter() {
  doc.value.characters.push(newCharacter())
}

function syncJsonFromForm() {
  jsonText.value = JSON.stringify(buildScript(), null, 2)
  jsonError.value = ''
}

function validateJson() {
  if (activeTab.value !== 'json') return true
  try {
    JSON.parse(jsonText.value)
    jsonError.value = ''
    return true
  } catch (e) {
    jsonError.value = `JSON 不合法：${e.message}`
    return false
  }
}

// 切页签：进 JSON 时同步最新对象；出 JSON 时校验并回写表单（不合法则阻止切换）
function beforeTabLeave(activeName, oldActiveName) {
  if (activeName === 'json') {
    syncJsonFromForm()
    return true
  }
  if (oldActiveName === 'json') {
    if (!validateJson()) {
      ElMessage.error('JSON 不合法，请先修正')
      return false
    }
    loadDoc(JSON.parse(jsonText.value))
  }
  return true
}

async function onOpen() {
  activeTab.value = 'basic'
  jsonError.value = ''
  if (isEdit.value) {
    try {
      const scr = await adminReq(`/api/admin/scripts/${encodeURIComponent(props.scriptId)}`)
      loadDoc(scr)
    } catch (e) {
      ElMessage.error(e.message)
      emit('update:visible', false)
    }
  } else {
    loadDoc(emptyDoc())
  }
}

function checkBasic(scr) {
  if (!scr.name || !scr.name.trim()) return '名称 name 不能为空'
  if (!scr.title || !scr.title.trim()) return '标题 title 不能为空'
  if (!scr.system) return '规则集 system 不能为空'
  if (!Array.isArray(scr.timeline) || scr.timeline.length === 0) return '时间轴至少需要 1 个节点'
  return ''
}

async function submit() {
  let scr
  if (activeTab.value === 'json') {
    if (!validateJson()) { ElMessage.error('JSON 不合法，请先修正'); return }
    scr = JSON.parse(jsonText.value)
  } else {
    scr = buildScript()
  }
  const err = checkBasic(scr)
  if (err) { ElMessage.warning(err); return }

  saving.value = true
  try {
    if (isEdit.value) {
      await adminReq(`/api/admin/scripts/${encodeURIComponent(props.scriptId)}`, {
        method: 'PUT',
        body: JSON.stringify(scr),
      })
      ElMessage.success('已保存')
    } else {
      await adminReq('/api/admin/scripts', { method: 'POST', body: JSON.stringify(scr) })
      ElMessage.success('已创建')
    }
    emit('update:visible', false)
    emit('saved')
  } catch (e) {
    ElMessage.error(e.status === 409 ? '同名剧本已存在' : e.message)
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.pane { max-width: 860px; }
.node-card {
  border: 1px solid var(--border); border-radius: 10px;
  padding: 12px; margin-bottom: 12px; background: var(--bg);
}
.node-head { display: flex; gap: 8px; align-items: center; margin-bottom: 8px; flex-wrap: wrap; }
.node-order { font-size: 12.5px; color: var(--text-secondary); width: 34px; flex: none; }
.node-head .spacer { flex: 1; }
.node-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-top: 8px; }
.json-area :deep(textarea) { font-family: ui-monospace, "Cascadia Code", Consolas, monospace; font-size: 12.5px; }
.json-error { color: #d4380d; font-size: 12.5px; margin-top: 6px; }
</style>
