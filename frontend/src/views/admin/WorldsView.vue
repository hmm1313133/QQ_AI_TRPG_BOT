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

    <!-- 新建世界分步向导 -->
    <el-dialog v-model="createVisible" title="新建世界" width="780px" :close-on-click-modal="false">
      <el-steps :active="step" align-center finish-status="success" style="margin-bottom:22px">
        <el-step title="基本信息" />
        <el-step title="背景故事" />
        <el-step title="素材" />
        <el-step title="主线剧情" />
        <el-step title="确认" />
      </el-steps>

      <!-- ① 基本信息 -->
      <el-form v-show="step === 0" label-width="110px">
        <el-form-item label="模式">
          <el-radio-group v-model="form.mode">
            <el-radio value="trpg">trpg 剧本跑团</el-radio>
            <el-radio value="simrpg">simrpg 开放模拟</el-radio>
            <el-radio value="roleplay">roleplay 角色扮演</el-radio>
          </el-radio-group>
          <div class="muted mode-desc">{{ MODE_DESC[form.mode] }}</div>
        </el-form-item>
        <el-form-item label="世界 ID">
          <el-input v-model="form.world_id" placeholder="留空由服务端自动生成" style="max-width:320px" />
        </el-form-item>
        <el-form-item v-if="form.mode === 'trpg'" label="选择剧本" required>
          <el-select v-model="form.script_id" placeholder="选择已有剧本" style="width:100%" filterable>
            <el-option
              v-for="s in scriptOptions"
              :key="s.id"
              :value="s.id"
              :label="`${s.name}（${s.title} · ${s.system}）`"
            />
          </el-select>
          <div class="muted" style="margin-top:6px">创建时将自动带入模组的背景、主线、角色、场景地点与势力，无需手动配置。</div>
        </el-form-item>
      </el-form>

      <!-- ② 背景故事（含 lore 逐条 / 段落拆分录入） -->
      <el-form v-show="step === 1" label-width="110px">
        <el-form-item :label="form.mode === 'simrpg' ? '世界设定' : '世界背景'" :required="form.mode === 'simrpg'">
          <el-input v-model="form.background" type="textarea" :rows="4" placeholder="世界背景设定文本" />
        </el-form-item>
        <el-form-item v-if="form.mode === 'simrpg'" label="初始场景">
          <el-input v-model="form.scene" type="textarea" :rows="2" placeholder="初始场景描述（可选）" />
        </el-form-item>

        <!-- 设定库快速录入（设计文档 §3.2：逐条添加 + 段落拆分两种入口） -->
        <el-form-item v-if="form.mode !== 'trpg'" label="设定条目">
          <div class="loc-list">
            <div class="muted">
              只写静态设定（地理/势力/规则/历史）；HP、任务进度等易变事实由系统自动管理。可留空，创建后在世界编辑器中维护。
            </div>
            <div v-for="(e, i) in form.lore" :key="i" class="npc-card">
              <div class="loc-row">
                <el-input v-model="e.title" size="small" placeholder="条目标题（必填）" style="width:200px" />
                <el-checkbox v-model="e.constant" size="small">恒定</el-checkbox>
                <span class="spacer"></span>
                <el-button type="danger" plain size="small" circle @click="form.lore.splice(i, 1)">×</el-button>
              </div>
              <el-input v-model="e.keysText" size="small" placeholder="关键词（逗号/顿号分隔，恒定条目可留空）" style="margin-top:6px" />
              <el-input v-model="e.content" type="textarea" :rows="2" placeholder="设定正文（必填）" style="margin-top:6px" />
            </div>
            <div>
              <el-button size="small" plain @click="form.lore.push(newLoreDraft())">+ 逐条添加</el-button>
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
        <div v-else class="muted" style="padding-left:110px">trpg 模式的设定来自剧本，可创建后在编辑器的「设定库」页签追加。</div>
      </el-form>

      <!-- ③ 素材 -->
      <div v-show="step === 2" class="wizard-step">
        <div class="block">
          <div class="block-head">
            <div class="block-title">人物卡</div>
            <el-button size="small" type="primary" plain @click="pickerVisible = true">从素材选择</el-button>
          </div>
          <div class="muted">创建即关联全局人物卡（数值真相仍在卡上）。已选 {{ form.import_cards.length }} 张：</div>
          <div v-if="form.import_cards.length" class="chip-list">
            <el-tag
              v-for="c in form.import_cards"
              :key="c.id"
              closable
              size="small"
              :title="c.name || c.id"
              @close="form.import_cards = form.import_cards.filter((x) => x.id !== c.id)"
            >{{ c.name || c.id }}</el-tag>
          </div>
          <!-- 待导入素材（素材库/跨世界/剧本角色，创建成功后自动导入） -->
          <div v-if="!assetSelEmpty" style="margin-top:10px">
            <div class="muted">待导入素材（创建后自动导入）：</div>
            <div v-for="(line, i) in assetSelSummary" :key="i" class="muted" style="margin-top:2px">· {{ line }}</div>
            <el-button size="small" text type="danger" style="margin-top:4px"
              @click="form.asset_sel = { library_rows: [], copy: [], script_characters: [], on_conflict: 'skip' }">清空待导入素材</el-button>
          </div>
        </div>

        <div class="block">
          <div class="block-head">
            <div class="block-title">结构化 NPC（{{ form.npcs.length }}）</div>
            <el-button size="small" plain @click="addNpc">+ 添加 NPC</el-button>
          </div>
          <el-collapse v-if="form.npcs.length" v-model="openNpcs">
            <el-collapse-item v-for="(npc, i) in form.npcs" :key="i" :name="i">
              <template #title>
                <span class="char-title">{{ npc.name || '（未命名）' }}</span>
                <el-tag size="small" effect="plain">{{ npc.kind }}</el-tag>
                <el-tag size="small" effect="plain">{{ npc.disposition }}</el-tag>
              </template>
              <CharacterForm v-model="form.npcs[i]" compact />
              <div class="char-ops">
                <el-button type="danger" plain size="small" @click="form.npcs.splice(i, 1)">删除</el-button>
              </div>
            </el-collapse-item>
          </el-collapse>
        </div>

        <div class="block">
          <div class="block-head">
            <div class="block-title">初始地点（{{ form.locations.length }}）</div>
            <el-button size="small" plain @click="form.locations.push({ name: '', description: '', atmosphere: '', danger: '', points: [] })">+ 添加地点</el-button>
          </div>
          <div class="loc-list">
            <div v-for="(loc, i) in form.locations" :key="i" class="npc-card">
              <div class="loc-row">
                <el-input v-model="loc.name" size="small" placeholder="地点名称（必填）" style="width:180px" />
                <el-input v-model="loc.atmosphere" size="small" placeholder="氛围（可选）" style="width:160px" />
                <el-input v-model="loc.danger" size="small" placeholder="危险度（可选）" style="width:130px" />
                <span class="spacer"></span>
                <el-button type="danger" plain size="small" circle @click="form.locations.splice(i, 1)">×</el-button>
              </div>
              <el-input v-model="loc.description" size="small" placeholder="描述（可选）" style="margin-top:6px" />
              <TagsInput v-model="loc.points" placeholder="兴趣点 / 可调查处（可选）" style="margin-top:6px" />
            </div>
          </div>
        </div>

        <div class="block">
          <div class="block-head">
            <div class="block-title">物品（{{ form.items.length }}）</div>
            <el-button size="small" plain @click="form.items.push({ name: '', type: 'other', description: '', location: '', owner: '', tags: [] })">+ 添加物品</el-button>
          </div>
          <div v-for="(it, i) in form.items" :key="i" class="npc-card">
            <div class="loc-row">
              <el-input v-model="it.name" size="small" placeholder="物品名" style="width:170px" />
              <el-select v-model="it.type" size="small" style="width:130px">
                <el-option label="weapon 武器" value="weapon" />
                <el-option label="consumable 消耗品" value="consumable" />
                <el-option label="key 关键道具" value="key" />
                <el-option label="material 材料" value="material" />
                <el-option label="other 其他" value="other" />
              </el-select>
              <el-input v-model="it.location" size="small" placeholder="所在地点" style="width:130px" />
              <el-input v-model="it.owner" size="small" placeholder="持有者（角色名/玩家）" style="width:150px" />
              <el-button type="danger" plain size="small" circle @click="form.items.splice(i, 1)">×</el-button>
            </div>
            <el-input v-model="it.description" size="small" placeholder="物品描述" style="margin-top:6px" />
          </div>
        </div>

        <div class="block">
          <div class="block-head">
            <div class="block-title">势力（{{ form.factions.length }}）</div>
            <el-button size="small" plain @click="form.factions.push({ name: '', reputation: 0, description: '', goals: [], leader: '' })">+ 添加势力</el-button>
          </div>
          <div v-for="(f, i) in form.factions" :key="i" class="npc-card">
            <div class="loc-row">
              <el-input v-model="f.name" size="small" placeholder="势力名" style="width:180px" />
              <el-input v-model="f.leader" size="small" placeholder="领袖（角色名）" style="width:150px" />
              <el-button type="danger" plain size="small" circle @click="form.factions.splice(i, 1)">×</el-button>
            </div>
            <el-input v-model="f.description" size="small" placeholder="势力描述" style="margin-top:6px" />
            <TagsInput v-model="f.goals" placeholder="势力目标" style="margin-top:6px" />
          </div>
        </div>
      </div>

      <!-- ④ 主线剧情（仅 simrpg / roleplay） -->
      <div v-show="step === 3" class="wizard-step">
        <template v-if="form.mode === 'trpg'">
          <div class="empty">trpg 模式的主线由剧本时间轴驱动，无需在此设置。</div>
        </template>
        <el-form v-else label-width="90px">
          <el-form-item label="主线标题">
            <el-input v-model="form.storyline.title" style="max-width:360px" placeholder="可留空，创建后再设置" />
          </el-form-item>
          <el-form-item label="主线前提">
            <el-input v-model="form.storyline.premise" type="textarea" :rows="3" placeholder="核心悬念 / 前提" />
          </el-form-item>
          <el-form-item label="分幕">
            <div class="loc-list">
              <div v-for="(a, i) in form.storyline.acts" :key="i" class="npc-card">
                <div class="loc-row">
                  <span class="muted mono" style="width:24px">{{ i + 1 }}</span>
                  <el-input v-model="a.title" size="small" placeholder="幕标题" style="flex:1" />
                  <el-select v-model="a.status" size="small" style="width:110px">
                    <el-option label="未开始" value="pending" />
                    <el-option label="进行中" value="active" />
                    <el-option label="已完成" value="done" />
                  </el-select>
                  <el-button type="danger" plain size="small" circle @click="form.storyline.acts.splice(i, 1)">×</el-button>
                </div>
                <el-input v-model="a.summary" type="textarea" :rows="2" placeholder="本幕概要" style="margin-top:6px" />
              </div>
              <el-button size="small" plain @click="form.storyline.acts.push({ id: '', title: '', summary: '', status: 'pending' })">+ 添加一幕</el-button>
            </div>
          </el-form-item>
        </el-form>
      </div>

      <!-- ⑤ 确认 -->
      <div v-show="step === 4" class="wizard-step">
        <div class="summary-block">
          <div class="summary-row"><span class="muted">模式</span><b>{{ form.mode }}</b></div>
          <div class="summary-row"><span class="muted">世界 ID</span><span class="mono">{{ form.world_id || '（自动生成）' }}</span></div>
          <div v-if="form.mode === 'trpg'" class="summary-row">
            <span class="muted">剧本</span><span>{{ scriptName(form.script_id) || '-' }}</span>
          </div>
          <div class="summary-row"><span class="muted">背景</span><span>{{ form.background ? form.background.slice(0, 60) + (form.background.length > 60 ? '…' : '') : '（未填写）' }}</span></div>
          <div class="summary-row"><span class="muted">设定条目</span><span>{{ validLore.length }} 条</span></div>
          <div class="summary-row"><span class="muted">人物卡</span><span>{{ form.import_cards.length ? form.import_cards.map((c) => c.name || c.id).join('、') : '无' }}</span></div>
          <div class="summary-row">
            <span class="muted">待导入素材</span>
            <span>{{ assetSelEmpty ? '无' : assetSelSummary.join('；') }}</span>
          </div>
          <div class="summary-row"><span class="muted">结构化 NPC</span><span>{{ namedNpcs.length ? namedNpcs.map((n) => n.name).join('、') : '无' }}</span></div>
          <div class="summary-row"><span class="muted">地点</span><span>{{ validLocations.length ? validLocations.map((l) => l.name).join('、') : '无' }}</span></div>
          <div class="summary-row"><span class="muted">物品</span><span>{{ validItems.length ? validItems.map((x) => x.name).join('、') : '无' }}</span></div>
          <div class="summary-row"><span class="muted">势力</span><span>{{ validFactions.length ? validFactions.map((x) => x.name).join('、') : '无' }}</span></div>
          <div class="summary-row">
            <span class="muted">主线</span>
            <span>{{ form.mode !== 'trpg' && form.storyline.title.trim() ? `${form.storyline.title}（${validActs.length} 幕）` : '无' }}</span>
          </div>
        </div>
      </div>

      <template #footer>
        <el-button size="small" @click="createVisible = false">取消</el-button>
        <el-button v-if="step > 0" size="small" @click="step--">上一步</el-button>
        <el-button v-if="step < 4" type="primary" size="small" @click="nextStep">下一步</el-button>
        <el-button v-else type="primary" size="small" :loading="creating" @click="createWorld">创建世界</el-button>
      </template>
    </el-dialog>

    <!-- 素材选择（人物卡创建时直接关联，其余三类创建成功后经 assets/import 导入） -->
    <AssetPicker v-model:visible="pickerVisible" @confirm="onPickAssets" />
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { adminApi, adminReq, assetApi } from '../../api/admin'
import TagsInput from '../../components/TagsInput.vue'
import CharacterForm from '../../components/CharacterForm.vue'
import AssetPicker from '../../components/AssetPicker.vue'

