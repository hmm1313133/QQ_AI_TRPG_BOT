<template>
  <div>
    <div class="page-title">世界管理</div>
    <div class="page-desc">查看世界状态，进入编辑器修正状态或维护设定库</div>

    <div class="card">
      <div class="card-title">世界列表</div>
      <div style="margin-bottom:12px">
        <el-button type="primary" size="small" @click="openCreate">新建世界</el-button>
      </div>
      <el-table :data="worlds" empty-text="暂无世界（在聊天端加载剧本后创建）">
        <el-table-column prop="id" label="世界">
          <template #default="{ row }"><span class="mono">{{ row.id }}</span></template>
        </el-table-column>
        <el-table-column prop="mode" label="模式" />
        <el-table-column label="剧本">
          <template #default="{ row }">{{ row.script_name || '-' }}</template>
        </el-table-column>
        <el-table-column label="当前场景">
          <template #default="{ row }">{{ row.scene || '-' }}</template>
        </el-table-column>
        <el-table-column prop="round" label="轮次" width="80" />
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button size="small" @click="router.push(`/admin/worlds/${encodeURIComponent(row.id)}`)">详情</el-button>
            <el-button type="danger" plain size="small" @click="removeWorld(row.id)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 新建世界向导 -->
    <el-dialog v-model="createVisible" title="新建世界" width="680px" :close-on-click-modal="false">
      <el-form label-width="110px">
        <el-form-item label="模式">
          <el-radio-group v-model="createForm.mode">
            <el-radio value="trpg">trpg 剧本跑团</el-radio>
            <el-radio value="simrpg">simrpg 开放模拟</el-radio>
            <el-radio value="roleplay">roleplay 角色扮演</el-radio>
          </el-radio-group>
          <div class="muted mode-desc">{{ MODE_DESC[createForm.mode] }}</div>
        </el-form-item>
        <el-form-item label="世界 ID">
          <el-input v-model="createForm.world_id" placeholder="留空由服务端自动生成" style="max-width:320px" />
        </el-form-item>

        <template v-if="createForm.mode === 'trpg'">
          <el-form-item label="选择剧本" required>
            <el-select v-model="createForm.script_id" placeholder="选择已有剧本" style="width:100%" filterable>
              <el-option
                v-for="s in scriptOptions"
                :key="s.id"
                :value="s.id"
                :label="`${s.name}（${s.title} · ${s.system}）`"
              />
            </el-select>
          </el-form-item>
        </template>

        <template v-else-if="createForm.mode === 'simrpg'">
          <el-form-item label="世界设定" required>
            <el-input v-model="createForm.background" type="textarea" :rows="4" placeholder="世界背景设定文本" />
          </el-form-item>
          <el-form-item label="初始场景">
            <el-input v-model="createForm.scene" type="textarea" :rows="2" placeholder="初始场景描述（可选）" />
          </el-form-item>
          <el-form-item label="初始地点">
            <div class="loc-list">
              <div v-for="(_, i) in createForm.locations" :key="i" class="loc-row">
                <el-input v-model="createForm.locations[i]" size="small" placeholder="地点名称" />
                <el-button type="danger" plain size="small" circle @click="createForm.locations.splice(i, 1)">×</el-button>
              </div>
              <el-button size="small" plain @click="createForm.locations.push('')">+ 添加地点</el-button>
            </div>
          </el-form-item>
        </template>

        <template v-else>
          <el-form-item label="世界设定">
            <el-input v-model="createForm.background" type="textarea" :rows="4" placeholder="世界背景设定文本" />
          </el-form-item>
          <el-form-item label="NPC 列表" required>
            <div class="loc-list">
              <div v-for="(npc, i) in createForm.npcs" :key="i" class="npc-card">
                <div class="loc-row">
                  <el-input v-model="npc.name" size="small" placeholder="名称（必填）" style="width:160px" />
                  <el-select v-model="npc.kind" size="small" style="width:100px">
                    <el-option label="npc" value="npc" />
                    <el-option label="pc" value="pc" />
                  </el-select>
                  <el-select v-model="npc.disposition" size="small" style="width:130px">
                    <el-option label="friendly 友善" value="friendly" />
                    <el-option label="neutral 中立" value="neutral" />
                    <el-option label="suspicious 警惕" value="suspicious" />
                    <el-option label="hostile 敌对" value="hostile" />
                  </el-select>
                  <el-button type="danger" plain size="small" circle @click="createForm.npcs.splice(i, 1)">×</el-button>
                </div>
                <el-input v-model="npc.personality" size="small" placeholder="性格 / 目标描述" style="margin-top:6px" />
              </div>
              <el-button size="small" plain @click="addNpc">+ 添加 NPC</el-button>
            </div>
          </el-form-item>
        </template>

        <!-- 设定库快速录入（设计文档 §3.2：逐条添加 + 段落拆分两种入口） -->
        <template v-if="createForm.mode !== 'trpg'">
          <el-form-item label="设定条目">
            <div class="loc-list">
              <div class="muted">
                只写静态设定（地理/势力/规则/历史）；HP、任务进度等易变事实由系统自动管理。可留空，创建后在世界编辑器中维护。
              </div>
              <div v-for="(e, i) in createForm.lore" :key="i" class="npc-card">
                <div class="loc-row">
                  <el-input v-model="e.title" size="small" placeholder="条目标题（必填）" style="width:200px" />
                  <el-checkbox v-model="e.constant" size="small">恒定</el-checkbox>
                  <span class="spacer"></span>
                  <el-button type="danger" plain size="small" circle @click="createForm.lore.splice(i, 1)">×</el-button>
                </div>
                <el-input v-model="e.keysText" size="small" placeholder="关键词（逗号/顿号分隔，恒定条目可留空）" style="margin-top:6px" />
                <el-input v-model="e.content" type="textarea" :rows="2" placeholder="设定正文（必填）" style="margin-top:6px" />
              </div>
              <div>
                <el-button size="small" plain @click="createForm.lore.push(newLoreDraft())">+ 逐条添加</el-button>
                <el-button size="small" plain @click="splitVisible = !splitVisible">
                  {{ splitVisible ? '收起段落拆分' : '粘贴大段文本拆分' }}
                </el-button>
              </div>
              <div v-if="splitVisible" class="npc-card">
                <el-input
                  v-model="splitText"
                  type="textarea"
                  :rows="5"
                  placeholder="粘贴大段设定文本，按空行拆成条目草稿"
                />
                <div style="margin-top:8px">
                  <el-button size="small" :disabled="!splitText.trim()" @click="previewSplit">预览拆分</el-button>
                </div>
                <template v-if="splitDrafts.length">
                  <div class="muted" style="margin:8px 0 4px">将拆分为 {{ splitDrafts.length }} 条（标题取首行前 20 字，可稍后补关键词）：</div>
                  <div v-for="(d, i) in splitDrafts" :key="i" class="split-draft">
                    <b>{{ d.title }}</b>
                    <span class="muted">{{ d.content.length }} 字</span>
                  </div>
                  <el-button size="small" type="primary" plain style="margin-top:8px" @click="confirmSplit">确认添加为条目草稿</el-button>
                </template>
              </div>
            </div>
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="createWorld">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { adminApi, adminReq } from '../../api/admin'

