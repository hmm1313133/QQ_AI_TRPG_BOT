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

      <!-- 主线 -->
      <el-tab-pane label="主线" name="storyline" lazy>
        <div class="card">
          <div class="card-title">主线剧情</div>
          <div v-if="!loaded.storylineDone" class="empty">加载中…</div>
          <!-- trpg 模式：主线由剧本时间轴驱动，只读展示 -->
          <template v-else-if="isTrpg">
            <div class="muted" style="margin-bottom:10px">trpg 模式的主线由剧本时间轴驱动，此处仅供只读查看。</div>
            <div v-if="!sections.storyline" class="empty">该世界没有独立主线（进度见「场景」与时间轴）</div>
            <template v-else>
              <h3 class="sl-title">{{ sections.storyline.title }}</h3>
              <p v-if="sections.storyline.premise" class="muted">{{ sections.storyline.premise }}</p>
              <div v-for="(a, i) in sections.storyline.acts || []" :key="a.id || i" class="act-row">
                <span class="act-index">{{ i + 1 }}</span>
                <div class="act-body">
                  <b>{{ a.title }}</b>
                  <div v-if="a.summary" class="muted">{{ a.summary }}</div>
                </div>
                <el-tag size="small" :type="actStatusType(a.status)">{{ actStatusLabel(a.status) }}</el-tag>
              </div>
            </template>
          </template>
          <!-- simrpg / roleplay：可编辑 -->
          <template v-else>
            <div v-if="!storylineForm" class="empty">
              尚未设置主线剧情
              <div style="margin-top:10px">
                <el-button type="primary" plain size="small" @click="storylineForm = newStoryline()">+ 创建主线</el-button>
              </div>
            </div>
            <template v-else>
              <el-form label-width="90px">
                <el-form-item label="主线标题" required>
                  <el-input v-model="storylineForm.title" style="max-width:360px" placeholder="如 活神之手" />
                </el-form-item>
                <el-form-item label="主线前提">
                  <el-input v-model="storylineForm.premise" type="textarea" :rows="3" placeholder="核心悬念 / 前提" />
                </el-form-item>
                <el-form-item label="分幕">
                  <div class="act-list">
                    <div v-for="(a, i) in storylineForm.acts" :key="i" class="act-edit-card">
                      <div class="act-edit-head">
                        <span class="act-index">{{ i + 1 }}</span>
                        <el-input v-model="a.title" size="small" placeholder="幕标题" style="flex:1" />
                        <el-select v-model="a.status" size="small" style="width:110px">
                          <el-option label="未开始" value="pending" />
                          <el-option label="进行中" value="active" />
                          <el-option label="已完成" value="done" />
                        </el-select>
                        <el-button size="small" plain circle :disabled="i === 0" title="上移" @click="moveAct(i, -1)">↑</el-button>
                        <el-button size="small" plain circle :disabled="i === storylineForm.acts.length - 1" title="下移" @click="moveAct(i, 1)">↓</el-button>
                        <el-button type="danger" plain size="small" circle @click="storylineForm.acts.splice(i, 1)">×</el-button>
                      </div>
                      <el-input v-model="a.summary" type="textarea" :rows="2" placeholder="本幕概要" style="margin-top:6px" />
                    </div>
                    <el-button size="small" plain @click="storylineForm.acts.push({ id: '', title: '', summary: '', status: 'pending' })">+ 添加一幕</el-button>
                  </div>
                </el-form-item>
              </el-form>
              <div style="display:flex;gap:10px;margin-top:12px">
                <el-button type="primary" size="small" :loading="saving.storyline" @click="saveStoryline">保存主线</el-button>
                <el-button type="danger" plain size="small" @click="clearStoryline">清空主线</el-button>
              </div>
            </template>
          </template>
        </div>
      </el-tab-pane>

      <!-- 角色 -->
      <el-tab-pane label="角色" name="characters" lazy>
        <div class="card">
          <div class="card-title">角色列表</div>
          <div v-if="!sections.characters" class="empty">加载中…</div>
          <template v-else>
            <div class="tab-toolbar">
              <el-button plain size="small" @click="addCharacter">+ 添加 NPC</el-button>
              <el-button type="primary" plain size="small" @click="pickerVisible = true">从素材库导入</el-button>
            </div>
            <el-collapse v-if="charList.length" v-model="openChars">
              <el-collapse-item v-for="(c, i) in charList" :key="i" :name="i">
                <template #title>
                  <span class="char-title">{{ c.name || '（未命名）' }}</span>
                  <el-tag size="small" effect="plain">{{ c.kind }}</el-tag>
                  <el-tag size="small" effect="plain">{{ c.disposition }}</el-tag>
                  <el-tag v-if="!c.alive" type="danger" size="small">已死亡</el-tag>
                </template>
                <CharacterForm v-model="charList[i]" />
                <div class="char-ops">
                  <el-button size="small" plain @click="collectEntity('character', c.name)">收藏到素材库</el-button>
                  <el-button type="danger" plain size="small" @click="charList.splice(i, 1)">删除角色</el-button>
                </div>
              </el-collapse-item>
            </el-collapse>
            <div v-else class="empty">暂无角色，点击上方按钮添加或从素材导入</div>
            <div style="margin-top:12px">
              <el-button type="primary" size="small" :loading="saving.characters" @click="saveCharacters">保存角色</el-button>
            </div>
          </template>
        </div>
      </el-tab-pane>

      <!-- 物品 -->
      <el-tab-pane label="物品" name="items" lazy>
        <div class="card">
          <div class="card-title">物品</div>
          <div v-if="!sections.items" class="empty">加载中…</div>
          <template v-else>
            <div class="tab-toolbar">
              <el-button plain size="small" @click="addItem">+ 添加物品</el-button>
              <el-button type="primary" plain size="small" @click="pickerVisible = true">从素材库导入</el-button>
            </div>
            <div v-for="(it, i) in itemList" :key="i" class="npc-card">
              <div class="npc-head">
                <el-input v-model="it.name" size="small" placeholder="物品名（唯一键）" style="width:180px" />
                <el-select v-model="it.type" size="small" style="width:130px">
                  <el-option label="weapon 武器" value="weapon" />
                  <el-option label="consumable 消耗品" value="consumable" />
                  <el-option label="key 关键道具" value="key" />
                  <el-option label="material 材料" value="material" />
                  <el-option label="other 其他" value="other" />
                </el-select>
                <el-select v-model="it.location" size="small" clearable filterable placeholder="所在地点" style="width:150px">
                  <el-option v-for="l in locList" :key="l.name" :label="l.name" :value="l.name" />
                </el-select>
                <el-select v-model="it.owner" size="small" clearable filterable placeholder="持有者" style="width:150px">
                  <el-option label="玩家" value="玩家" />
                  <el-option v-for="c in charList" :key="c.name" :label="c.name" :value="c.name" />
                </el-select>
                <span class="spacer"></span>
                <el-button type="danger" plain size="small" circle @click="itemList.splice(i, 1)">×</el-button>
              </div>
              <el-input v-model="it.description" type="textarea" :rows="2" placeholder="物品描述" style="margin-top:6px" />
              <TagsInput v-model="it.tags" placeholder="标签（如 魔法 / 任务物品）" style="margin-top:6px" />
            </div>
            <div v-if="!itemList.length" class="empty">暂无物品</div>
            <div style="margin-top:12px">
              <el-button type="primary" size="small" :loading="saving.items" @click="saveMapSection('items', itemList, '物品')">保存物品</el-button>
            </div>
          </template>
        </div>
      </el-tab-pane>

      <!-- 地点 · 势力 -->
      <el-tab-pane label="地点·势力" name="places" lazy>
        <div class="card">
          <div class="card-title">地点</div>
          <div v-if="!sections.locations" class="empty">加载中…</div>
          <template v-else>
            <div v-for="(l, i) in locList" :key="i" class="npc-card">
              <div class="npc-head">
                <el-input v-model="l.name" size="small" placeholder="地点名（唯一键）" style="width:200px" />
                <el-input v-model="l.atmosphere" size="small" placeholder="氛围（如 阴森潮湿）" style="width:180px" />
                <el-input v-model="l.danger" size="small" placeholder="危险度（如 低危）" style="width:140px" />
                <span class="spacer"></span>
                <el-button size="small" plain @click="collectEntity('location', l.name)">收藏</el-button>
                <el-button type="danger" plain size="small" circle @click="locList.splice(i, 1)">×</el-button>
              </div>
              <el-input v-model="l.description" type="textarea" :rows="2" placeholder="描述" style="margin-top:6px" />
              <div class="npc-grid" style="margin-top:6px">
                <TagsInput v-model="l.exits" placeholder="出口（可前往地点）" />
                <TagsInput v-model="l.points" placeholder="兴趣点 / 可调查处" />
              </div>
            </div>
            <el-button plain size="small" @click="locList.push({ name: '', description: '', atmosphere: '', danger: '', exits: [], points: [] })">+ 添加地点</el-button>
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
                <el-input v-model="f.leader" size="small" placeholder="领袖（角色名）" style="width:140px" />
                <span class="spacer"></span>
                <el-button size="small" plain @click="collectEntity('faction', f.name)">收藏</el-button>
                <el-button type="danger" plain size="small" circle @click="facList.splice(i, 1)">×</el-button>
              </div>
              <div class="npc-head" style="margin-top:6px">
                <span class="muted">玩家声誉</span>
                <el-slider v-model="f.reputation" :min="-100" :max="100" style="flex:1;max-width:260px" />
                <span class="mono" style="width:40px;text-align:right">{{ f.reputation }}</span>
              </div>
              <el-input v-model="f.description" type="textarea" :rows="2" placeholder="势力描述" style="margin-top:6px" />
              <TagsInput v-model="f.goals" placeholder="势力目标" style="margin-top:6px" />
            </div>
            <el-button plain size="small" @click="facList.push({ name: '', reputation: 0, description: '', goals: [], leader: '' })">+ 添加势力</el-button>
            <div style="margin-top:12px">
              <el-button type="primary" size="small" :loading="saving.factions" @click="saveMapSection('factions', facList, '势力')">保存势力</el-button>
            </div>
          </template>
        </div>
      </el-tab-pane>

      <!-- 任务 · 线索 -->
      <el-tab-pane label="任务·线索" name="quests" lazy>
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

      <!-- 设定库 -->
      <el-tab-pane label="设定库" name="lore" lazy>
        <div class="card">
          <LorebookPanel v-if="activeTab === 'lore'" :world-id="worldId" />
        </div>
      </el-tab-pane>

      <!-- 存档 -->
      <el-tab-pane label="存档" name="saves" lazy>
        <div class="card">
          <div class="card-title">
            游玩存档
            <el-button size="small" text style="margin-left:8px" @click="loadSaves">刷新</el-button>
          </div>
          <div class="tab-toolbar">
            <el-button type="primary" size="small" @click="openSaveDialog">+ 新建存档</el-button>
          </div>
          <div v-if="saves === null" class="empty">加载中…</div>
          <el-table v-else :data="saves" size="small" empty-text="暂无存档">
            <el-table-column prop="name" label="名称" min-width="140" />
            <el-table-column prop="round_count" label="轮次" width="70" />
            <el-table-column label="时间" width="160">
              <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column prop="note" label="备注" min-width="140" show-overflow-tooltip />
            <el-table-column label="类型" width="80">
              <template #default="{ row }">
                <el-tag v-if="row.auto" size="small" type="info" effect="plain">自动</el-tag>
                <el-tag v-else size="small" effect="plain">手动</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="150">
              <template #default="{ row }">
                <el-button size="small" @click="restoreSave(row)">恢复</el-button>
                <el-button type="danger" plain size="small" @click="removeSave(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <!-- 新建存档 -->
        <el-dialog v-model="saveDialog.visible" title="新建存档" width="420px" :close-on-click-modal="false">
          <el-form label-width="70px">
            <el-form-item label="名称" required>
              <el-input v-model="saveDialog.name" placeholder="如 进入古堡前" />
            </el-form-item>
            <el-form-item label="备注">
              <el-input v-model="saveDialog.note" type="textarea" :rows="2" placeholder="可选" />
            </el-form-item>
          </el-form>
          <template #footer>
            <el-button size="small" @click="saveDialog.visible = false">取消</el-button>
            <el-button type="primary" size="small" :loading="saveDialog.creating" @click="createSave">保存</el-button>
          </template>
        </el-dialog>
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
            保存时仅回写可编辑分区：scene / characters / locations / factions / items / storyline / quests / hidden_info / metrics；
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

    <!-- 素材导入 -->
    <AssetPicker v-model:visible="pickerVisible" :exclude-world-id="worldId" @confirm="onImportConfirm" />
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { worldApi, loreApi, assetApi, saveApi } from '../../api/admin'
import LorebookPanel from '../../components/LorebookPanel.vue'
import TagsInput from '../../components/TagsInput.vue'
import CharacterForm from '../../components/CharacterForm.vue'
import AssetPicker from '../../components/AssetPicker.vue'

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
  items: null,
  storyline: null,
})
const saving = reactive({
  scene: false, characters: false, locations: false, factions: false,
  quests: false, hidden: false, items: false, storyline: false, json: false,
})
const loaded = reactive({})
const injections = ref(null)