const router = useRouter()

const worlds = ref([])

const createVisible = ref(false)
const creating = ref(false)
const scriptOptions = ref([])
const step = ref(0)
const pickerVisible = ref(false)
const openNpcs = ref([])

const form = reactive({
  mode: 'trpg',
  world_id: '',
  script_id: '',
  background: '',
  scene: '',
  locations: [],
  npcs: [],
  import_cards: [], // [{id, name}]
  // 待创建后导入的素材（素材库/跨世界复制/剧本角色；人物卡经 import_cards 创建时关联，不在此列）
  asset_sel: { library_rows: [], copy: [], script_characters: [], on_conflict: 'skip' },
  items: [],
  factions: [],
  storyline: { title: '', premise: '', acts: [] },
  lore: [],
})
const splitVisible = ref(false)
const splitText = ref('')
const splitDrafts = ref([])

const MODE_DESC = {
  trpg: '按已有剧本跑团：完整规则 + 时间轴推进，需选择剧本。',
  simrpg: '开放世界模拟：无固定剧本，轻量规则，离线后世界继续演化，需填世界设定与初始地点。',
  roleplay: '纯角色扮演：无规则无剧本，以 NPC 互动为核心，至少需要 1 个角色（结构化 NPC / 人物卡 / 角色类素材）。',
}

