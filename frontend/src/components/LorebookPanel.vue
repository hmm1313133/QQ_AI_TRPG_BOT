<template>
  <div class="lorebook">
    <!-- 命中测试（设计文档 §3.2：SillyTavern 用户调试 lorebook 的核心工具） -->
    <el-collapse class="test-panel">
      <el-collapse-item name="test">
        <template #title><span class="test-title">命中测试</span></template>
        <div class="test-body">
          <el-input
            v-model="testText"
            type="textarea"
            :rows="3"
            placeholder="输入一段测试文本（如玩家发言），查看哪些条目会被触发"
          />
          <div style="margin-top:8px">
            <el-button type="primary" size="small" :loading="testing" :disabled="!testText.trim()" @click="runTest">测试</el-button>
            <span v-if="testResult" class="muted" style="margin-left:10px">
              预算 {{ testResult.budget }} 字符<span v-if="testResult.recursion"> · 递归扫描已开启</span>
            </span>
          </div>
          <template v-if="testResult">
            <div class="budget-row">
              <span class="muted">预算占用 {{ usedChars }} / {{ testResult.budget }}</span>
              <el-progress
                :percentage="budgetPct"
                :status="budgetPct >= 100 ? 'exception' : budgetPct > 80 ? 'warning' : ''"
                :stroke-width="10"
                style="flex:1"
              />
            </div>
            <div v-if="testResult.front?.length" class="hit-group">
              <div class="hit-group-title">前部注入（世界观区）</div>
              <div v-for="(h, i) in testResult.front" :key="'f' + i" class="hit-row">
                <span class="hit-title">{{ h.entry.title }}</span>
                <span class="muted">{{ h.reason }}</span>
                <span class="mono muted">{{ h.chars }} 字符</span>
              </div>
            </div>
            <div v-if="testResult.tail?.length" class="hit-group">
              <div class="hit-group-title">尾部注入（风格指令区）</div>
              <div v-for="(h, i) in testResult.tail" :key="'t' + i" class="hit-row">
                <span class="hit-title">{{ h.entry.title }}</span>
                <span class="muted">{{ h.reason }}</span>
                <span class="mono muted">{{ h.chars }} 字符</span>
              </div>
            </div>
            <div v-if="testResult.dropped?.length" class="hit-group">
              <div class="hit-group-title">被预算裁剪</div>
              <div v-for="(h, i) in testResult.dropped" :key="'d' + i" class="hit-row dropped">
                <span class="hit-title">{{ h.entry.title }}</span>
                <span class="muted">{{ h.reason }}</span>
                <span class="mono muted">{{ h.chars }} 字符</span>
              </div>
            </div>
            <div v-if="!usedChars && !testResult.dropped?.length" class="empty">没有条目被触发</div>
          </template>
        </div>
      </el-collapse-item>
    </el-collapse>

    <div class="lore-body">
      <!-- 左：分类分组列表 -->
      <div class="lore-left">
        <el-input v-model="search" size="small" clearable placeholder="搜索标题 / 关键词 / 内容" />
        <div class="left-toolbar">
          <el-checkbox v-model="enabledOnly" size="small">仅看启用</el-checkbox>
          <span class="spacer"></span>
          <el-button type="primary" size="small" @click="startCreate">+ 新建条目</el-button>
        </div>
        <div v-if="loading" class="empty">加载中…</div>
        <template v-else>
          <el-collapse v-model="openGroups">
            <el-collapse-item
              v-for="g in groupedEntries"
              :key="g.value"
              :name="g.value"
            >
              <template #title>
                <span>{{ g.label }} <span class="muted">({{ g.items.length }})</span></span>
              </template>
              <div
                v-for="e in g.items"
                :key="e.id"
                class="entry-row"
                :class="{ active: draft && draft.id === e.id }"
                @click="startEdit(e)"
              >
                <span class="entry-title">{{ e.title }}</span>
                <span class="entry-badges">
                  <el-tag v-if="e.constant" size="small" type="warning" effect="plain">恒定</el-tag>
                  <el-tag v-if="e.position === 'tail'" size="small" type="info" effect="plain">尾部</el-tag>
                  <el-tag v-if="!e.enabled" size="small" type="danger" effect="plain">停用</el-tag>
                </span>
              </div>
              <div v-if="!g.items.length" class="empty" style="padding:10px">无匹配条目</div>
            </el-collapse-item>
          </el-collapse>
          <div v-if="!entries.length" class="empty">暂无设定条目，点击"新建条目"创建</div>
        </template>
      </div>

      <!-- 右：条目编辑器 -->
      <div class="lore-right">
        <LoreEntryForm
          v-if="draft"
          :entry="draft"
          :saving="saving"
          :is-new="isNew"
          :constant-total="constantTotal"
          @save="saveEntry"
          @remove="removeEntry"
          @cancel="draft = null"
        />
        <div v-else class="empty" style="padding-top:60px">从左侧选择条目进行编辑，或新建条目</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { loreApi } from '../api/admin'
