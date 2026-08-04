<template>
  <div>
    <div class="editor-head">
      <div>
        <div class="page-title">世界编辑器 <span class="mono">{{ worldId }}</span></div>
        <div class="page-desc">
          {{ detail ? `模式 ${detail.mode}${detail.script_name ? ' · 剧本 ' + detail.script_name : ''}` : '加载中…' }}
        </div>
      </div>
      <el-button size="small" @click="router.push('/admin/worlds')">← 返回世界列表</el-button>
    </div>

    <el-tabs v-model="activeTab" class="editor-tabs">
      <!-- 概览 -->
      <el-tab-pane label="概览" name="overview">
        <div class="card">
          <div class="card-title">世界信息</div>
          <div v-if="!detail" class="empty">加载中…</div>
          <template v-else>
            <div class="overview-grid">
              <div><div class="muted">世界 ID</div><p class="mono">{{ detail.world_id }}</p></div>
              <div><div class="muted">模式</div><p>{{ detail.mode }}</p></div>
              <div><div class="muted">剧本</div><p>{{ detail.script_name || '-' }}</p></div>
              <div><div class="muted">回合数</div><p>{{ detail.round_count }}</p></div>
              <div><div class="muted">当前场景</div><p>{{ detail.scene?.node_name || '-' }}</p></div>
              <div><div class="muted">最近更新</div><p>{{ detail.last_update || '-' }}</p></div>
            </div>
            <div class="muted" style="margin-top:10px">指标（张力 / 混乱 / 掌控 / 进度）</div>
            <p>{{ metricsText }}</p>
          </template>
        </div>
        <div class="card danger-card">
          <div class="card-title">危险操作</div>
          <div style="display:flex;gap:10px">
            <el-button type="primary" plain size="small" :loading="advancing" @click="advance">推进时间轴 →</el-button>
            <el-button type="danger" plain size="small" @click="removeWorld">删除世界</el-button>
          </div>
        </div>
      </el-tab-pane>

      <!-- 设定库 -->
      <el-tab-pane label="设定库" name="lore" lazy>
        <div class="card">
          <LorebookPanel v-if="activeTab === 'lore'" :world-id="worldId" />
        </div>
      </el-tab-pane>

      <!-- 场景 -->
      <el-tab-pane label="场景" name="scene" lazy>
        <div class="card">
          <div class="card-title">当前场景</div>
          <div v-if="!sections.scene" class="empty">加载中…</div>
          <el-form v-else label-width="90px">
            <el-form-item label="节点 ID">
              <el-input v-model="sections.scene.node_id" style="max-width:280px" class="mono" />
            </el-form-item>
            <el-form-item label="名称" required>
              <el-input v-model="sections.scene.node_name" style="max-width:360px" />
            </el-form-item>
            <el-form-item label="类型">
              <el-input v-model="sections.scene.node_type" style="max-width:200px" placeholder="act / scene / event" />
            </el-form-item>
            <el-form-item label="描述">
              <el-input v-model="sections.scene.description" type="textarea" :rows="3" />
            </el-form-item>
            <el-form-item label="叙事">
              <el-input v-model="sections.scene.narrative" type="textarea" :rows="2" />
            </el-form-item>
            <el-form-item label="氛围">
              <el-input v-model="sections.scene.atmosphere" style="max-width:420px" />
            </el-form-item>
            <el-form-item label="危险度">
              <el-input v-model="sections.scene.danger_level" style="max-width:200px" placeholder="如 安全 / 低危 / 高危" />
            </el-form-item>
            <el-form-item label="调查点">
              <TagsInput v-model="sections.scene.investigation_points" placeholder="可调查的线索点" />
            </el-form-item>
            <el-form-item label="出口">
              <TagsInput v-model="sections.scene.exits" placeholder="可前往的地点 / 场景" />
            </el-form-item>
            <el-form-item label="KP 笔记">
              <el-input v-model="sections.scene.kp_notes" type="textarea" :rows="3" />
            </el-form-item>
          </el-form>
          <el-button type="primary" size="small" :loading="saving.scene" :disabled="!sections.scene" @click="saveSection('scene')">保存场景</el-button>
        </div>
      </el-tab-pane>

      <!-- 角色 -->
      <el-tab-pane label="角色" name="characters" lazy>
        <div class="card">
          <div class="card-title">NPC 列表</div>
          <div v-if="!sections.characters" class="empty">加载中…</div>
          <template v-else>
            <div v-for="(c, i) in charList" :key="i" class="npc-card">
              <div class="npc-head">
                <el-input v-model="c.name" size="small" placeholder="名称（作为唯一键）" style="width:180px" />
                <el-select v-model="c.kind" size="small" style="width:90px">
                  <el-option label="npc" value="npc" />
                  <el-option label="pc" value="pc" />
                </el-select>
                <el-select v-model="c.disposition" size="small" style="width:130px">
                  <el-option label="friendly 友善" value="friendly" />
                  <el-option label="neutral 中立" value="neutral" />
                  <el-option label="suspicious 警惕" value="suspicious" />
                  <el-option label="hostile 敌对" value="hostile" />
                  <el-option label="dead 死亡" value="dead" />
                </el-select>
                <el-checkbox v-model="c.alive" size="small">存活</el-checkbox>
                <el-input v-model="c.location" size="small" placeholder="所在地点" style="width:140px" />
                <span class="spacer"></span>
                <el-button type="danger" plain size="small" circle @click="charList.splice(i, 1)">×</el-button>
              </div>
              <div class="npc-grid">
                <el-input v-model="c.role" size="small" placeholder="角色定位 role" />
                <el-input v-model="c.current_action" size="small" placeholder="当前行动 current_action" />
                <el-input v-model="c.motivation" size="small" placeholder="动机 motivation" />
                <el-input v-model="c.secrets" size="small" placeholder="秘密 secrets" />
              </div>
              <el-input v-model="c.dialogue_style" size="small" placeholder="对话风格 dialogue_style" style="margin-top:6px" />
              <div class="npc-grid" style="margin-top:6px">
                <TagsInput v-model="c.traits" placeholder="性格特质（记仇/胆小/贪婪…）" />
                <TagsInput v-model="c.key_dialogue" placeholder="关键台词" />
              </div>
              <!-- 目标 -->
              <div class="goal-list">
                <div v-for="(g, gi) in c.goals" :key="gi" class="goal-row">
                  <el-input v-model="g.description" size="small" placeholder="目标描述" style="flex:1" />
                  <el-input-number v-model="g.priority" :min="1" :max="10" size="small" style="width:110px" title="优先级 1-10" />
                  <el-input-number v-model="g.progress" :min="0" :max="100" size="small" style="width:110px" title="进度 0-100" />
                  <el-button type="danger" plain size="small" circle @click="c.goals.splice(gi, 1)">×</el-button>
                </div>
                <el-button size="small" plain @click="c.goals.push({ description: '', priority: 5, progress: 0 })">+ 添加目标</el-button>
              </div>
              <div class="npc-grid" style="margin-top:6px">
                <div class="mood-row">
                  <span class="muted">心情 愉悦度</span>
                  <el-input-number v-model="c.mood.valence" :min="-100" :max="100" size="small" />
                  <span class="muted">激活度</span>
                  <el-input-number v-model="c.mood.arousal" :min="0" :max="100" size="small" />
                </div>
                <el-input v-model="c.notes" size="small" placeholder="备注 notes" />
              </div>
            </div>
            <el-button plain size="small" @click="addCharacter">+ 添加 NPC</el-button>
            <div style="margin-top:12px">
              <el-button type="primary" size="small" :loading="saving.characters" @click="saveCharacters">保存角色</el-button>
            </div>
          </template>
        </div>
      </el-tab-pane>

      <!-- 地点 / 势力 -->
      <el-tab-pane label="地点/势力" name="places" lazy>
        <div class="card">
          <div class="card-title">地点</div>
          <div v-if="!sections.locations" class="empty">加载中…</div>
          <template v-else>
            <div v-for="(l, i) in locList" :key="i" class="npc-card">
              <div class="npc-head">
                <el-input v-model="l.name" size="small" placeholder="地点名（唯一键）" style="width:200px" />
                <span class="spacer"></span>
                <el-button type="danger" plain size="small" circle @click="locList.splice(i, 1)">×</el-button>
              </div>
              <el-input v-model="l.description" type="textarea" :rows="2" placeholder="描述" style="margin-top:6px" />
              <TagsInput v-model="l.exits" placeholder="出口（可前往地点）" style="margin-top:6px" />
            </div>
            <el-button plain size="small" @click="locList.push({ name: '', description: '', exits: [] })">+ 添加地点</el-button>
            <div style="margin-top:12px">
              <el-button type="primary" size="small" :loading="saving.locations" @click="saveMapSection('locations', locList, '地点')">保存地点</el-button>
            </div>
          </template>
        </div>
        <div class="card">
          <div class="card-title">势力</div>
          <div v-if="!sections.factions" class="empty">加载中…</div>
          <template v-else>
            <div v-for="(f, i) in facList" :key="i" class="npc-card">
              <div class="npc-head">
                <el-input v-model="f.name" size="small" placeholder="势力名（唯一键）" style="width:200px" />
                <span class="muted">玩家声誉</span>
                <el-slider v-model="f.reputation" :min="-100" :max="100" style="flex:1;max-width:260px" />
                <span class="mono" style="width:40px;text-align:right">{{ f.reputation }}</span>
                <el-button type="danger" plain size="small" circle @click="facList.splice(i, 1)">×</el-button>
              </div>
            </div>
            <el-button plain size="small" @click="facList.push({ name: '', reputation: 0 })">+ 添加势力</el-button>
            <div style="margin-top:12px">
              <el-button type="primary" size="small" :loading="saving.factions" @click="saveMapSection('factions', facList, '势力')">保存势力</el-button>
            </div>
          </template>
        </div>
      </el-tab-pane>

      <!-- 任务 / 线索 -->
      <el-tab-pane label="任务/线索" name="quests" lazy>
        <div class="card">
          <div class="card-title">任务</div>
          <div v-if="!sections.quests" class="empty">加载中…</div>
          <template v-else>
            <div v-for="(q, i) in sections.quests" :key="i" class="quest-row">
              <el-checkbox v-model="q.completed" size="small">已完成</el-checkbox>
              <el-input v-model="q.description" size="small" placeholder="任务描述" style="flex:1" />
              <el-button type="danger" plain size="small" circle @click="sections.quests.splice(i, 1)">×</el-button>
            </div>
            <el-button plain size="small" @click="sections.quests.push({ description: '', completed: false })">+ 添加任务</el-button>
            <div style="margin-top:12px">
              <el-button type="primary" size="small" :loading="saving.quests" @click="saveSection('quests')">保存任务</el-button>
            </div>
          </template>
        </div>
        <div class="card">
          <div class="card-title">隐藏线索</div>
          <div v-if="!sections.hidden" class="empty">加载中…</div>
          <template v-else>
            <div v-for="(h, i) in sections.hidden" :key="i" class="quest-row">
              <el-input v-model="h.id" size="small" placeholder="ID" style="width:120px" class="mono" />
              <el-input v-model="h.description" size="small" placeholder="线索内容" style="flex:1" />
              <el-select v-model="h.source" size="small" style="width:110px">
                <el-option label="scene 场景" value="scene" />
                <el-option label="clue 线索" value="clue" />
                <el-option label="npc" value="npc" />
              </el-select>
              <el-checkbox v-model="h.discovered" size="small">已发现</el-checkbox>
              <el-button type="danger" plain size="small" circle @click="sections.hidden.splice(i, 1)">×</el-button>
            </div>
            <el-button plain size="small" @click="sections.hidden.push({ id: '', description: '', source: 'scene', discovered: false })">+ 添加线索</el-button>
            <div style="margin-top:12px">
              <el-button type="primary" size="small" :loading="saving.hidden" @click="saveSection('hidden')">保存线索</el-button>
            </div>
          </template>
        </div>
      </el-tab-pane>

      <!-- 注入记录 -->
      <el-tab-pane label="注入记录" name="injections" lazy>
        <div class="card">
          <div class="card-title">
            最近回合实际注入记录
            <el-button size="small" text style="margin-left:8px" @click="loadInjections">刷新</el-button>
          </div>
          <div v-if="injections === null" class="empty">加载中…</div>
          <div v-else-if="!injections.length" class="empty">暂无记录（世界产生新回合后可见）</div>
          <el-timeline v-else style="padding-left:4px">
            <el-timeline-item
              v-for="(r, idx) in [...injections].reverse()"
              :key="idx"
              :timestamp="`第 ${injections.length - idx} 条记录（旧 → 新共 ${injections.length} 条）`"
              placement="top"
            >
              <div v-if="r.front?.length" class="hit-group">
                <div class="hit-group-title">前部注入</div>
                <div v-for="(h, i) in r.front" :key="'f' + i" class="hit-row">
                  <span class="hit-title">{{ h.entry.title }}</span>
                  <span class="muted">{{ h.reason }}</span>
                  <span class="mono muted">{{ h.chars }} 字符</span>
                </div>
              </div>
              <div v-if="r.tail?.length" class="hit-group">
                <div class="hit-group-title">尾部注入</div>
                <div v-for="(h, i) in r.tail" :key="'t' + i" class="hit-row">
                  <span class="hit-title">{{ h.entry.title }}</span>
                  <span class="muted">{{ h.reason }}</span>
                  <span class="mono muted">{{ h.chars }} 字符</span>
                </div>
              </div>
              <div v-if="r.dropped?.length" class="hit-group">
                <div class="hit-group-title">被预算裁剪</div>
                <div v-for="(h, i) in r.dropped" :key="'d' + i" class="hit-row dropped">
                  <span class="hit-title">{{ h.entry.title }}</span>
                  <span class="muted">{{ h.reason }}</span>
                  <span class="mono muted">{{ h.chars }} 字符</span>
                </div>
              </div>
              <div v-if="!r.front?.length && !r.tail?.length && !r.dropped?.length" class="muted">本回合无 lore 注入</div>
            </el-timeline-item>
          </el-timeline>
        </div>
      </el-tab-pane>

      <!-- JSON -->
      <el-tab-pane label="JSON" name="json" lazy>
        <div class="card">
          <div class="card-title">完整世界状态 JSON（高阶编辑）</div>
          <div class="muted" style="margin-bottom:8px">
            保存时仅回写可编辑分区：scene / characters / locations / factions / quests / hidden_info / metrics；
            其余字段（clock、event_log、locks 等运行时数据）的修改会被忽略。
          </div>
          <div v-if="jsonText === null" class="empty">加载中…</div>
          <template v-else>
            <el-input v-model="jsonText" type="textarea" :rows="22" class="json-area" @blur="validateJson" />
            <div v-if="jsonError" class="json-error">{{ jsonError }}</div>
            <div style="margin-top:12px">
              <el-button type="primary" size="small" :loading="saving.json" @click="saveJson">校验并保存</el-button>
              <el-button size="small" @click="loadJson">重新加载</el-button>
            </div>
          </template>
        </div>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { worldApi } from '../../api/admin'