function newCharacter() {
  return {
    name: '', kind: 'npc', role: '', card_ref: '', alive: true, disposition: 'neutral',
    location: '', current_action: '', motivation: '', secrets: '', dialogue_style: '',
    key_dialogue: [], traits: [], appearance: '', personality: '', backstory: '', skills: [],
    goals: [], mood: { valence: 0, arousal: 0, tags: [], updated_at: 0 }, notes: '', id: '',
  }
}

function addNpc() {
  form.npcs.push(newCharacter())
  openNpcs.value = [form.npcs.length - 1]
}

function newLoreDraft() {
  return { title: '', keysText: '', content: '', constant: false }
}

function scriptName(id) {
  const s = scriptOptions.value.find((x) => x.id === id)
  return s ? s.name : id
}

// ---------- 汇总（确认步） ----------

const namedNpcs = computed(() => form.npcs.filter((n) => n.name.trim()))
const validLocations = computed(() => form.locations.filter((l) => l.name.trim()))
const validItems = computed(() => form.items.filter((x) => x.name.trim()))
const validFactions = computed(() => form.factions.filter((x) => x.name.trim()))
const validActs = computed(() => form.storyline.acts.filter((a) => a.title.trim()))
const validLore = computed(() => form.lore.filter((e) => e.title.trim() && e.content.trim()))

