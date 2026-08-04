<template>
  <div class="tags-input">
    <el-tag
      v-for="(t, i) in modelValue"
      :key="i"
      closable
      size="small"
      class="tags-input-tag"
      :title="t"
      @close="removeAt(i)"
    >{{ t }}</el-tag>
    <input
      v-model="draft"
      class="tags-input-field"
      :placeholder="modelValue.length ? '' : placeholder"
      @keydown.enter.prevent="commit"
      @keydown="onKeydown"
      @blur="commit"
    />
  </div>
</template>

<script setup>
import { ref } from 'vue'

const props = defineProps({
  modelValue: { type: Array, default: () => [] },
  placeholder: { type: String, default: '输入后回车添加，支持逗号/顿号批量分隔' },
})
const emit = defineEmits(['update:modelValue'])

const draft = ref('')

// 支持中文逗号/英文逗号/顿号/分号/回车批量分隔（设计文档 §4.5）
const SEP = /[,，、;；\n]+/

function commit() {
  const parts = draft.value.split(SEP).map((s) => s.trim()).filter(Boolean)
  if (parts.length) {
    const next = [...props.modelValue]
    for (const p of parts) {
      if (!next.includes(p)) next.push(p)
    }
    emit('update:modelValue', next)
  }
  draft.value = ''
}

function removeAt(i) {
  const next = [...props.modelValue]
  next.splice(i, 1)
  emit('update:modelValue', next)
}

function onKeydown(e) {
  // 中文输入法组词期间 Enter 不触发提交
  if (e.keyCode === 229) return
  if (e.key === 'Backspace' && !draft.value && props.modelValue.length) {
    removeAt(props.modelValue.length - 1)
  }
}
</script>

<style scoped>
.tags-input {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  width: 100%;
  min-height: 32px;
  padding: 3px 8px;
  border: 1px solid var(--el-border-color, #dcdfe6);
  border-radius: 4px;
  background: var(--surface);
  cursor: text;
  transition: border-color .2s;
}
.tags-input:hover { border-color: var(--el-border-color-hover, #c0c4cc); }
.tags-input:focus-within { border-color: var(--primary); }
.tags-input-tag { max-width: 220px; }
.tags-input-field {
  flex: 1;
  min-width: 120px;
  border: none;
  outline: none;
  font-size: 13px;
  padding: 4px 0;
  background: transparent;
  color: var(--text);
}
</style>