import { loreApi } from '../../api/admin'
import LorebookPanel from '../../components/LorebookPanel.vue'
import TagsInput from '../../components/TagsInput.vue'

const route = useRoute()
const router = useRouter()
const worldId = route.params.id

const activeTab = ref('overview')
const detail = ref(null)
const advancing = ref(false)

// 分区数据（按需加载）
const sections = reactive({
  scene: null,
  characters: null,
  locations: null,
  factions: null,
  quests: null,
  hidden: null,
})
const saving = reactive({
  scene: false, characters: false, locations: false,
  factions: false, quests: false, hidden: false, json: false,
})
const loaded = reactive({})
const injections = ref(null)

// 可编辑的列表形态（map 分区在编辑期间转为数组）
const charList = ref([])
const locList = ref([])
const facList = ref([])

const metricsText = computed(() => {
  const m = detail.value?.metrics
  if (!m) return '-'
  return `${m.tension_level} / ${m.chaos_level} / ${m.player_agency} / ${m.objective_progress}`
})

// ---------- 概览 ----------

async function loadDetail() {
  try {
    detail.value = await worldApi.detail(worldId)
  } catch (e) {
    ElMessage.error('加载世界详情失败：' + e.message)
  }
}

async function advance() {
  advancing.value = true
  try {
    const r = await worldApi.advance(worldId)
    ElMessage.success(r?.message || '已推进')
    await loadDetail()
  } catch (e) {
    ElMessage.error('推进失败：' + e.message)
  } finally {
    advancing.value = false
  }
}