// ---------- 大段文本拆分（与服务端 importTextDrafts 同一规则：标题取首行前 20 字） ----------

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
    form.lore.push({ title: d.title, keysText: '', content: d.content, constant: false })
  }
  ElMessage.success(`已添加 ${splitDrafts.value.length} 条草稿，请逐条补关键词`)
  splitDrafts.value = []
  splitText.value = ''
  splitVisible.value = false
}

// ---------- 素材选择 ----------

async function onPickAssets(sel) {
  // 人物卡：创建时随 import_cards 直接关联（逻辑不变）
  if (sel.cards.length) {
    // 解析人物卡名称（用于预览与 roleplay 校验兜底）
    try {
      const catalog = await assetApi.catalog()
      const byId = new Map((catalog?.cards || []).map((c) => [c.id, c]))
      for (const id of sel.cards) {
        if (!form.import_cards.some((c) => c.id === id)) {
          form.import_cards.push({ id, name: byId.get(id)?.name || '' })
        }
      }
    } catch {
      for (const id of sel.cards) {
        if (!form.import_cards.some((c) => c.id === id)) form.import_cards.push({ id, name: '' })
      }
    }
  }
  // 其余三类：存入 asset_sel，创建成功后经 assets/import 导入（按 id/name 去重累加）
  const asel = form.asset_sel
  for (const r of sel.library_rows || []) {
    if (!asel.library_rows.some((x) => x.id === r.id)) asel.library_rows.push(r)
  }
  for (const c of sel.copy || []) {
    if (!asel.copy.some((x) => x.world_id === c.world_id && x.kind === c.kind && x.name === c.name)) asel.copy.push(c)
  }
  for (const s of sel.script_characters || []) {
    if (!asel.script_characters.some((x) => x.script_id === s.script_id && x.name === s.name)) asel.script_characters.push(s)
  }
  asel.on_conflict = sel.on_conflict || 'skip'
}

