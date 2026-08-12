<template>
  <div>
    <div class="page-title">导演系统</div>
    <div class="page-desc">Director-Planner 运行记录：每轮叙事指导与场景计划刷新</div>

    <div class="card">
      <div class="toolbar">
        <el-select v-model="filter.sessionId" size="small" style="width:220px" @change="loadLogs">
          <el-option label="全部会话" value="" />
          <el-option v-for="s in sessions" :key="s.id" :label="s.world_name || s.id" :value="s.id" />
        </el-select>
        <el-select v-model="filter.limit" size="small" style="width:110px" @change="loadLogs">
          <el-option label="50 条" :value="50" />
          <el-option label="100 条" :value="100" />
        </el-select>
        <el-button size="small" @click="loadLogs">刷新</el-button>
        <span class="spacer"></span>
        <span class="muted">自动刷新（10s）</span>
        <el-switch v-model="autoRefresh" size="small" />
      </div>

      <div class="stat-grid">
        <div class="stat"><div class="num">{{ stats.total }}</div><div class="label">记录总数</div></div>
        <div class="stat"><div class="num">{{ stats.ai }}</div><div class="label">AI 指导数</div></div>
        <div class="stat"><div class="num">{{ stats.fallback }}</div><div class="label">规则兜底数</div></div>
        <div class="stat"><div class="num">{{ stats.hitRate }}</div><div class="label">状态更新命中率</div></div>
        <div class="stat"><div class="num">{{ stats.avgMs }}</div><div class="label">平均耗时 (ms)</div></div>
      </div>
    </div>

    <div class="card">
      <div class="card-title">运行记录</div>
      <div v-if="loading" class="empty">加载中…</div>
      <el-table v-else :data="logs" size="small" empty-text="暂无导演系统运行记录（进入世界跑团后产生）">
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="expand-box">
              <template v-if="row.kind === 'guidance'">
                <div class="expand-title">关注点（focus）</div>
                <div v-if="(row.focus || []).length" class="tag-list">
                  <el-tag v-for="(f, i) in row.focus" :key="i" size="small" effect="plain" :title="f">{{ f }}</el-tag>
                </div>
                <div v-else class="muted">无</div>
                <div class="expand-title">约束（constraints）</div>
                <div v-if="(row.constraints || []).length" class="tag-list">
                  <el-tag v-for="(c, i) in row.constraints" :key="i" size="small" type="info" effect="plain" :title="c">{{ c }}</el-tag>
                </div>
                <div v-else class="muted">无</div>
              </template>
              <template v-if="(row.state_updates || []).length">
                <div class="expand-title">状态更新明细</div>
                <el-table :data="row.state_updates" size="small">
                  <el-table-column prop="type" label="类型" width="180" />
                  <el-table-column prop="target" label="目标" min-width="160" show-overflow-tooltip />
                  <el-table-column prop="value" label="值" min-width="120" show-overflow-tooltip />
                </el-table>
              </template>
              <template v-if="row.scene_plan">
                <div class="expand-title">场景计划（scene_plan）</div>
                <pre class="mono plan">{{ row.scene_plan }}</pre>
              </template>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="时间" width="100">
          <template #default="{ row }"><span class="mono">{{ fmtTime(row.time) }}</span></template>
        </el-table-column>
        <el-table-column label="会话" min-width="140">
          <template #default="{ row }"><span class="mono">{{ sessionName(row.session_id) }}</span></template>
        </el-table-column>
        <el-table-column prop="round" label="轮次" width="70" />
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.kind === 'guidance'" size="small">叙事指导</el-tag>
            <el-tag v-else size="small" effect="dark" color="#8b5cf6" style="border-color:#8b5cf6">场景计划</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="来源" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.source === 'ai'" type="success" size="small">AI 生成</el-tag>
            <el-tag v-else-if="row.source === 'fallback'" type="warning" size="small">降级规则</el-tag>
            <el-tag v-else type="info" size="small">纯规则</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="基调/节奏" min-width="110">
          <template #default="{ row }">
            <span v-if="row.kind === 'guidance'">{{ row.tone || '-' }} / {{ row.pacing || '-' }}</span>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="耗时" width="90">
          <template #default="{ row }">{{ row.elapsed_ms }} ms</template>
        </el-table-column>
        <el-table-column label="状态更新" width="110">
          <template #default="{ row }">
            <el-tooltip v-if="(row.state_updates || []).length" placement="top">
              <template #content>
                <div v-for="(u, i) in row.state_updates" :key="i">{{ u.type }} → {{ u.target }} = {{ u.value }}</div>
              </template>
              <span>命中 {{ row.applied_updates }}/{{ row.state_updates.length }}</span>
            </el-tooltip>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="错误" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">
            <span v-if="row.error" class="err">{{ row.error }}</span>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { adminApi, directorApi } from '../../api/admin'

