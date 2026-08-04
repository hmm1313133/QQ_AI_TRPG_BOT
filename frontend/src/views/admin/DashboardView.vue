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
      <div class="card-title">
        QQ 渠道
        <router-link to="/admin/bot" class="more-link">详情 →</router-link>
      </div>
      <div v-if="botUnavailable" class="muted">QQ 机器人未接入</div>
      <div v-else class="bot-row">
        <span class="dot" :class="{ on: bot.connected }"></span>
        <el-tag :type="bot.connected ? 'success' : 'info'" size="small">
          {{ bot.connected ? '已连接' : '未连接' }}
        </el-tag>
        <el-tag :type="bot.running ? 'success' : 'warning'" size="small">
          {{ bot.running ? '运行中' : '已停止' }}
        </el-tag>
        <span class="muted">收 {{ bot.rx_count ?? 0 }} / 发 {{ bot.tx_count ?? 0 }} · 重连 {{ bot.reconnect_count ?? 0 }} 次</span>
      </div>
    </div>

    <div class="card section-gap">
      <div class="card-title">活跃会话</div>
      <el-table :data="sessions" size="default" empty-text="暂无会话">
        <el-table-column prop="id" label="会话">
          <template #default="{ row }"><span class="mono">{{ row.id }}</span></template>
        </el-table-column>
        <el-table-column prop="mode" label="模式" />
        <el-table-column label="剧本">
          <template #default="{ row }">{{ row.script || '-' }}</template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { adminApi, adminReq } from '../../api/admin'

const status = ref({})
const sessions = ref([])
const bot = ref({})
const botUnavailable = ref(false)

onMounted(async () => {
  status.value = await adminApi('/api/admin/status')
  sessions.value = (await adminApi('/api/admin/sessions')) || []
  try {
    bot.value = await adminReq('/api/admin/bot')
  } catch {
    botUnavailable.value = true
  }
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
.more-link { font-size: 12.5px; font-weight: 400; color: var(--primary); text-decoration: none; margin-left: 10px; }
.bot-row { display: flex; align-items: center; gap: 10px; }
.dot {
  display: inline-block; width: 9px; height: 9px; border-radius: 50%;
  background: #c0c4cc;
}
.dot.on { background: #22c55e; box-shadow: 0 0 0 3px rgba(34, 197, 94, .18); }
</style>
