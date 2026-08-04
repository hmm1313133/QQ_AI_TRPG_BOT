<template>
  <div>
    <div class="page-title">世界管理</div>
    <div class="page-desc">查看世界状态，手动推进剧情或修正状态</div>

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
            <el-button size="small" @click="showWorld(row.id)">详情</el-button>
            <el-button type="danger" plain size="small" @click="removeWorld(row.id)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 新建世界向导 -->
    <el-dialog v-model="createVisible" title="新建世界" width="620px" :close-on-click-modal="false">
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
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="createWorld">创建</el-button>
      </template>
    </el-dialog>

    <div v-if="detail" class="card">
      <div class="card-title">世界详情 <span class="muted">{{ currentWorld }}</span></div>
      <div class="detail-grid">
        <div>
          <div class="muted">当前场景</div>
          <p>{{ sceneText }}</p>
          <div class="muted">目标</div>
          <p v-if="quests.length" v-html="quests"></p>
          <p v-else>-</p>
          <div class="muted">指标（张力/混乱/掌控/进度）</div>
          <p>{{ metricsText }}</p>
        </div>
        <div>
          <div class="muted">NPC 状态</div>
          <p v-if="npcs.length" v-html="npcs"></p>
          <p v-else>-</p>
          <div class="muted">待触发事件</div>
          <p v-if="events.length" v-html="events"></p>
          <p v-else>-</p>
          <div class="muted">锁定事实</div>
          <p v-if="locks.length" v-html="locks"></p>
          <p v-else>-</p>
        </div>
      </div>
      <div style="display:flex;gap:10px;margin-top:14px">
        <el-button type="primary" size="small" @click="advance">推进时间轴 →</el-button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { adminApi, adminReq } from '../../api/admin'

const worlds = ref([])
const detail = ref(null)
const currentWorld = ref('')

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
})

const MODE_DESC = {
  trpg: '按已有剧本跑团：完整规则 + 时间轴推进，需选择剧本。',
  simrpg: '开放世界模拟：无固定剧本，轻量规则，离线后世界继续演化，需填世界设定与初始地点。',
  roleplay: '纯角色扮演：无规则无剧本，以 NPC 互动为核心，至少需要 1 个 NPC。',
}

function addNpc() {
  createForm.npcs.push({ name: '', kind: 'npc', disposition: 'neutral', personality: '' })
}

async function openCreate() {
  Object.assign(createForm, {
    mode: 'trpg', world_id: '', script_id: '', background: '', scene: '',
    locations: [], npcs: [],
  })
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
    if (currentWorld.value === id) { detail.value = null; currentWorld.value = '' }
    loadWorlds()
  } catch (e) {
    ElMessage.error(e.status === 409 ? '有会话正在使用该世界，无法删除' : e.message)
  }
}

const sceneText = computed(() => {
  if (!detail.value) return '-'
  const s = detail.value.scene || {}
  return `${s.node_name || '-'} (${s.node_id || '-'})`
})
const quests = computed(() => (detail.value?.quests || [])
  .map((q) => `${q.completed ? '✅' : '◻️'} ${escapeHtml(q.description)}`).join('<br>'))
const metricsText = computed(() => {
  const m = detail.value?.metrics || {}
  return `${m.tension_level} / ${m.chaos_level} / ${m.player_agency} / ${m.objective_progress}`
})
const npcs = computed(() => Object.values(detail.value?.characters || {})
  .map((n) => `${n.alive ? '🙂' : '⚰️'} ${escapeHtml(n.name)}: ${escapeHtml(n.disposition)}`).join('<br>'))
const events = computed(() => (detail.value?.event_queue || [])
  .filter((e) => !e.triggered).map((e) => `◦ ${escapeHtml(e.description)}`).join('<br>'))
const locks = computed(() => (detail.value?.locks || [])
  .map((l) => `🔒 ${escapeHtml(l.key)}`).join('<br>'))

function escapeHtml(s) {
  return String(s ?? '').replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]))
}

async function loadWorlds() {
  worlds.value = (await adminApi('/api/admin/worlds')) || []
}

async function showWorld(id) {
  detail.value = await adminApi('/api/admin/worlds/' + encodeURIComponent(id))
  currentWorld.value = id
}

async function advance() {
  if (!currentWorld.value) return
  try {
    const r = await adminApi(`/api/admin/worlds/${encodeURIComponent(currentWorld.value)}/advance`, { method: 'POST' })
    ElMessage.success(r.message || '已推进')
    showWorld(currentWorld.value)
  } catch (e) {
    ElMessage.error('推进失败')
  }
}

onMounted(loadWorlds)
</script>

<style scoped>
.detail-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.detail-grid p { font-size: 13.5px; line-height: 1.8; margin: 4px 0 12px; }
.mode-desc { margin-top: 6px; line-height: 1.6; }
.loc-list { display: flex; flex-direction: column; gap: 8px; align-items: flex-start; width: 100%; }
.loc-row { display: flex; gap: 8px; align-items: center; width: 100%; }
.npc-card {
  width: 100%; border: 1px solid var(--border); border-radius: 8px;
  padding: 8px 10px; background: var(--bg);
}
@media (max-width: 860px) {
  .detail-grid { grid-template-columns: 1fr; }
}
</style>
