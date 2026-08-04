<template>
  <div>
    <div class="page-title">聊天记录</div>
    <div class="page-desc">跑团日志（.log start 开始记录的会话）</div>

    <div class="card">
      <div style="display:flex;gap:10px;align-items:center">
        <el-select v-model="sessionID" placeholder="选择会话" style="width:260px">
          <el-option v-for="s in sessions" :key="s" :label="s" :value="s" />
        </el-select>
        <el-button size="small" @click="loadLogs">加载</el-button>
      </div>
    </div>

    <div class="card">
      <div v-if="!queried" class="empty">选择会话后加载</div>
      <div v-else-if="!entries.length" class="empty">该会话暂无日志</div>
      <template v-else>
        <div v-for="(e, i) in entries" :key="i" class="log-item">
          <el-tag :type="e.role === 'user' ? 'info' : 'success'" size="small">{{ e.role }}</el-tag>
          <span class="muted">{{ e.timestamp || '' }}</span>
          <span>{{ e.content }}</span>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { adminApi } from '../../api/admin'

const sessions = ref([])
const sessionID = ref('')
const entries = ref([])
const queried = ref(false)

onMounted(async () => {
  sessions.value = await adminApi('/api/admin/logs') || []
})

async function loadLogs() {
  if (!sessionID.value) return
  entries.value = await adminApi('/api/admin/logs/' + encodeURIComponent(sessionID.value)) || []
  queried.value = true
}
</script>

<style scoped>
.log-item { padding: 9px 12px; border-bottom: 1px solid #f0f1f3; font-size: 13px; display: flex; gap: 10px; align-items: baseline; }
.log-item:last-child { border-bottom: none; }
</style>
