<template>
  <div>
    <div class="page-title">素材库</div>
    <div class="page-desc">跨世界复用的创作素材：角色 / 地点 / 物品 / 势力 / 主线 / 世界观</div>

    <div class="card">
      <div class="toolbar">
        <el-select v-model="filter.kind" size="small" style="width:130px" @change="loadAssets">
          <el-option label="全部类型" value="" />
          <el-option v-for="k in KIND_OPTIONS" :key="k.value" :label="k.label" :value="k.value" />
        </el-select>
        <el-input
          v-model="filter.q"
          size="small"
          clearable
          placeholder="关键词搜索"
          style="width:200px"
          @keyup.enter="loadAssets"
          @clear="loadAssets"
        />
        <el-input
          v-model="filter.tag"
          size="small"
          clearable
          placeholder="标签筛选"
          style="width:140px"
          @keyup.enter="loadAssets"
          @clear="loadAssets"
        />
        <el-button size="small" @click="loadAssets">搜索</el-button>
        <span class="spacer"></span>
        <el-button size="small" @click="openParse">解析导入</el-button>
        <el-button type="primary" size="small" @click="openCreate">+ 新建素材</el-button>
      </div>

      <div v-if="loading" class="empty">加载中…</div>
      <el-table v-else :data="assets" size="small" empty-text="素材库为空，点击右上角新建">
        <el-table-column prop="name" label="名称" min-width="130" />
        <el-table-column label="类型" width="90">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ kindLabel(row.kind) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="标签" min-width="130">
          <template #default="{ row }">
            <el-tag v-for="t in row.tags || []" :key="t" size="small" style="margin-right:4px" :title="t">{{ t }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="summary" label="摘要" min-width="170" show-overflow-tooltip />
        <el-table-column prop="source" label="来源" min-width="120" show-overflow-tooltip />
        <el-table-column label="更新时间" width="150">
          <template #default="{ row }">{{ fmtTime(row.updated_at || row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="140">
          <template #default="{ row }">
            <el-button size="small" @click="openEdit(row)">编辑</el-button>
            <el-button type="danger" plain size="small" @click="removeAsset(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 新建 / 编辑素材 -->
    <el-dialog
      v-model="dialog.visible"
      :title="dialog.id ? '编辑素材' : '新建素材'"
      width="720px"
      :close-on-click-modal="false"
    >
      <el-form label-width="90px" size="small">
        <div class="meta-grid">
          <el-form-item label="类型" required>
            <el-select v-model="dialog.kind" :disabled="!!dialog.id" @change="resetPayload">
              <el-option v-for="k in KIND_OPTIONS" :key="k.value" :label="k.label" :value="k.value" />
            </el-select>
          </el-form-item>
          <el-form-item label="名称" required>
            <el-input v-model="dialog.name" placeholder="素材名称" />
          </el-form-item>
          <el-form-item label="来源">
            <el-input v-model="dialog.source" placeholder="如 手动创建" />
          </el-form-item>
          <el-form-item label="标签">
            <TagsInput v-model="dialog.tags" placeholder="素材标签" />
          </el-form-item>
        </div>
        <el-form-item label="摘要">
          <el-input v-model="dialog.summary" type="textarea" :rows="2" placeholder="一句话说明这个素材" />
        </el-form-item>
      </el-form>

      <el-divider content-position="left">实体内容</el-divider>

      <!-- character：精简版角色表单 -->
      <CharacterForm v-if="dialog.kind === 'character'" v-model="dialog.payload" compact />

      <!-- location -->
      <el-form v-else-if="dialog.kind === 'location'" label-width="90px" size="small">
        <el-form-item label="描述">
          <el-input v-model="dialog.payload.description" type="textarea" :rows="3" />
        </el-form-item>
        <div class="meta-grid">
          <el-form-item label="氛围">
            <el-input v-model="dialog.payload.atmosphere" placeholder="如 阴森潮湿" />
          </el-form-item>
          <el-form-item label="危险度">
            <el-input v-model="dialog.payload.danger" placeholder="如 低危" />
          </el-form-item>
        </div>
        <el-form-item label="出口">
          <TagsInput v-model="dialog.payload.exits" placeholder="可前往的地点" />
        </el-form-item>
        <el-form-item label="兴趣点">
          <TagsInput v-model="dialog.payload.points" placeholder="可调查处 / 兴趣点" />
        </el-form-item>
      </el-form>

      <!-- item -->
      <el-form v-else-if="dialog.kind === 'item'" label-width="90px" size="small">
        <div class="meta-grid">
          <el-form-item label="类型">
            <el-select v-model="dialog.payload.type">
              <el-option label="weapon 武器" value="weapon" />
              <el-option label="consumable 消耗品" value="consumable" />
              <el-option label="key 关键道具" value="key" />
              <el-option label="material 材料" value="material" />
              <el-option label="other 其他" value="other" />
            </el-select>
          </el-form-item>
          <el-form-item label="所在地点">
            <el-input v-model="dialog.payload.location" />
          </el-form-item>
        </div>
        <el-form-item label="描述">
          <el-input v-model="dialog.payload.description" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="标签">
          <TagsInput v-model="dialog.payload.tags" placeholder="物品标签" />
        </el-form-item>
      </el-form>

      <!-- faction -->
      <el-form v-else-if="dialog.kind === 'faction'" label-width="90px" size="small">
        <el-form-item label="领袖">
          <el-input v-model="dialog.payload.leader" placeholder="角色名" style="max-width:240px" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="dialog.payload.description" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="势力目标">
          <TagsInput v-model="dialog.payload.goals" placeholder="势力目标" />
        </el-form-item>
      </el-form>

      <!-- storyline -->
      <el-form v-else-if="dialog.kind === 'storyline'" label-width="90px" size="small">
        <el-form-item label="主线前提">
          <el-input v-model="dialog.payload.premise" type="textarea" :rows="3" placeholder="核心悬念 / 前提" />
        </el-form-item>
        <el-form-item label="分幕">
          <div class="act-list">
            <div v-for="(a, i) in dialog.payload.acts" :key="i" class="act-row">
              <span class="muted mono" style="width:26px">{{ i + 1 }}</span>
              <el-input v-model="a.title" size="small" placeholder="幕标题" style="flex:1" />
              <el-select v-model="a.status" size="small" style="width:100px">
                <el-option label="未开始" value="pending" />
                <el-option label="进行中" value="active" />
                <el-option label="已完成" value="done" />
              </el-select>
              <el-button type="danger" plain size="small" circle @click="dialog.payload.acts.splice(i, 1)">×</el-button>
            </div>
            <el-button size="small" plain @click="dialog.payload.acts.push({ id: '', title: '', summary: '', status: 'pending' })">+ 添加一幕</el-button>
          </div>
        </el-form-item>
      </el-form>

      <!-- world：世界观（lore 附带条目只读预览，详细编辑在设定库） -->
      <el-form v-else-if="dialog.kind === 'world'" label-width="90px" size="small">
        <el-form-item label="世界观概述">
          <el-input v-model="dialog.payload.setting" type="textarea" :rows="3" />
        </el-form-item>
        <div class="meta-grid">
          <el-form-item label="时代">
            <el-input v-model="dialog.payload.era" placeholder="如 近未来" />
          </el-form-item>
          <el-form-item label="地点">
            <el-input v-model="dialog.payload.location" placeholder="主要舞台" />
          </el-form-item>
          <el-form-item label="氛围">
            <el-input v-model="dialog.payload.atmosphere" placeholder="如 压抑神秘" />
          </el-form-item>
          <el-form-item label="基调">
            <el-input v-model="dialog.payload.tone" placeholder="如 黑暗悬疑" />
          </el-form-item>
        </div>
        <el-form-item label="详细背景">
          <el-input v-model="dialog.payload.backstory" type="textarea" :rows="4" />
        </el-form-item>
        <el-form-item label="主题">
          <TagsInput v-model="dialog.payload.themes" placeholder="主题关键词" />
        </el-form-item>
        <el-form-item label="附带设定">
          <div class="lore-list">
            <div v-for="(l, i) in dialog.payload.lore || []" :key="l.id || i" class="lore-item">
              <div class="lore-head">
                <span class="lore-title">{{ l.title }}</span>
                <el-tag v-for="k in l.keys || []" :key="k" size="small" effect="plain" :title="k">{{ k }}</el-tag>
                <span class="spacer"></span>
                <el-button type="danger" plain size="small" circle @click="dialog.payload.lore.splice(i, 1)">×</el-button>
              </div>
              <div class="muted lore-content">{{ truncate(l.content, 80) }}</div>
            </div>
            <div v-if="!(dialog.payload.lore || []).length" class="muted">暂无附带设定条目</div>
            <div class="muted">详细编辑请在导入世界后到设定库管理</div>
          </div>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button size="small" @click="dialog.visible = false">取消</el-button>
        <el-button type="primary" size="small" :loading="dialog.saving" @click="saveAsset">{{ dialog.id ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>

    <!-- 解析导入：粘贴文本或上传文件，解析为草稿后勾选批量入库 -->
    <el-dialog v-model="parseDlg.visible" title="解析导入" width="760px" :close-on-click-modal="false">
      <el-form label-width="90px" size="small">
        <el-form-item label="上传文件">
          <div style="width:100%">
            <input ref="parseFileEl" type="file" accept=".png,.json,.txt,.md" class="file-input" @change="onParseFile" />
            <div class="muted" style="margin-top:6px">支持 SillyTavern 角色卡（PNG / JSON）与 txt / md 文本</div>
          </div>
        </el-form-item>
        <el-form-item label="粘贴文本">
          <el-input
            v-model="parseDlg.text"
            type="textarea"
            :rows="5"
            :disabled="!!parseDlg.file"
            :placeholder="parseDlg.file ? '已选择文件，将优先解析文件' : '粘贴角色卡 / 设定文本，由 LLM 解析为素材草稿'"
          />
        </el-form-item>
      </el-form>
      <div style="display:flex;align-items:center;gap:10px">
        <el-button type="primary" size="small" :loading="parseDlg.parsing" @click="startParse">开始解析</el-button>
        <span class="muted">LLM 解析可能需要几十秒，请耐心等待</span>
      </div>

      <template v-if="parseDlg.drafts.length">
        <el-divider content-position="left">解析草稿（{{ parseDlg.drafts.length }} 条，来源：{{ parseDlg.parser || '-' }}）</el-divider>
        <div class="draft-list">
          <div v-for="g in draftGroups" :key="g.kind" class="draft-group">
            <div class="draft-group-title">
              <el-tag size="small" :type="KIND_TAG_TYPES[g.kind] || 'info'" effect="plain">{{ g.label }}</el-tag>
              <span class="muted">{{ g.items.length }} 条</span>
            </div>
            <div v-for="d in g.items" :key="d._key" class="draft-item">
              <el-checkbox v-model="d.checked" style="margin-right:8px" />
              <div class="draft-body">
                <div class="draft-row">
                  <el-input v-model="d.name" size="small" placeholder="素材名称" style="width:200px" />
                  <el-input v-model="d.summary" size="small" placeholder="一句话摘要" style="flex:1" />
                </div>
                <div class="muted draft-preview">{{ draftPreview(d) }}</div>
              </div>
            </div>
          </div>
        </div>
      </template>

      <template #footer>
        <el-button size="small" @click="parseDlg.visible = false">取消</el-button>
        <el-button
          type="primary"
          size="small"
          :loading="parseDlg.importing"
          :disabled="!checkedCount"
          @click="importDrafts"
        >入库所选（{{ checkedCount }}）</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { assetApi } from '../../api/admin'
import TagsInput from '../../components/TagsInput.vue'
import CharacterForm from '../../components/CharacterForm.vue'

const KIND_OPTIONS = [
  { label: '角色', value: 'character' },
  { label: '地点', value: 'location' },
  { label: '物品', value: 'item' },
  { label: '势力', value: 'faction' },
  { label: '主线', value: 'storyline' },
  { label: '世界观', value: 'world' },
]
const KIND_LABELS = Object.fromEntries(KIND_OPTIONS.map((k) => [k.value, k.label]))
// 草稿分组的标签着色
const KIND_TAG_TYPES = {
  character: 'primary',
  location: 'success',
  item: 'warning',
  faction: 'danger',
  storyline: 'info',
  world: 'primary',
}

const assets = ref([])
const loading = ref(false)
const filter = reactive({ kind: '', q: '', tag: '' })

const dialog = reactive({
  visible: false,
  id: '',
  kind: 'character',
  name: '',
  tags: [],
  summary: '',
  source: '',
  payload: {},
  saving: false,
})

function kindLabel(kind) {
  return KIND_LABELS[kind] || kind
}

function fmtTime(s) {
  return (s || '').replace('T', ' ').slice(0, 16) || '-'
}

function emptyPayload(kind) {
  switch (kind) {
    case 'character':
      return { name: '', kind: 'npc', disposition: 'neutral', alive: true }
    case 'location':
      return { name: '', description: '', atmosphere: '', danger: '', exits: [], points: [] }
    case 'item':
      return { name: '', type: 'other', description: '', location: '', owner: '', tags: [] }
    case 'faction':
      return { name: '', reputation: 0, description: '', goals: [], leader: '' }
    case 'storyline':
      return { title: '', premise: '', acts: [] }
    case 'world':
      return { setting: '', era: '', location: '', atmosphere: '', tone: '', backstory: '', themes: [], lore: [] }
    default:
      return {}
  }
}

async function loadAssets() {
  loading.value = true
  try {
    assets.value = (await assetApi.list({
      kind: filter.kind,
      q: filter.q.trim(),
      tag: filter.tag.trim(),
    })) || []
  } catch (e) {
    ElMessage.error('加载素材库失败：' + e.message)
    assets.value = []
  } finally {
    loading.value = false
  }
}

function resetPayload() {
  dialog.payload = emptyPayload(dialog.kind)
}

function openCreate() {
  Object.assign(dialog, {
    visible: true, id: '', kind: 'character', name: '',
    tags: [], summary: '', source: '', saving: false,
  })
  dialog.payload = emptyPayload('character')
}

async function openEdit(row) {
  try {
    const asset = await assetApi.get(row.id)
    Object.assign(dialog, {
      visible: true,
      id: asset.id,
      kind: asset.kind,
      name: asset.name,
      tags: [...(asset.tags || [])],
      summary: asset.summary || '',
      source: asset.source || '',
      saving: false,
    })
    let payload = emptyPayload(asset.kind)
    if (asset.payload) {
      // payload 是 json.RawMessage，前端拿到时已是解析好的对象
      const raw = typeof asset.payload === 'string' ? JSON.parse(asset.payload) : asset.payload
      payload = Object.assign(payload, raw)
    }
    dialog.payload = payload
  } catch (e) {
    ElMessage.error('加载素材详情失败：' + e.message)
  }
}

function buildPayload() {
  const p = { ...dialog.payload }
  // 实体名与素材名保持一致（storyline 用标题承载名称；world payload 没有 name 字段，跳过回写）
  if (dialog.kind === 'storyline') {
    p.title = p.title || dialog.name.trim()
    p.acts = (p.acts || [])
      .filter((a) => a.title.trim())
      .map((a, i) => ({ id: a.id || `act_${i + 1}`, title: a.title.trim(), summary: a.summary || '', status: a.status || 'pending' }))
  } else if (dialog.kind !== 'world') {
    p.name = dialog.name.trim()
  }
  return p
}

async function saveAsset() {
  if (!dialog.name.trim()) { ElMessage.warning('请填写素材名称'); return }
  const body = {
    kind: dialog.kind,
    name: dialog.name.trim(),
    tags: dialog.tags,
    summary: dialog.summary,
    source: dialog.source,
    payload: buildPayload(),
  }
  dialog.saving = true
  try {
    if (dialog.id) {
      await assetApi.update(dialog.id, body)
      ElMessage.success('素材已保存')
    } else {
      await assetApi.create(body)
      ElMessage.success('素材已创建')
    }
    dialog.visible = false
    await loadAssets()
  } catch (e) {
    ElMessage.error('保存失败：' + e.message)
  } finally {
    dialog.saving = false
  }
}

async function removeAsset(row) {
  try {
    await ElMessageBox.confirm(`删除素材「${row.name}」不可恢复，确定继续？`, '删除素材', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await assetApi.remove(row.id)
    ElMessage.success('已删除')
    await loadAssets()
  } catch (e) {
    ElMessage.error('删除失败：' + e.message)
  }
}

// ---------- 解析导入 ----------

const parseFileEl = ref(null)
const parseDlg = reactive({
  visible: false,
  file: null,
  text: '',
  parsing: false,
  parser: '',
  drafts: [], // [{ _key, checked, kind, name, summary, tags, payload }]
  importing: false,
})
let draftSeq = 0

const draftGroups = computed(() => {
  const groups = []
  for (const d of parseDlg.drafts) {
    let g = groups.find((x) => x.kind === d.kind)
    if (!g) {
      g = { kind: d.kind, label: kindLabel(d.kind), items: [] }
      groups.push(g)
    }
    g.items.push(d)
  }
  return groups
})
const checkedCount = computed(() => parseDlg.drafts.filter((d) => d.checked).length)

function truncate(s, n) {
  s = s || ''
  return s.length > n ? s.slice(0, n) + '…' : s
}

// 草稿 payload 摘要预览：按类型挑最有代表性的字段截断展示
function draftPreview(d) {
  const p = d.payload || {}
  const text = p.setting || p.backstory || p.personality || p.description || p.premise || d.summary || ''
  return truncate(text, 100)
}

function onParseFile(e) {
  parseDlg.file = e.target.files[0] || null
  if (parseDlg.file) parseDlg.text = ''
}

function openParse() {
  Object.assign(parseDlg, { visible: true, file: null, text: '', parsing: false, parser: '', drafts: [], importing: false })
  if (parseFileEl.value) parseFileEl.value.value = ''
}

async function startParse() {
  if (!parseDlg.file && !parseDlg.text.trim()) { ElMessage.warning('请选择文件或粘贴文本'); return }
  parseDlg.parsing = true
  try {
    // 错误响应为 text/plain，adminReq 抛出时 e.message 即后端错误文本（如「LLM 素材解析未配置」）
    const res = parseDlg.file
      ? await assetApi.parse(parseDlg.file)
      : await assetApi.parseText(parseDlg.text.trim())
    parseDlg.parser = res?.parser || ''
    parseDlg.drafts = (res?.drafts || []).map((d) => ({
      _key: ++draftSeq,
      checked: true,
      kind: d.kind || 'item',
      name: d.name || '',
      summary: d.summary || '',
      tags: d.tags || [],
      payload: d.payload || {},
    }))
    if (!parseDlg.drafts.length) ElMessage.warning('未解析出素材草稿')
  } catch (e) {
    ElMessage.error('解析失败：' + e.message)
  } finally {
    parseDlg.parsing = false
  }
}

async function importDrafts() {
  const chosen = parseDlg.drafts.filter((d) => d.checked)
  if (!chosen.length) { ElMessage.warning('请至少勾选一条草稿'); return }
  parseDlg.importing = true
  try {
    const res = await assetApi.batchCreate(chosen.map((d) => ({
      kind: d.kind,
      name: d.name.trim(),
      tags: d.tags,
      summary: d.summary,
      source: `解析导入（${parseDlg.parser || 'unknown'}）`,
      payload: d.payload,
    })))
    const errs = res?.errors || []
    if (errs.length) {
      ElMessage.warning(`成功入库 ${res?.created || 0} 条，失败 ${errs.length} 条：${errs.join('；')}`)
    } else {
      ElMessage.success(`已入库 ${res?.created || 0} 条素材`)
    }
    parseDlg.visible = false
    await loadAssets()
  } catch (e) {
    ElMessage.error('入库失败：' + e.message)
  } finally {
    parseDlg.importing = false
  }
}

onMounted(loadAssets)
</script>

<style scoped>
.toolbar { display: flex; gap: 8px; margin-bottom: 12px; flex-wrap: wrap; }
.spacer { flex: 1; }
.empty { padding: 24px 0; text-align: center; color: var(--text-secondary); font-size: 13px; }
.meta-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0 16px; }
.act-list { display: flex; flex-direction: column; gap: 6px; align-items: flex-start; width: 100%; }
.act-row { display: flex; align-items: center; gap: 8px; width: 100%; }
.lore-list { display: flex; flex-direction: column; gap: 8px; width: 100%; }
.lore-item { border: 1px solid var(--border); border-radius: 8px; padding: 8px 10px; background: var(--bg); }
.lore-head { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.lore-title { font-weight: 600; }
.lore-content { margin-top: 4px; line-height: 1.6; }
.file-input {
  padding: 8px 12px; font: inherit; font-size: 13.5px;
  border: 1px solid var(--border); border-radius: 8px; background: var(--surface);
}
.draft-list { max-height: 46vh; overflow-y: auto; padding-right: 4px; }
.draft-group { margin-bottom: 14px; }
.draft-group-title { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.draft-item { display: flex; align-items: flex-start; margin-bottom: 8px; }
.draft-body { flex: 1; }
.draft-row { display: flex; gap: 8px; }
.draft-preview { margin-top: 4px; line-height: 1.6; }

@media (max-width: 900px) {
  .meta-grid { grid-template-columns: 1fr; }
}
</style>