const logs = ref([])
const sessions = ref([])
const loading = ref(false)
const autoRefresh = ref(false)
const filter = ref({ sessionId: '', limit: 50 })

const stats = computed(() => {
  const list = logs.value
  const total = list.length
  const ai = list.filter((l) => l.source === 'ai').length
  const fallback = list.filter((l) => l.source === 'fallback' || l.source === 'rule').length
  const updTotal = list.reduce((n, l) => n + (l.state_updates || []).length, 0)
  const updHit = list.reduce((n, l) => n + (l.applied_updates || 0), 0)
  const hitRate = updTotal ? Math.round((updHit / updTotal) * 100) + '%' : '-'
  const avgMs = total ? Math.round(list.reduce((n, l) => n + (l.elapsed_ms || 0), 0) / total) : '-'
  return { total, ai, fallback, hitRate, avgMs }
})

function fmtTime(t) {
  if (!t) return '-'
  const d = new Date(t)
  if (isNaN(d)) return t
  const p = (n) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

// 会话 id -> 世界显示名（无名称回退 id）
const sessionNameMap = computed(() => {
  const m = {}
  for (const s of sessions.value) m[s.id] = s.world_name || ''
  return m
})
function sessionName(id) {
  return sessionNameMap.value[id] || id
}

async function loadLogs() {
  loading.value = true
  try {
    logs.value = (await directorApi.logs({ session_id: filter.value.sessionId, limit: filter.value.limit })) || []
  } finally {
    loading.value = false
  }
}

let timer = null
function stopTimer() {
  if (timer) { clearInterval(timer); timer = null }
}
watch(autoRefresh, (on) => {
  stopTimer()
  if (on) timer = setInterval(loadLogs, 10000)
})

onMounted(async () => {
  sessions.value = (await adminApi('/api/admin/sessions')) || []
  loadLogs()
})
onUnmounted(stopTimer)
</script>

<style scoped>
.toolbar { display: flex; align-items: center; gap: 10px; margin-bottom: 16px; flex-wrap: wrap; }
.toolbar .spacer { flex: 1; }
.stat-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: 14px; }
.stat {
  background: var(--bg); border: 1px solid var(--border);
  border-radius: 12px; padding: 14px 16px;
}
.stat .num { font-size: 24px; font-weight: 700; color: var(--primary); }
.stat .label { font-size: 12.5px; color: var(--text-secondary); margin-top: 4px; }
.err { color: var(--el-color-danger); font-size: 12.5px; }
.expand-box { padding: 10px 24px 16px 48px; }
.expand-title { font-size: 13px; font-weight: 600; margin: 10px 0 6px; }
.tag-list { display: flex; flex-wrap: wrap; gap: 6px; }
.plan {
  background: var(--bg); border: 1px solid var(--border); border-radius: 8px;
  padding: 12px 14px; white-space: pre-wrap; word-break: break-all; margin: 0;
}
</style>
