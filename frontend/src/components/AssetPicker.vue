<template>
  <el-dialog
    :model-value="visible"
    title="从素材导入"
    width="760px"
    :close-on-click-modal="false"
    @update:model-value="emit('update:visible', $event)"
    @open="onOpen"
  >
    <el-tabs v-model="tab">
      <!-- 素材库 -->
      <el-tab-pane label="素材库" name="library">
        <div class="picker-toolbar">
          <el-select v-model="libKind" size="small" style="width:130px" @change="loadLibrary">
            <el-option label="全部类型" value="" />
            <el-option v-for="k in KIND_OPTIONS" :key="k.value" :label="k.label" :value="k.value" />
          </el-select>
          <el-input
            v-model="libQuery"
            size="small"
            clearable
            placeholder="关键词搜索"
            style="width:200px"
            @keyup.enter="loadLibrary"
            @clear="loadLibrary"
          />
          <el-button size="small" @click="loadLibrary">搜索</el-button>
        </div>
        <el-table
          :data="library"
          size="small"
          height="320"
          empty-text="素材库为空"
          @selection-change="(rows) => (selLibrary = rows)"
        >
          <el-table-column type="selection" width="40" />
          <el-table-column prop="name" label="名称" min-width="120" />
          <el-table-column label="类型" width="90">
            <template #default="{ row }">{{ kindLabel(row.kind) }}</template>
          </el-table-column>
          <el-table-column label="标签" min-width="120">
            <template #default="{ row }">
              <el-tag v-for="t in row.tags || []" :key="t" size="small" style="margin-right:4px" :title="t">{{ t }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="summary" label="摘要" min-width="160" show-overflow-tooltip />
          <el-table-column prop="source" label="来源" min-width="110" show-overflow-tooltip />
        </el-table>
      </el-tab-pane>

      <!-- 其他世界 -->
      <el-tab-pane label="其他世界" name="worlds">
        <el-select v-model="srcWorldId" size="small" placeholder="选择来源世界" style="width:100%" filterable>
          <el-option
            v-for="w in otherWorlds"
            :key="w.id"
            :value="w.id"
            :label="`${w.id}（${w.mode}）`"
          />
        </el-select>
        <div v-if="!srcWorld" class="empty">请先选择来源世界</div>
        <template v-else>
          <div v-for="g in worldGroups" :key="g.kind" class="copy-group">
            <div class="copy-group-title">{{ g.label }}（{{ g.names.length }}）</div>
            <el-checkbox
              v-for="n in g.names"
              :key="n"
              :model-value="isCopyChecked(srcWorld.id, g.kind, n)"
              size="small"
              @change="toggleCopy(srcWorld.id, g.kind, n, $event)"
            >{{ n }}</el-checkbox>
            <div v-if="!g.names.length" class="muted">无</div>
          </div>
        </template>
      </el-tab-pane>
    </el-tabs>

    <template #footer>
      <div class="picker-footer">
        <span class="muted">已选 {{ totalSelected }} 项</span>
        <el-radio-group v-model="onConflict" size="small">
          <el-radio-button value="skip">冲突时跳过</el-radio-button>
          <el-radio-button value="overwrite">冲突时覆盖</el-radio-button>
        </el-radio-group>
        <span class="spacer"></span>
        <el-button size="small" @click="emit('update:visible', false)">取消</el-button>
        <el-button type="primary" size="small" :disabled="!totalSelected" @click="confirm">确认导入</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { assetApi } from '../api/admin'

const props = defineProps({
  visible: { type: Boolean, default: false },
  // 排除的世界 ID（导入目标世界自身不出现在「其他世界」里）
  excludeWorldId: { type: String, default: '' },
})
const emit = defineEmits(['update:visible', 'confirm'])

const KIND_OPTIONS = [
  { label: '角色', value: 'character' },
  { label: '地点', value: 'location' },
  { label: '物品', value: 'item' },
  { label: '势力', value: 'faction' },
  { label: '主线', value: 'storyline' },
  { label: '世界观', value: 'world' },
]
const KIND_LABELS = Object.fromEntries(KIND_OPTIONS.map((k) => [k.value, k.label]))

const tab = ref('library')

// 素材库
const library = ref([])
const selLibrary = ref([])
const libKind = ref('')
const libQuery = ref('')

// 目录（其他世界）
const catalog = ref({ worlds: [] })
const srcWorldId = ref('')

// 跨世界复制勾选
const copySet = ref(new Map()) // key: `${world_id}|${kind}|${name}` -> {world_id,kind,name}

const onConflict = ref('skip')

function kindLabel(kind) {
  return KIND_LABELS[kind] || kind
}

const otherWorlds = computed(() =>
  (catalog.value.worlds || []).filter((w) => w.id !== props.excludeWorldId)
)
const srcWorld = computed(() =>
  otherWorlds.value.find((w) => w.id === srcWorldId.value) || null
)
const worldGroups = computed(() => {
  const w = srcWorld.value
  if (!w) return []
  return [
    { kind: 'character', label: '角色', names: w.characters || [] },
    { kind: 'location', label: '地点', names: w.locations || [] },
    { kind: 'item', label: '物品', names: w.items || [] },
    { kind: 'faction', label: '势力', names: w.factions || [] },
  ]
})

const totalSelected = computed(() =>
  selLibrary.value.length + copySet.value.size
)

function copyKey(worldId, kind, name) {
  return `${worldId}|${kind}|${name}`
}
function isCopyChecked(worldId, kind, name) {
  return copySet.value.has(copyKey(worldId, kind, name))
}
function toggleCopy(worldId, kind, name, checked) {
  const next = new Map(copySet.value)
  if (checked) next.set(copyKey(worldId, kind, name), { world_id: worldId, kind, name })
  else next.delete(copyKey(worldId, kind, name))
  copySet.value = next
}

async function loadLibrary() {
  try {
    library.value = (await assetApi.list({ kind: libKind.value, q: libQuery.value.trim() })) || []
  } catch (e) {
    ElMessage.error('加载素材库失败：' + e.message)
    library.value = []
  }
}

async function loadCatalog() {
  try {
    const data = await assetApi.catalog()
    catalog.value = {
      worlds: data?.worlds || [],
    }
  } catch (e) {
    ElMessage.error('加载素材目录失败：' + e.message)
  }
}

async function onOpen() {
  tab.value = 'library'
  selLibrary.value = []
  copySet.value = new Map()
  onConflict.value = 'skip'
  await Promise.all([loadLibrary(), loadCatalog()])
}

function confirm() {
  emit('confirm', {
    library: selLibrary.value.map((a) => a.id),
    // library_rows 完整行对象（含 kind/name），供创建向导区分角色类素材；library 保持 id 数组不变
    library_rows: selLibrary.value.map((a) => ({ id: a.id, kind: a.kind, name: a.name })),
    copy: [...copySet.value.values()],
    on_conflict: onConflict.value,
  })
  emit('update:visible', false)
}
</script>

<style scoped>
.picker-toolbar { display: flex; gap: 8px; margin-bottom: 10px; }
.copy-group { margin-top: 12px; }
.copy-group-title { font-size: 13px; font-weight: 600; margin-bottom: 6px; }
.copy-group :deep(.el-checkbox) { margin-right: 14px; }
.picker-footer { display: flex; align-items: center; gap: 12px; }
.spacer { flex: 1; }
.empty { padding: 24px 0; text-align: center; color: var(--text-secondary); font-size: 13px; }
.muted { color: var(--text-secondary); font-size: 12.5px; }
</style>