async function removeWorld() {
  const id = worldId
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
    await worldApi.remove(id)
    ElMessage.success('已删除')
    router.push('/admin/worlds')
  } catch (e) {
    ElMessage.error(e.status === 409 ? '有会话正在使用该世界，无法删除' : e.message)
  }
}

// ---------- 分区加载 ----------

async function loadSection(part) {
  try {
    const data = await worldApi.section(worldId, part)
    sections[part] = data
    if (part === 'characters') {
      charList.value = Object.entries(data || {}).map(([k, v]) => normalizeChar(k, v))
    } else if (part === 'locations') {
      locList.value = Object.entries(data || {}).map(([k, v]) => ({ name: v?.name || k, description: v?.description || '', exits: [...(v?.exits || [])] }))
    } else if (part === 'factions') {
      facList.value = Object.entries(data || {}).map(([k, v]) => ({ name: v?.name || k, reputation: v?.reputation ?? 0 }))
    }
  } catch (e) {
    ElMessage.error(`加载分区 ${part} 失败：` + e.message)
    sections[part] = null
  }
}

function normalizeChar(key, v) {
  return {
    name: v?.name || key,
    kind: v?.kind || 'npc',
    role: v?.role || '',
    alive: v?.alive ?? true,
    disposition: v?.disposition || 'neutral',
    location: v?.location || '',
    current_action: v?.current_action || '',
    motivation: v?.motivation || '',
    secrets: v?.secrets || '',
    dialogue_style: v?.dialogue_style || '',
    key_dialogue: [...(v?.key_dialogue || [])],
    traits: [...(v?.traits || [])],
    goals: (v?.goals || []).map((g) => ({ description: g.description || '', priority: g.priority ?? 5, progress: g.progress ?? 0 })),
    mood: { valence: v?.mood?.valence ?? 0, arousal: v?.mood?.arousal ?? 0, tags: [...(v?.mood?.tags || [])], updated_at: v?.mood?.updated_at ?? 0 },
    notes: v?.notes || '',
    id: v?.id || '',
    card_ref: v?.card_ref || '',
  }
}