// 待导入素材是否为空
const assetSelEmpty = computed(() =>
  !form.asset_sel.library_rows.length && !form.asset_sel.copy.length && !form.asset_sel.script_characters.length
)

// 角色类素材来源（roleplay 校验与种子镜像用）：素材库角色 / 跨世界角色 / 剧本角色
const assetCharacterNames = computed(() => {
  const names = []
  for (const r of form.asset_sel.library_rows) {
    if (r.kind === 'character' && r.name) names.push(r.name)
  }
  for (const c of form.asset_sel.copy) {
    if (c.kind === 'character' && c.name) names.push(c.name)
  }
  for (const s of form.asset_sel.script_characters) {
    if (s.name) names.push(s.name)
  }
  return names
})

// 待导入素材摘要（向导素材区与确认步汇总展示）
const assetSelSummary = computed(() => {
  const parts = []
  if (form.asset_sel.library_rows.length) {
    parts.push(`素材库 ${form.asset_sel.library_rows.length} 项（${form.asset_sel.library_rows.map((r) => r.name || r.id).join('、')}）`)
  }
  if (form.asset_sel.copy.length) {
    parts.push(`跨世界复制 ${form.asset_sel.copy.length} 项（${form.asset_sel.copy.map((c) => c.name).join('、')}）`)
  }
  if (form.asset_sel.script_characters.length) {
    parts.push(`剧本角色 ${form.asset_sel.script_characters.length} 项（${form.asset_sel.script_characters.map((s) => s.name).join('、')}）`)
  }
  return parts
})

