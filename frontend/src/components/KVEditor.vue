<template>
  <div class="kv-editor">
    <div v-for="(row, i) in rows" :key="i" class="kv-row">
      <el-input v-model="row.key" placeholder="键" size="small" class="kv-key" @input="emitChange" />
      <el-input-number v-model="row.value" size="small" :controls="false" class="kv-value" @change="emitChange" />
      <el-button type="danger" plain size="small" circle @click="removeRow(i)">×</el-button>
    </div>
    <el-button size="small" plain @click="addRow">+ 添加</el-button>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'

// 可复用键值编辑器：行内增删，key 文本 + value 数字，双向绑定 map<string, number>
const props = defineProps({
  modelValue: { type: Object, default: () => ({}) },
})
const emit = defineEmits(['update:modelValue'])

const rows = ref([])
let lastEmitted = null

function toRows(obj) {
  return Object.entries(obj || {}).map(([key, value]) => ({ key, value }))
}

// 仅当外部（非本组件）更新 modelValue 时重建行，避免输入中被重建打断
watch(
  () => props.modelValue,
  (v) => {
    if (v === lastEmitted) return
    rows.value = toRows(v)
  },
  { immediate: true }
)

function emitChange() {
  const obj = {}
  for (const r of rows.value) {
    const k = (r.key || '').trim()
    if (k) obj[k] = r.value ?? 0
  }
  lastEmitted = obj
  emit('update:modelValue', obj)
}

function addRow() {
  rows.value.push({ key: '', value: 0 })
}

function removeRow(i) {
  rows.value.splice(i, 1)
  emitChange()
}
</script>

<style scoped>
.kv-editor { display: flex; flex-direction: column; gap: 8px; align-items: flex-start; }
.kv-row { display: flex; gap: 8px; align-items: center; width: 100%; }
.kv-key { max-width: 200px; }
.kv-value { width: 110px; }
</style>