function addCharacter() {
  charList.value.push(normalizeChar('', null))
}

async function ensureTabData(tab) {
  const need = {
    scene: ['scene'],
    characters: ['characters'],
    places: ['locations', 'factions'],
    quests: ['quests', 'hidden'],
  }[tab]
  if (need) {
    for (const p of need) {
      if (!loaded[p]) {
        loaded[p] = true
        await loadSection(p)
      }
    }
  } else if (tab === 'injections' && injections.value === null) {
    await loadInjections()
  } else if (tab === 'json' && jsonText.value === null) {
    await loadJson()
  }
}

watch(activeTab, ensureTabData)

// ---------- 分区保存 ----------

async function saveSection(part) {
  saving[part] = true
  try {
    await worldApi.saveSection(worldId, part, sections[part])
    ElMessage.success('已保存')
  } catch (e) {
    ElMessage.error('保存失败：' + e.message)
  } finally {
    saving[part] = false
  }
}

async function saveCharacters() {
  const names = charList.value.map((c) => c.name.trim())
  if (names.some((n) => !n)) { ElMessage.warning('存在未命名的角色'); return }
  if (new Set(names).size !== names.length) { ElMessage.warning('角色名重复'); return }
  const map = {}
  for (const c of charList.value) map[c.name.trim()] = c
  saving.characters = true
  try {
    await worldApi.saveSection(worldId, 'characters', map)
    ElMessage.success('角色已保存')
    sections.characters = map
  } catch (e) {
    ElMessage.error('保存失败：' + e.message)
  } finally {
    saving.characters = false
  }
}