// ---------- 向导流程 ----------

function nextStep() {
  if (step.value === 0 && form.mode === 'trpg' && !form.script_id) {
    ElMessage.warning('trpg 模式必须选择剧本')
    return
  }
  if (step.value === 1 && form.mode === 'simrpg' && !form.background.trim()) {
    ElMessage.warning('simrpg 模式必须填写世界设定')
    return
  }
  step.value++
}

async function openCreate() {
  Object.assign(form, {
    mode: 'trpg', world_id: '', script_id: '', background: '', scene: '',
    locations: [], npcs: [], import_cards: [],
    asset_sel: { library_rows: [], copy: [], script_characters: [], on_conflict: 'skip' },
    items: [], factions: [],
    storyline: { title: '', premise: '', acts: [] }, lore: [],
  })
  step.value = 0
  openNpcs.value = []
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
  const f = form
  if (f.mode === 'trpg' && !f.script_id) { ElMessage.warning('trpg 模式必须选择剧本'); return }
  if (f.mode === 'simrpg' && !f.background.trim()) { ElMessage.warning('simrpg 模式必须填写世界设定'); return }
  if (f.mode === 'roleplay' && namedNpcs.value.length === 0 && f.import_cards.length === 0 && assetCharacterNames.value.length === 0) {
    ElMessage.warning('roleplay 模式至少需要 1 个角色（结构化 NPC / 人物卡 / 角色类素材）')
    return
  }
  // 重名检查
  const npcNames = namedNpcs.value.map((n) => n.name.trim())
  if (new Set(npcNames).size !== npcNames.length) { ElMessage.warning('NPC 名称重复'); return }

  const lore = f.mode === 'trpg' ? [] : validLore.value.map((e) => ({
    title: e.title.trim(),
    content: e.content,
    keys: e.keysText.split(/[,，、;；]+/).map((s) => s.trim()).filter(Boolean),
    constant: e.constant,
  }))

  // roleplay 服务端要求 npcs 非空：把结构化 NPC / 人物卡镜像为简易种子（同名会与结构化角色合并）
  let npcSeeds = []
  let mirrored = false // 仅靠素材角色镜像占位种子时置真（导入时强制 overwrite 覆盖占位）
  if (f.mode === 'roleplay') {
    npcSeeds = namedNpcs.value.map((n) => ({
      name: n.name.trim(), kind: n.kind, disposition: n.disposition, personality: n.personality,
      appearance: n.appearance, backstory: n.backstory, skills: n.skills, card_ref: n.card_ref,
    }))
    for (const c of f.import_cards) {
      if (c.name && !npcSeeds.some((n) => n.name === c.name)) {
        npcSeeds.push({ name: c.name, kind: 'pc', disposition: 'neutral', personality: '', card_ref: c.id })
      }
    }
    // 结构化 NPC 与人物卡都为空、只能靠素材角色满足校验时：
    // 把选中角色类素材的名字镜像为简易占位种子，创建后导入用完整素材覆盖
    if (!npcSeeds.length && assetCharacterNames.value.length) {
      for (const name of assetCharacterNames.value) {
        if (!npcSeeds.some((n) => n.name === name)) {
          npcSeeds.push({ name, kind: 'npc', disposition: 'neutral' })
        }
      }
      mirrored = true
    }
    if (!npcSeeds.length) {
      ElMessage.warning('roleplay 模式至少需要一个可命名的角色（素材/人物卡名称未能解析，请改加结构化 NPC）')
      return
    }
  }

  const storyline = f.mode !== 'trpg' && f.storyline.title.trim()
    ? {
        title: f.storyline.title.trim(),
        premise: f.storyline.premise,
        acts: validActs.value.map((a, i) => ({
          id: a.id || `act_${i + 1}`, title: a.title.trim(), summary: a.summary, status: a.status || 'pending',
        })),
      }
    : null

  const body = {
    world_id: f.world_id.trim(),
    mode: f.mode,
    background: f.background,
    scene: f.mode === 'simrpg' ? f.scene : '',
    script_id: f.mode === 'trpg' ? f.script_id : '',
    locations: [],
    location_defs: f.mode === 'simrpg' ? validLocations.value.map((l) => ({
      name: l.name.trim(), description: l.description, atmosphere: l.atmosphere,
      danger: l.danger, points: l.points || [],
    })) : [],
    npcs: npcSeeds,
    characters: namedNpcs.value,
    items: validItems.value,
    factions: validFactions.value,
    import_cards: f.import_cards.map((c) => c.id),
    lore,
  }
  if (storyline) body.storyline = storyline

  creating.value = true
  try {
    const state = await adminReq('/api/admin/worlds', { method: 'POST', body: JSON.stringify(body) })
    const id = state?.world_id || f.world_id.trim()
    ElMessage.success(`世界已创建，聊天中发送 .world enter ${id || '<id>'} 进入`)

    // 创建成功后导入待导入素材（人物卡已随创建关联，不重复传）。
    // 导入失败不影响世界已创建的事实，warning 提示后仍正常跳转编辑器。
    if (id && !assetSelEmpty.value) {
      try {
        const res = await assetApi.import(id, {
          library: f.asset_sel.library_rows.map((r) => r.id),
          cards: [],
          copy: f.asset_sel.copy,
          script_characters: f.asset_sel.script_characters,
          // 素材角色镜像占位种子时强制覆盖：占位种子无数据，以导入的完整素材为准
          on_conflict: mirrored ? 'overwrite' : f.asset_sel.on_conflict,
        })
        const imported = res?.imported ?? 0
        if (res?.conflicts?.length || res?.errors?.length) {
          const parts = [`成功导入 ${imported} 项`]
          if (res.conflicts?.length) parts.push(`跳过冲突 ${res.conflicts.length} 项`)
          if (res.errors?.length) parts.push(`失败 ${res.errors.length} 项：${res.errors.join('；')}`)
          ElMessage.warning(parts.join('，'))
        } else {
          ElMessage.success(`素材导入完成：${imported} 项`)
        }
      } catch (e) {
        ElMessage.warning('世界已创建，但素材导入失败（可在世界编辑器中重试）：' + e.message)
      }
    }

    createVisible.value = false
    if (id) {
      router.push(`/admin/worlds/${encodeURIComponent(id)}`)
    } else {
      loadWorlds()
    }
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

.wizard-step { max-height: 52vh; overflow-y: auto; padding-right: 6px; }
.block { margin-bottom: 18px; }
.block-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.block-title { font-size: 13.5px; font-weight: 600; }
.chip-list { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.char-title { font-weight: 600; margin-right: 10px; }
.char-ops { display: flex; justify-content: flex-end; margin-top: 8px; }
.el-collapse :deep(.el-tag) { margin-right: 6px; }
.empty { padding: 24px 0; text-align: center; color: var(--text-secondary); font-size: 13px; }

.summary-block { border: 1px solid var(--border); border-radius: 8px; padding: 12px 16px; background: var(--bg); }
.summary-row { display: flex; gap: 14px; padding: 6px 0; font-size: 13.5px; }
.summary-row .muted { flex: none; width: 80px; }
</style>
