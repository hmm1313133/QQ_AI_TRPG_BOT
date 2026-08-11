<template>
  <el-dialog
    :model-value="modelValue"
    title="剧情摘要"
    width="640px"
    @update:model-value="(v) => emit('update:modelValue', v)"
    @open="init"
  >
    <div v-if="loading" class="empty">加载中…</div>
    <div v-else-if="noWorld" class="empty">
      当前会话还没有进行中的世界（先在侧栏进入一个世界）
    </div>
    <template v-else>
      <div class="sum-hint">
        摘要由 AI 按阶段生成：对话滑出近期窗口后，每个阶段总结为一条；多条摘要按时间顺序注入上下文，替代远期对话。
        可以修改、删除已有条目，或手动补录。
        <span v-if="pendingTurns > 0" class="muted">（还有 {{ pendingTurns }} 轮对话待总结）</span>
      </div>

      <div v-if="entries.length === 0" class="empty">
        暂无剧情摘要——多进行几轮对话后会自动生成，也可以在下方手动补录
      </div>
      <div v-for="e in entries" :key="e.id" class="entry">
        <div class="entry-head">
          <span class="entry-range">{{ rangeLabel(e) }}</span>
          <span class="entry-ops">
            <template v-if="editingId === e.id">
              <el-button size="small" text @click="cancelEdit">取消</el-button>
              <el-button type="primary" size="small" :loading="saving" @click="saveEdit(e)">保存</el-button>
            </template>
            <template v-else>
              <el-button size="small" text :disabled="editingId !== null" @click="startEdit(e)">编辑</el-button>
              <el-button size="small" text type="danger" :disabled="editingId !== null" @click="del(e)">删除</el-button>
            </template>
          </span>
        </div>
        <el-input
          v-if="editingId === e.id"
          v-model="editText"
          type="textarea"
          :autosize="{ minRows: 3, maxRows: 16 }"
        />
        <div v-else class="entry-text">{{ e.text }}</div>
      </div>

      <!-- 手动补录 -->
      <div class="add-box">
        <el-input
          v-model="newText"
          type="textarea"
          :autosize="{ minRows: 2, maxRows: 8 }"
          placeholder="手动补录一条摘要（追加到列表末尾）…"
        />
        <el-button
          size="small"
          style="margin-top: 6px"
          :loading="saving"
          :disabled="!newText.trim()"
          @click="add"
        >+ 补录一条</el-button>
      </div>
    </template>

    <template #footer>
      <el-button size="small" @click="emit('update:modelValue', false)">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  getChatSummary,
  addChatSummaryEntry,
  editChatSummaryEntry,
  deleteChatSummaryEntry,
} from '../api/chat'
import { ensureChatToken, getChatAuth } from '../api/session'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue'])

const entries = ref([])
const pendingTurns = ref(0)
const loading = ref(false)
const noWorld = ref(false)
const saving = ref(false)

const editingId = ref(null)
const editText = ref('')
const newText = ref('')

function rangeLabel(e) {
  if (e.round_to > 0) return `第 ${e.round_from}–${e.round_to} 轮`
  return '手动补录'
}

async function init() {
  loading.value = true
  noWorld.value = false
  entries.value = []
  pendingTurns.value = 0
  editingId.value = null
  editText.value = ''
  newText.value = ''
  try {
    const t = await ensureChatToken()
    const r = await getChatSummary(t, getChatAuth())
    entries.value = r?.entries || []
    pendingTurns.value = r?.pending_turns || 0
  } catch (e) {
    // 404 = 会话未进入世界，展示引导文案
    if (e.status === 404) {
      noWorld.value = true
    } else {
      ElMessage.error('加载剧情摘要失败：' + e.message)
    }
  } finally {
    loading.value = false
  }
}

function startEdit(e) {
  editingId.value = e.id
  editText.value = e.text
}

function cancelEdit() {
  editingId.value = null
  editText.value = ''
}

async function saveEdit(e) {
  const text = editText.value.trim()
  if (!text) {
    ElMessage.warning('内容不能为空')
    return
  }
  saving.value = true
  try {
    const t = await ensureChatToken()
    await editChatSummaryEntry(t, getChatAuth(), e.id, text)
    e.text = text
    ElMessage.success('已保存')
    cancelEdit()
  } catch (err) {
    ElMessage.error('保存失败：' + err.message)
  } finally {
    saving.value = false
  }
}

async function del(e) {
  try {
    await ElMessageBox.confirm('删除这条摘要？AI 将不再记得这段剧情。', '删除摘要', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return // 用户取消
  }
  saving.value = true
  try {
    const t = await ensureChatToken()
    await deleteChatSummaryEntry(t, getChatAuth(), e.id)
    entries.value = entries.value.filter((x) => x.id !== e.id)
    ElMessage.success('已删除')
  } catch (err) {
    ElMessage.error('删除失败：' + err.message)
  } finally {
    saving.value = false
  }
}

async function add() {
  const text = newText.value.trim()
  if (!text) return
  saving.value = true
  try {
    const t = await ensureChatToken()
    const r = await addChatSummaryEntry(t, getChatAuth(), text)
    entries.value.push({ id: r.id, round_from: 0, round_to: 0, text })
    newText.value = ''
    ElMessage.success('已补录')
  } catch (err) {
    ElMessage.error('补录失败：' + err.message)
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.sum-hint { font-size: 12px; color: var(--text-secondary); margin-bottom: 10px; line-height: 1.6; }
.empty { padding: 24px 0; text-align: center; color: var(--text-secondary); font-size: 13px; }
.entry { border: 1px solid var(--border); border-radius: 8px; padding: 10px 12px; margin-bottom: 10px; }
.entry-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 6px; }
.entry-range { font-size: 12px; color: var(--text-secondary); }
.entry-text { font-size: 13.5px; line-height: 1.7; white-space: pre-wrap; word-break: break-word; }
.add-box { margin-top: 14px; border-top: 1px dashed var(--border); padding-top: 12px; }
</style>
