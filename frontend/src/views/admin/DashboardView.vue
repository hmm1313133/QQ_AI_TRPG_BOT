<template>
  <div>
    <div class="page-title">仪表盘</div>
    <div class="page-desc">系统运行状态总览</div>

    <div class="stat-grid">
      <div class="stat"><div class="num">{{ status.version ?? '-' }}</div><div class="label">版本</div></div>
      <div class="stat"><div class="num">{{ status.uptime ?? '-' }}</div><div class="label">运行时长</div></div>
      <div class="stat"><div class="num">{{ status.sessionCount ?? '-' }}</div><div class="label">活跃会话</div></div>
    </div>

    <div class="card section-gap">
      <div class="card-title">活跃会话</div>
      <el-table :data="sessions" size="default" empty-text="暂无会话">
        <el-table-column prop="id" label="会话">
          <template #default="{ row }">
            <div><b>{{ row.world_name || row.id }}</b></div>
            <div v-if="row.world_name" class="mono muted">{{ row.id }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="mode" label="模式" />
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { adminApi } from '../../api/admin'

const status = ref({})
const sessions = ref([])

onMounted(async () => {
  status.value = await adminApi('/api/admin/status')
  sessions.value = (await adminApi('/api/admin/sessions')) || []
})
</script>

<style scoped>
.stat-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 14px; }
.stat {
  background: var(--surface); border: 1px solid var(--border);
  border-radius: 12px; padding: 18px; box-shadow: var(--shadow);
}
.stat .num { font-size: 26px; font-weight: 700; color: var(--primary); }
.stat .label { font-size: 12.5px; color: var(--text-secondary); margin-top: 4px; }
.section-gap { margin-top: 16px; }
</style>