async function saveMapSection(part, list, label) {
  const names = list.map((x) => x.name.trim())
  if (names.some((n) => !n)) { ElMessage.warning(`存在未命名的${label}`); return }
  if (new Set(names).size !== names.length) { ElMessage.warning(`${label}名重复`); return }
  const map = {}
  for (const x of list) map[x.name.trim()] = x
  saving[part] = true
  try {
    await worldApi.saveSection(worldId, part, map)
    ElMessage.success(`${label}已保存`)
    sections[part] = map
  } catch (e) {
    ElMessage.error('保存失败：' + e.message)
  } finally {
    saving[part] = false
  }
}

// ---------- 注入记录 ----------

async function loadInjections() {
  try {
    injections.value = (await loreApi.injections(worldId)) || []
  } catch (e) {
    ElMessage.error('加载注入记录失败：' + e.message)
    injections.value = []
  }
}

// ---------- JSON 兜底 ----------

const jsonText = ref(null)
const jsonError = ref('')

// JSON 字段 → 可写分区（hidden_info 对应 part=hidden）
const JSON_WRITABLE = [
  ['scene', 'scene'],
  ['characters', 'characters'],
  ['locations', 'locations'],
  ['factions', 'factions'],
  ['quests', 'quests'],
  ['hidden_info', 'hidden'],
  ['metrics', 'metrics'],
]