// 可编辑的列表形态（map 分区在编辑期间转为数组）
const charList = ref([])
const locList = ref([])
const facList = ref([])
const itemList = ref([])
const openChars = ref([])
const storylineForm = ref(null)

// 素材导入
const pickerVisible = ref(false)
const importing = ref(false)

// 存档
const saves = ref(null)
const saveDialog = reactive({ visible: false, name: '', note: '', creating: false })

const isTrpg = computed(() => detail.value?.mode === 'trpg')

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
      locList.value = Object.entries(data || {}).map(([k, v]) => ({
        name: v?.name || k,
        description: v?.description || '',
        atmosphere: v?.atmosphere || '',
        danger: v?.danger || '',
        exits: [...(v?.exits || [])],
        points: [...(v?.points || [])],
      }))
    } else if (part === 'factions') {
      facList.value = Object.entries(data || {}).map(([k, v]) => ({
        name: v?.name || k,
        reputation: v?.reputation ?? 0,
        description: v?.description || '',
        goals: [...(v?.goals || [])],
        leader: v?.leader || '',
      }))
    } else if (part === 'items') {
      itemList.value = Object.entries(data || {}).map(([k, v]) => ({
        name: v?.name || k,
        type: v?.type || 'other',
        description: v?.description || '',
        location: v?.location || '',
        owner: v?.owner || '',
        tags: [...(v?.tags || [])],
      }))
    } else if (part === 'storyline') {
      storylineForm.value = data
        ? {
            title: data.title || '',
            premise: data.premise || '',
            acts: (data.acts || []).map((a, i) => ({
              id: a.id || `act_${i + 1}`,
              title: a.title || '',
              summary: a.summary || '',
              status: a.status || 'pending',
            })),
          }
        : null
      loaded.storylineDone = true
    }
  } catch (e) {
    ElMessage.error(`加载分区 ${part} 失败：` + e.message)
    sections[part] = null
    if (part === 'storyline') loaded.storylineDone = true
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
    appearance: v?.appearance || '',
    personality: v?.personality || '',
    backstory: v?.backstory || '',
    skills: [...(v?.skills || [])],
    goals: (v?.goals || []).map((g) => ({ description: g.description || '', priority: g.priority ?? 5, progress: g.progress ?? 0 })),
    mood: { valence: v?.mood?.valence ?? 0, arousal: v?.mood?.arousal ?? 0, tags: [...(v?.mood?.tags || [])], updated_at: v?.mood?.updated_at ?? 0 },
    notes: v?.notes || '',
    id: v?.id || '',
    card_ref: v?.card_ref || '',
  }
}

