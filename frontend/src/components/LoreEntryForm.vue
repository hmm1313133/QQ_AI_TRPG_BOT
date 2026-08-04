<template>
  <div class="entry-form">
    <!-- 设计文档 §3.1 红线：状态与设定分离 -->
    <el-alert
      type="info"
      :closable="false"
      class="form-tip"
      title="只写静态设定（地理/势力/规则/历史）；HP、任务进度、关系值等易变事实由系统自动管理，不要写进条目。"
    />

    <el-form label-width="90px" label-position="left">
      <el-form-item label="标题" required>
        <el-input v-model="entry.title" placeholder="卡片名，仅管理用（如：北境·寒鸦堡）" style="max-width:420px" />
        <el-tag v-if="entry.source === 'script'" size="small" type="warning" style="margin-left:8px">来源：剧本</el-tag>
        <el-tag v-else-if="entry.source === 'system'" size="small" type="info" style="margin-left:8px">来源：系统</el-tag>
      </el-form-item>

      <el-form-item label="分类">
        <el-select v-model="entry.category" style="width:200px" @change="onCategoryChange">
          <el-option v-for="c in LORE_CATEGORIES" :key="c.value" :label="c.label" :value="c.value" />
        </el-select>
      </el-form-item>

      <el-form-item label="关键词">
        <div style="width:100%">
          <TagsInput v-model="entry.keys" placeholder="触发词，命中即注入；回车添加，支持逗号/顿号批量分隔" />
          <div v-if="shortKeys.length" class="warn-text">
            ⚠ 关键词「{{ shortKeys.join('、') }}」不足 2 字，子串匹配容易误命中，建议加长或删除
          </div>
        </div>
      </el-form-item>

      <el-form-item label="关联词">
        <div style="width:100%">
          <TagsInput v-model="entry.secondary_keys" placeholder="次键（可选），与关键词组合判断" />
          <el-radio-group v-if="entry.secondary_keys.length" v-model="entry.secondary_mode" size="small" style="margin-top:6px">
            <el-radio value="and_any">and_any 任一次键出现才注入</el-radio>
            <el-radio value="and_all">and_all 全部次键出现才注入</el-radio>
            <el-radio value="not_any">not_any 次键出现则排除</el-radio>
          </el-radio-group>
        </div>
      </el-form-item>

      <el-form-item label="恒定">
        <el-switch v-model="entry.constant" />
        <span class="muted" style="margin-left:8px">无视关键词永远注入（只放最核心设定）</span>
        <div v-if="entry.constant && constantTotal > 3" class="warn-text">
          ⚠ 当前世界已有 {{ constantTotal }} 条恒定条目，建议不超过 3 条（世界观一句话、主线一句话、语气一条）
        </div>
      </el-form-item>

      <el-form-item label="插入位置">
        <el-radio-group v-model="entry.position">
          <el-radio value="front">前部（世界观区）</el-radio>
          <el-radio value="tail">尾部（风格指令区）</el-radio>
        </el-radio-group>
      </el-form-item>

      <el-form-item label="优先级">
        <div class="priority-row">
          <el-slider v-model="entry.priority" :min="0" :max="100" style="flex:1" />
          <span class="mono" style="width:32px;text-align:right">{{ entry.priority }}</span>
        </div>
        <div class="muted">预算不足时低优先级先被裁</div>
      </el-form-item>

      <el-form-item label="启用">
        <el-switch v-model="entry.enabled" />
        <span class="muted" style="margin-left:8px">停用的条目不进检索</span>
      </el-form-item>

      <el-form-item label="内容" required>
        <el-input v-model="entry.content" type="textarea" :rows="8" placeholder="设定正文" />
      </el-form-item>
    </el-form>

    <div class="form-actions">
      <el-button v-if="!isNew" type="danger" plain size="small" @click="$emit('remove')">删除条目</el-button>
      <span class="spacer"></span>
      <el-button size="small" @click="$emit('cancel')">取消</el-button>
      <el-button type="primary" size="small" :loading="saving" @click="onSave">{{ isNew ? '创建条目' : '保存' }}</el-button>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { ElMessage } from 'element-plus'
import TagsInput from './TagsInput.vue'
import { LORE_CATEGORIES } from './loreMeta'

const props = defineProps({
  entry: { type: Object, required: true }, // 草稿对象，由父组件持有，本表单直接编辑其字段
  saving: { type: Boolean, default: false },
  isNew: { type: Boolean, default: false },
  constantTotal: { type: Number, default: 0 }, // 世界当前恒定条目总数（>3 提示）
})
const emit = defineEmits(['save', 'remove', 'cancel'])

const shortKeys = computed(() =>
  (props.entry.keys || []).filter((k) => [...k].length < 2)
)

// 风格条目（文风/描写修饰）适合近底部注入，选中时默认切到 tail
function onCategoryChange(v) {
  if (v === 'style') {
    props.entry.position = 'tail'
  }
}

function onSave() {
  const e = props.entry
  if (!e.title.trim()) { ElMessage.warning('标题不能为空'); return }
  if (!e.content.trim()) { ElMessage.warning('内容不能为空'); return }
  if (!e.constant && !(e.keys || []).length) { ElMessage.warning('非恒定条目至少需要 1 个关键词'); return }
  emit('save')
}
</script>

<style scoped>
.form-tip { margin-bottom: 16px; }
.warn-text { color: var(--el-color-warning, #e6a23c); font-size: 12.5px; margin-top: 4px; line-height: 1.6; }
.priority-row { display: flex; align-items: center; gap: 12px; width: 100%; max-width: 420px; }
.form-actions { display: flex; align-items: center; gap: 10px; margin-top: 4px; }
.spacer { flex: 1; }
</style>