async function loadJson() {
  jsonError.value = ''
  try {
    const full = await worldApi.detail(worldId)
    jsonText.value = JSON.stringify(full, null, 2)
  } catch (e) {
    ElMessage.error('加载失败：' + e.message)
  }
}

function validateJson() {
  if (jsonText.value === null) return null
  try {
    jsonError.value = ''
    return JSON.parse(jsonText.value)
  } catch (e) {
    jsonError.value = 'JSON 格式错误：' + e.message
    return null
  }
}

async function saveJson() {
  const obj = validateJson()
  if (!obj) { ElMessage.warning('请先修正 JSON 格式错误'); return }
  saving.json = true
  try {
    for (const [field, part] of JSON_WRITABLE) {
      if (obj[field] !== undefined && obj[field] !== null) {
        await worldApi.saveSection(worldId, part, obj[field])
      }
    }
    ElMessage.success('可写分区已全部保存')
    await loadDetail()
    loaded.scene = loaded.characters = loaded.locations = false
    loaded.factions = loaded.quests = loaded.hidden = false
  } catch (e) {
    ElMessage.error('保存失败：' + e.message)
  } finally {
    saving.json = false
  }
}

onMounted(loadDetail)
</script>

<style scoped>
.editor-head { display: flex; justify-content: space-between; align-items: flex-start; }
.overview-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 14px; }
.overview-grid p { font-size: 13.5px; margin-top: 2px; }
.danger-card { border-color: #f3c2c2; }

.npc-card {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 10px 12px;
  background: var(--bg);
  margin-bottom: 10px;
}
.npc-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.npc-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-top: 6px; }
.goal-list { margin-top: 8px; display: flex; flex-direction: column; gap: 6px; align-items: flex-start; }
.goal-row { display: flex; gap: 8px; align-items: center; width: 100%; }
.mood-row { display: flex; align-items: center; gap: 8px; }
.quest-row { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.spacer { flex: 1; }

.hit-group { margin-top: 8px; }
.hit-group-title { font-size: 13px; font-weight: 600; margin-bottom: 4px; }
.hit-row { display: flex; align-items: center; gap: 10px; padding: 4px 8px; border-radius: 6px; font-size: 13px; }
.hit-row:nth-child(odd) { background: var(--bg); }
.hit-row.dropped { opacity: .55; }
.hit-title { font-weight: 500; min-width: 120px; }

.json-area :deep(textarea) { font-family: ui-monospace, "Cascadia Code", Consolas, monospace; font-size: 12.5px; }
.json-error { color: var(--el-color-danger, #f56c6c); font-size: 12.5px; margin-top: 6px; }

@media (max-width: 900px) {
  .overview-grid { grid-template-columns: 1fr 1fr; }
  .npc-grid { grid-template-columns: 1fr; }
}
</style>