function addCharacter() {
  charList.value.push(normalizeChar('', null))
  openChars.value = [charList.value.length - 1]
}

function addItem() {
  itemList.value.push({ name: '', type: 'other', description: '', location: '', owner: '', tags: [] })
}

async function ensureTabData(tab) {
  const need = {
    scene: ['scene'],
    storyline: ['storyline'],
    characters: ['characters'],
    items: ['items', 'locations', 'characters'],
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
  } else if (tab === 'saves' && saves.value === null) {
    await loadSaves()
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

// ---------- 主线 ----------

function newStoryline() {
  return { title: '', premise: '', acts: [{ id: 'act_1', title: '', summary: '', status: 'pending' }] }
}

function moveAct(i, dir) {
  const acts = storylineForm.value.acts
  const j = i + dir
  if (j < 0 || j >= acts.length) return
  const [a] = acts.splice(i, 1)
  acts.splice(j, 0, a)
}

function actStatusLabel(s) {
  return { pending: '未开始', active: '进行中', done: '已完成' }[s] || s
}

function actStatusType(s) {
  return { pending: 'info', active: 'warning', done: 'success' }[s] || 'info'
}

async function saveStoryline() {
  const f = storylineForm.value
  if (!f.title.trim()) { ElMessage.warning('请填写主线标题'); return }
  const data = {
    title: f.title.trim(),
    premise: f.premise,
    acts: f.acts
      .filter((a) => a.title.trim())
      .map((a, i) => ({ id: a.id || `act_${i + 1}`, title: a.title.trim(), summary: a.summary, status: a.status || 'pending' })),
  }
  saving.storyline = true
  try {
    await worldApi.saveSection(worldId, 'storyline', data)
    ElMessage.success('主线已保存')
    sections.storyline = data
  } catch (e) {
    ElMessage.error('保存失败：' + e.message)
  } finally {
    saving.storyline = false
  }
}

async function clearStoryline() {
  try {
    await ElMessageBox.confirm('清空后该世界将没有主线剧情，确定继续？', '清空主线', {
      type: 'warning',
      confirmButtonText: '清空',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  saving.storyline = true
  try {
    await worldApi.saveSection(worldId, 'storyline', null)
    ElMessage.success('主线已清空')
    sections.storyline = null
    storylineForm.value = null
  } catch (e) {
    ElMessage.error('清空失败：' + e.message)
  } finally {
    saving.storyline = false
  }
}

// ---------- 素材联动 ----------

function escapeHtml(s) {
  return String(s).replace(/[&<>"]/g, (ch) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[ch]))
}

async function onImportConfirm(payload) {
  importing.value = true
  try {
    const res = await assetApi.import(worldId, payload)
    const lines = [`成功导入 ${res?.imported ?? 0} 项`]
    if (res?.conflicts?.length) lines.push(`跳过冲突 ${res.conflicts.length} 项：${res.conflicts.map(escapeHtml).join('、')}`)
    if (res?.errors?.length) lines.push(`失败 ${res.errors.length} 项：${res.errors.map(escapeHtml).join('；')}`)
    ElMessageBox.alert(lines.join('<br>'), '导入结果', {
      confirmButtonText: '知道了',
      dangerouslyUseHTMLStringMessage: true,
    })
    // 导入会影响多个分区，全部标记为未加载并按需重载
    for (const k of Object.keys(loaded)) loaded[k] = false
    await ensureTabData(activeTab.value)
  } catch (e) {
    ElMessage.error('导入失败：' + e.message)
  } finally {
    importing.value = false
  }
}

async function collectEntity(kind, name) {
  if (!name?.trim()) { ElMessage.warning('请先填写名称并保存，再收藏'); return }
  let summary = ''
  try {
    const { value } = await ElMessageBox.prompt(
      '收藏的是服务端已保存的实体（请先保存当前修改）。可填写一句摘要（可选）：',
      `收藏「${name}」到素材库`,
      { confirmButtonText: '收藏', cancelButtonText: '取消' }
    )
    summary = value || ''
  } catch {
    return
  }
  try {
    await assetApi.collect(worldId, { kind, name: name.trim(), tags: [], summary })
    ElMessage.success('已收藏到素材库')
  } catch (e) {
    ElMessage.error('收藏失败：' + e.message)
  }
}

// ---------- 存档 ----------

function fmtTime(s) {
  return (s || '').replace('T', ' ').slice(0, 19) || '-'
}

async function loadSaves() {
  try {
    saves.value = (await saveApi.list(worldId)) || []
  } catch (e) {
    ElMessage.error('加载存档失败：' + e.message)
    saves.value = []
  }
}

function openSaveDialog() {
  saveDialog.name = ''
  saveDialog.note = ''
  saveDialog.visible = true
}

async function createSave() {
  if (!saveDialog.name.trim()) { ElMessage.warning('请填写存档名称'); return }
  saveDialog.creating = true
  try {
    await saveApi.create(worldId, { name: saveDialog.name.trim(), note: saveDialog.note.trim() })
    ElMessage.success('存档已创建')
    saveDialog.visible = false
    await loadSaves()
  } catch (e) {
    ElMessage.error('创建存档失败：' + e.message)
  } finally {
    saveDialog.creating = false
  }
}

async function restoreSave(row) {
  try {
    await ElMessageBox.confirm(
      `恢复到存档「${row.name}」（第 ${row.round_count} 轮）？当前进度会先自动备份为一条自动存档。`,
      '恢复存档',
      { type: 'warning', confirmButtonText: '恢复', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    const r = await saveApi.restore(worldId, row.id)
    ElMessage.success(r?.message || '已恢复')
    await loadSaves()
    await loadDetail()
  } catch (e) {
    ElMessage.error('恢复失败：' + e.message)
  }
}

async function removeSave(row) {
  try {
    await ElMessageBox.confirm(`删除存档「${row.name}」不可恢复，确定继续？`, '删除存档', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await saveApi.remove(worldId, row.id)
    ElMessage.success('已删除')
    await loadSaves()
  } catch (e) {
    ElMessage.error('删除失败：' + e.message)
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
  ['items', 'items'],
  ['storyline', 'storyline'],
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
    for (const k of Object.keys(loaded)) loaded[k] = false
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

.tab-toolbar { display: flex; gap: 10px; margin-bottom: 12px; }

.npc-card {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 10px 12px;
  background: var(--bg);
  margin-bottom: 10px;
}
.npc-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.npc-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-top: 6px; }
.quest-row { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.spacer { flex: 1; }

.char-title { font-weight: 600; margin-right: 10px; }
.char-ops { display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px; }
.el-collapse :deep(.el-tag) { margin-right: 6px; }

.sl-title { margin: 4px 0 6px; font-size: 15px; }
.act-list { display: flex; flex-direction: column; gap: 8px; align-items: flex-start; width: 100%; }
.act-edit-card {
  width: 100%; border: 1px solid var(--border); border-radius: 8px;
  padding: 8px 10px; background: var(--bg);
}
.act-edit-head { display: flex; align-items: center; gap: 8px; }
.act-row { display: flex; align-items: flex-start; gap: 10px; padding: 8px 0; border-bottom: 1px solid var(--border); }
.act-row:last-child { border-bottom: none; }
.act-index {
  flex: none; width: 22px; height: 22px; border-radius: 50%;
  background: var(--primary-soft); color: var(--primary);
  font-size: 12px; font-weight: 600;
  display: inline-flex; align-items: center; justify-content: center;
}
.act-body { flex: 1; font-size: 13.5px; }

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
