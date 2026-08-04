<template>
  <div class="char-form">
    <!-- 基本信息 -->
    <div class="form-section">
      <div class="section-title">基本信息</div>
      <el-form label-width="90px" size="small">
        <div class="form-grid">
          <el-form-item label="姓名" required>
            <el-input v-model="c.name" placeholder="角色名（世界内唯一）" />
          </el-form-item>
          <el-form-item label="类型">
            <el-select v-model="c.kind">
              <el-option label="npc" value="npc" />
              <el-option label="pc" value="pc" />
            </el-select>
          </el-form-item>
          <el-form-item label="立场">
            <el-select v-model="c.disposition">
              <el-option label="friendly 友善" value="friendly" />
              <el-option label="neutral 中立" value="neutral" />
              <el-option label="suspicious 警惕" value="suspicious" />
              <el-option label="hostile 敌对" value="hostile" />
              <el-option label="dead 死亡" value="dead" />
            </el-select>
          </el-form-item>
          <el-form-item label="所在地点">
            <el-input v-model="c.location" placeholder="可留空" />
          </el-form-item>
          <el-form-item label="角色定位">
            <el-input v-model="c.role" placeholder="role，如 酒馆老板" />
          </el-form-item>
          <el-form-item label="人物卡">
            <el-input v-model="c.card_ref" readonly placeholder="未关联" class="mono">
              <template v-if="c.card_ref" #append>已关联</template>
            </el-input>
          </el-form-item>
          <el-form-item label="存活">
            <el-checkbox v-model="c.alive">存活</el-checkbox>
          </el-form-item>
        </div>
      </el-form>
    </div>

    <el-divider />

    <!-- 形象与性格 -->
    <div class="form-section">
      <div class="section-title">形象与性格</div>
      <el-form label-width="90px" size="small">
        <el-form-item label="外貌">
          <el-input v-model="c.appearance" type="textarea" :rows="2" placeholder="外貌描写" />
        </el-form-item>
        <el-form-item label="性格">
          <el-input v-model="c.personality" type="textarea" :rows="2" placeholder="性格描述（长文）" />
        </el-form-item>
        <el-form-item label="特质标签">
          <TagsInput v-model="c.traits" placeholder="性格特质（记仇/胆小/贪婪…）" />
        </el-form-item>
        <el-form-item label="对话风格">
          <el-input v-model="c.dialogue_style" placeholder="如 说话简短、爱用比喻" />
        </el-form-item>
        <el-form-item label="关键台词">
          <TagsInput v-model="c.key_dialogue" placeholder="角色的标志性台词" />
        </el-form-item>
      </el-form>
    </div>

    <el-divider />

    <!-- 背景与能力 -->
    <div class="form-section">
      <div class="section-title">背景与能力</div>
      <el-form label-width="90px" size="small">
        <el-form-item label="背景故事">
          <el-input v-model="c.backstory" type="textarea" :rows="3" placeholder="背景故事" />
        </el-form-item>
        <el-form-item label="能力特长">
          <TagsInput v-model="c.skills" placeholder="如 剑术(娴熟)、潜行" />
        </el-form-item>
      </el-form>
    </div>

    <template v-if="!compact">
      <el-divider />

      <!-- 状态与扮演 -->
      <div class="form-section">
        <div class="section-title">状态与扮演</div>
        <el-form label-width="90px" size="small">
          <div class="form-grid">
            <el-form-item label="当前行动">
              <el-input v-model="c.current_action" placeholder="current_action" />
            </el-form-item>
            <el-form-item label="动机">
              <el-input v-model="c.motivation" placeholder="motivation" />
            </el-form-item>
          </div>
          <el-form-item label="秘密">
            <el-input v-model="c.secrets" placeholder="secrets（不会主动透露）" />
          </el-form-item>
          <el-form-item label="目标">
            <div class="goal-list">
              <div v-for="(g, gi) in c.goals" :key="gi" class="goal-row">
                <el-input v-model="g.description" size="small" placeholder="目标描述" style="flex:1" />
                <el-input-number v-model="g.priority" :min="1" :max="10" size="small" style="width:110px" title="优先级 1-10" />
                <el-input-number v-model="g.progress" :min="0" :max="100" size="small" style="width:110px" title="进度 0-100" />
                <el-button type="danger" plain size="small" circle @click="c.goals.splice(gi, 1)">×</el-button>
              </div>
              <el-button size="small" plain @click="c.goals.push({ description: '', priority: 5, progress: 0 })">+ 添加目标</el-button>
            </div>
          </el-form-item>
          <el-form-item label="心情">
            <div class="mood-row">
              <span class="muted">愉悦度</span>
              <el-input-number v-model="c.mood.valence" :min="-100" :max="100" size="small" />
              <span class="muted">激活度</span>
              <el-input-number v-model="c.mood.arousal" :min="0" :max="100" size="small" />
            </div>
          </el-form-item>
          <el-form-item label="备注">
            <el-input v-model="c.notes" type="textarea" :rows="2" placeholder="KP 备注" />
          </el-form-item>
        </el-form>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, onMounted } from 'vue'
import TagsInput from './TagsInput.vue'

const props = defineProps({
  modelValue: { type: Object, required: true },
  // 精简版：隐藏「状态与扮演」区块（素材库编辑等场景）
  compact: { type: Boolean, default: false },
})
defineEmits(['update:modelValue'])

const c = computed(() => props.modelValue)

// 补齐缺省字段，保证各控件可安全双向绑定
function ensureDefaults() {
  const v = props.modelValue
  if (!v.name) v.name = ''
  if (!v.kind) v.kind = 'npc'
  if (v.alive === undefined || v.alive === null) v.alive = true
  if (!v.disposition) v.disposition = 'neutral'
  for (const k of ['role', 'card_ref', 'location', 'appearance', 'personality', 'dialogue_style',
    'backstory', 'current_action', 'motivation', 'secrets', 'notes']) {
    if (typeof v[k] !== 'string') v[k] = ''
  }
  for (const k of ['traits', 'key_dialogue', 'skills']) {
    if (!Array.isArray(v[k])) v[k] = []
  }
  if (!Array.isArray(v.goals)) v.goals = []
  if (!v.mood || typeof v.mood !== 'object') v.mood = { valence: 0, arousal: 0, tags: [], updated_at: 0 }
}

onMounted(ensureDefaults)
</script>

<style scoped>
.char-form :deep(.el-divider) { margin: 14px 0; }
.section-title { font-size: 13px; font-weight: 600; color: var(--text-secondary); margin-bottom: 10px; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0 16px; }
.goal-list { display: flex; flex-direction: column; gap: 6px; align-items: flex-start; width: 100%; }
.goal-row { display: flex; gap: 8px; align-items: center; width: 100%; }
.mood-row { display: flex; align-items: center; gap: 8px; }
.muted { color: var(--text-secondary); font-size: 12.5px; }
.mono { font-family: ui-monospace, "Cascadia Code", Consolas, monospace; }

@media (max-width: 900px) {
  .form-grid { grid-template-columns: 1fr; }
}
</style>
