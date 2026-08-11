<template>
  <el-dialog
    :model-value="modelValue"
    title="剧情摘要"
    width="640px"
    :close-on-click-modal="false"
    :before-close="requestClose"
    @update:model-value="(v) => emit('update:modelValue', v)"
    @open="init"
  >
    <div v-if="loading" class="empty">加载中…</div>
    <div v-else-if="noWorld" class="empty">
      当前会话还没有进行中的世界（先在侧栏进入一个世界）
    </div>
    <template v-else>
      <div class="sum-hint">
        摘要由 AI 随剧情滚动更新，与近期对话窗口一起构成 AI 的上下文；可以手动修正。
        <span v-if="pendingTurns > 0" class="muted">（还有 {{ pendingTurns }} 轮对话待总结）</span>
        <span v-if="dirty" class="dirty-hint">● 有未保存更改</span>
      </div>
      <el-input
        v-model="summary"
        type="textarea"
        :autosize="{ minRows: 8, maxRows: 24 }"
        placeholder="暂无剧情摘要——进行几轮对话后会自动生成"
      />
    </template>

    <template #footer>
      <el-button size="small" @click="close">关闭</el-button>
      <el-button
        type="primary"
        size="small"
        :loading="saving"
        :disabled="loading || noWorld"
        @click="save"
      >保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { nextTick, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getChatSummary, saveChatSummary } from '../api/chat'
import { ensureChatToken, getChatAuth } from '../api/session'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue'])

const summary = ref('')
const pendingTurns = ref(0)
const loading = ref(false)
const noWorld = ref(false)
const saving = ref(false)
const dirty = ref(false)

// 变更标记未保存；加载完成后由 init 复位
watch(summary, () => { dirty.value = true })

async function init() {
  loading.value = true
  noWorld.value = false
  summary.value = ''
  pendingTurns.value = 0
  dirty.value = false
  try {
    const t = await ensureChatToken()
    const r = await getChatSummary(t, getChatAuth())
    summary.value = r?.summary || ''
    pendingTurns.value = r?.pending_turns || 0
    // 等 watch 跑完再复位 dirty，避免加载本身被当成用户修改
    await nextTick()
    dirty.value = false
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

async function confirmDiscard(action) {
  if (!dirty.value) return true
  try {
    await ElMessageBox.confirm(`有未保存的更改，${action}会丢弃这些更改。继续？`, '未保存更改', {
      type: 'warning',
      confirmButtonText: '放弃更改',
      cancelButtonText: '取消',
    })
    return true
  } catch {
    return false
  }
}

async function save() {
  saving.value = true
  try {
    const t = await ensureChatToken()
    await saveChatSummary(t, getChatAuth(), summary.value.trim())
    ElMessage.success('剧情摘要已保存')
    dirty.value = false
  } catch (e) {
    ElMessage.error('保存失败：' + e.message)
  } finally {
    saving.value = false
  }
}

// el-dialog 的 before-close（X / ESC）
async function requestClose(done) {
  if (await confirmDiscard('关闭')) done()
}

// 底部「关闭」按钮
async function close() {
  if (await confirmDiscard('关闭')) emit('update:modelValue', false)
}
</script>

<style scoped>
.sum-hint { font-size: 12px; color: var(--text-secondary); margin-bottom: 10px; line-height: 1.6; }
.dirty-hint { color: var(--el-color-warning, #e6a23c); margin-left: 6px; }
.empty { padding: 24px 0; text-align: center; color: var(--text-secondary); font-size: 13px; }
</style>