import LoreEntryForm from './LoreEntryForm.vue'
import { LORE_CATEGORIES, emptyLoreEntry } from './loreMeta'

const props = defineProps({
  worldId: { type: String, required: true },
})

const entries = ref([])
const loading = ref(false)
const search = ref('')
const enabledOnly = ref(false)
const openGroups = ref(LORE_CATEGORIES.map((c) => c.value))

const draft = ref(null)
const isNew = ref(false)
const saving = ref(false)

const testText = ref('')
const testing = ref(false)
const testResult = ref(null)

const constantTotal = computed(() => entries.value.filter((e) => e.constant).length)

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  return entries.value.filter((e) => {
    if (enabledOnly.value && !e.enabled) return false
    if (!q) return true
    const hay = [e.title, e.content, ...(e.keys || []), ...(e.secondary_keys || [])]
      .join('\n').toLowerCase()
    return hay.includes(q)
  })
})

const groupedEntries = computed(() =>
  LORE_CATEGORIES
    .map((c) => ({ ...c, items: filtered.value.filter((e) => (e.category || 'background') === c.value) }))
    .filter((g) => g.items.length || !search.value.trim())
)

const usedChars = computed(() => {
  if (!testResult.value) return 0
  const sum = (list) => (list || []).reduce((a, h) => a + (h.chars || 0), 0)
  return sum(testResult.value.front) + sum(testResult.value.tail)
})
const budgetPct = computed(() => {
  if (!testResult.value?.budget) return 0
  return Math.min(100, Math.round((usedChars.value / testResult.value.budget) * 100))
})

async function load() {
  loading.value = true
  try {
    entries.value = (await loreApi.list(props.worldId)) || []
  } catch (e) {
    ElMessage.error('加载设定库失败：' + e.message)
  } finally {
    loading.value = false
  }
}

function startCreate() {
  draft.value = emptyLoreEntry()
  isNew.value = true
}

function startEdit(e) {
  draft.value = {
    ...emptyLoreEntry(),
    ...JSON.parse(JSON.stringify(e)),
    keys: [...(e.keys || [])],
    secondary_keys: [...(e.secondary_keys || [])],
  }
  isNew.value = false
}

function entryPayload(e) {
  return {
    title: e.title,
    category: e.category,
    keys: e.keys || [],
    secondary_keys: e.secondary_keys || [],
    secondary_mode: e.secondary_keys?.length ? e.secondary_mode : '',
    constant: e.constant,
    position: e.position,
    priority: e.priority,
    enabled: e.enabled,
    content: e.content,
  }
}

async function saveEntry() {
  saving.value = true
  try {
    if (isNew.value) {
      await loreApi.create(props.worldId, entryPayload(draft.value))
      ElMessage.success('条目已创建')
    } else {
      await loreApi.update(props.worldId, draft.value.id, entryPayload(draft.value))
      ElMessage.success('条目已保存')
    }
    draft.value = null
    await load()
  } catch (e) {
    ElMessage.error((isNew.value ? '创建失败：' : '保存失败：') + e.message)
  } finally {
    saving.value = false
  }
}

async function removeEntry() {
  try {
    await ElMessageBox.confirm(`删除条目「${draft.value.title}」？此操作不可恢复。`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await loreApi.remove(props.worldId, draft.value.id)
    ElMessage.success('已删除')
    draft.value = null
    await load()
  } catch (e) {
    ElMessage.error('删除失败：' + e.message)
  }
}

async function runTest() {
  testing.value = true
  try {
    testResult.value = await loreApi.test(props.worldId, testText.value)
  } catch (e) {
    ElMessage.error('测试失败：' + e.message)
  } finally {
    testing.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.test-panel { margin-bottom: 14px; }
.test-title { font-weight: 600; }
.test-body { padding: 4px 0 8px; }
.budget-row { display: flex; align-items: center; gap: 12px; margin: 12px 0 4px; }
.hit-group { margin-top: 10px; }
.hit-group-title { font-size: 13px; font-weight: 600; margin-bottom: 4px; }
.hit-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 4px 8px;
  border-radius: 6px;
  font-size: 13px;
}
.hit-row:nth-child(odd) { background: var(--bg); }
.hit-row.dropped { opacity: .55; }
.hit-title { font-weight: 500; min-width: 120px; }

.lore-body { display: flex; gap: 16px; align-items: flex-start; }
.lore-left {
  width: 320px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.left-toolbar { display: flex; align-items: center; }
.spacer { flex: 1; }
.entry-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}
.entry-row:hover { background: var(--primary-soft); }
.entry-row.active { background: var(--primary-soft); font-weight: 600; }
.entry-title { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.entry-badges { display: flex; gap: 4px; flex-shrink: 0; }
.lore-right { flex: 1; min-width: 0; }

@media (max-width: 900px) {
  .lore-body { flex-direction: column; }
  .lore-left { width: 100%; }
}
</style>