const router = useRouter()

const worlds = ref([])

const createVisible = ref(false)
const creating = ref(false)
const scriptOptions = ref([])
const createForm = reactive({
  mode: 'trpg',
  world_id: '',
  script_id: '',
  background: '',
  scene: '',
  locations: [],
  npcs: [],
  lore: [],
})
const splitVisible = ref(false)
const splitText = ref('')
const splitDrafts = ref([])

const MODE_DESC = {
  trpg: '按已有剧本跑团：完整规则 + 时间轴推进，需选择剧本。',
  simrpg: '开放世界模拟：无固定剧本，轻量规则，离线后世界继续演化，需填世界设定与初始地点。',
  roleplay: '纯角色扮演：无规则无剧本，以 NPC 互动为核心，至少需要 1 个 NPC。',
}

function addNpc() {
  createForm.npcs.push({ name: '', kind: 'npc', disposition: 'neutral', personality: '' })
}

function newLoreDraft() {
  return { title: '', keysText: '', content: '', constant: false }
}

// 大段文本按空行拆段落（与服务端 importTextDrafts 同一规则：标题取首行前 20 字）
function previewSplit() {
  const text = splitText.value.replace(/\r\n/g, '\n')
  splitDrafts.value = text
    .split('\n\n')
    .map((p) => p.trim())
    .filter(Boolean)
    .map((p) => {
      let title = p.split('\n')[0]
      if ([...title].length > 20) title = [...title].slice(0, 20).join('')
      return { title, content: p }
    })
  if (!splitDrafts.value.length) ElMessage.warning('未解析到有效段落')
}

