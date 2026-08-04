<template>
  <div>
    <div class="page-title">记忆查看器</div>
    <div class="page-desc">按世界与实体浏览长期记忆</div>

    <div class="card">
      <div style="display:flex;gap:10px;align-items:center">
        <el-select v-model="world" placeholder="选择世界" style="width:220px" @change="loadEntities">
          <el-option v-for="w in worlds" :key="w.id" :label="w.id" :value="w.id" />
        </el-select>
        <el-select v-model="entity" placeholder="选择实体" style="width:180px">
          <el-option v-for="e in entities" :key="e" :label="e" :value="e" />
        </el-select>
        <el-button size="small" @click="loadMemory">查询</el-button>
      </div>
    </div>

    <div class="card">
      <div v-if="!queried" class="empty">选择世界与实体后查询</div>
      <div v-else-if="!entries.length" class="empty">暂无记忆</div>
      <template v-else>
        <div v-for="(m, i) in entries" :key="i" class="memory-item" :class="{ invalid: m.invalid }">
          <span class="imp">{{ m.importance }}</span>
          <span v-if="m.pinned" class="pin">📌</span>
          <span>{{ m.content }}</span>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { adminApi } from '../../api/admin'

const worlds = ref([])
const entities = ref([])
const world = ref('')
const entity = ref('')
const entries = ref([])
const queried = ref(false)

onMounted(async () => {
  worlds.value = await adminApi('/api/admin/worlds')
  if (worlds.value.length) {
    world.value = worlds.value[0].id
    await loadEntities(world.value)
  }
})

async function loadEntities(worldID) {
  const w = await adminApi('/api/admin/worlds/' + encodeURIComponent(worldID))
  entities.value = ['_world'].concat(Object.keys(w.characters || {}))
  entity.value = entities.value[0] || ''
}

async function loadMemory() {
  if (!world.value || !entity.value) return
  entries.value = await adminApi(`/api/admin/memory/${encodeURIComponent(world.value)}/${encodeURIComponent(entity.value)}`) || []
  queried.value = true
}
</script>

<style scoped>
.memory-item { padding: 9px 12px; border-bottom: 1px solid #f0f1f3; font-size: 13px; display: flex; gap: 10px; align-items: baseline; }
.memory-item:last-child { border-bottom: none; }
.memory-item .imp { flex: none; font-size: 11px; font-weight: 700; color: var(--primary); width: 22px; }
.memory-item.invalid { opacity: .45; text-decoration: line-through; }
.memory-item .pin { color: #e6a23c; font-size: 11px; flex: none; }
</style>