function confirmSplit() {
  for (const d of splitDrafts.value) {
    createForm.lore.push({ title: d.title, keysText: '', content: d.content, constant: false })
  }
  ElMessage.success(`已添加 ${splitDrafts.value.length} 条草稿，请逐条补关键词`)
  splitDrafts.value = []
  splitText.value = ''
  splitVisible.value = false
}

async function openCreate() {
  Object.assign(createForm, {
    mode: 'trpg', world_id: '', script_id: '', background: '', scene: '',
    locations: [], npcs: [], lore: [],
  })
  splitVisible.value = false
  splitText.value = ''
  splitDrafts.value = []
  addNpc()
  createVisible.value = true
  try {
    scriptOptions.value = (await adminApi('/api/admin/scripts')) || []
  } catch {
    scriptOptions.value = []
  }
}

async function createWorld() {
  const f = createForm
  if (f.mode === 'trpg' && !f.script_id) { ElMessage.warning('trpg 模式必须选择剧本'); return }
  if (f.mode === 'simrpg' && !f.background.trim()) { ElMessage.warning('simrpg 模式必须填写世界设定'); return }
  if (f.mode === 'roleplay') {
    const valid = f.npcs.filter((n) => n.name.trim())
    if (valid.length === 0) { ElMessage.warning('roleplay 模式至少需要 1 个 NPC'); return }
  }
  const lore = f.mode === 'trpg' ? [] : f.lore
    .filter((e) => e.title.trim() && e.content.trim())
    .map((e) => ({
      title: e.title.trim(),
      content: e.content,
      keys: e.keysText.split(/[,，、;；]+/).map((s) => s.trim()).filter(Boolean),
      constant: e.constant,
    }))
  const body = {
    world_id: f.world_id.trim(),
    mode: f.mode,
    background: f.background,
    scene: f.scene,
    script_id: f.mode === 'trpg' ? f.script_id : '',
    locations: f.mode === 'simrpg' ? f.locations.map((s) => s.trim()).filter(Boolean) : [],
    npcs: f.mode === 'roleplay'
      ? f.npcs.filter((n) => n.name.trim()).map((n) => ({
          name: n.name.trim(), kind: n.kind, disposition: n.disposition, personality: n.personality,
        }))
      : [],
    lore,
  }
  creating.value = true
  try {
    await adminReq('/api/admin/worlds', { method: 'POST', body: JSON.stringify(body) })
    ElMessage.success('世界已创建')
    createVisible.value = false
    loadWorlds()
  } catch (e) {
    ElMessage.error(e.status === 409 ? '世界 ID 已存在，请更换或留空自动生成' : e.message)
  } finally {
    creating.value = false
  }
}

async function removeWorld(id) {
  try {
    const { value } = await ElMessageBox.prompt(
      `删除世界「${id}」不可恢复。请输入世界 ID 确认：`,
      '删除确认',
      {
        type: 'warning',
        confirmButtonText: '删除',
        cancelButtonText: '取消',
        inputPattern: new RegExp(`^${id.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`),
        inputErrorMessage: '输入的世界 ID 不匹配',
      }
    )
    if (value !== id) return
  } catch {
    return
  }
  try {
    await adminReq(`/api/admin/worlds/${encodeURIComponent(id)}`, { method: 'DELETE' })
    ElMessage.success('已删除')
    loadWorlds()
  } catch (e) {
    ElMessage.error(e.status === 409 ? '有会话正在使用该世界，无法删除' : e.message)
  }
}

async function loadWorlds() {
  worlds.value = (await adminApi('/api/admin/worlds')) || []
}

onMounted(loadWorlds)
</script>

<style scoped>
.mode-desc { margin-top: 6px; line-height: 1.6; }
.loc-list { display: flex; flex-direction: column; gap: 8px; align-items: flex-start; width: 100%; }
.loc-row { display: flex; gap: 8px; align-items: center; width: 100%; }
.npc-card {
  width: 100%; border: 1px solid var(--border); border-radius: 8px;
  padding: 8px 10px; background: var(--bg);
}
.spacer { flex: 1; }
.split-draft {
  display: flex; justify-content: space-between; gap: 10px;
  font-size: 12.5px; padding: 4px 8px; border-radius: 6px;
}
.split-draft:nth-child(odd) { background: var(--surface); }
</style>
