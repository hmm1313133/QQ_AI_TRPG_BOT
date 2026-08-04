<template>
  <div>
    <div class="page-title">机器人</div>
    <div class="page-desc">QQ 机器人连接状态与生命周期控制（每 5 秒自动刷新）</div>

    <div v-if="unavailable" class="card">
      <div class="empty">QQ 机器人未接入（后端未注入 Bot 控制器）</div>
    </div>

    <template v-else>
      <div class="card">
        <div class="card-title">
          连接状态
          <span class="dot" :class="{ on: status.connected }"></span>
          <el-tag :type="status.connected ? 'success' : 'info'" size="small">
            {{ status.connected ? '已连接' : '未连接' }}
          </el-tag>
          <el-tag :type="status.running ? 'success' : 'warning'" size="small" style="margin-left:6px">
            {{ status.running ? '运行中' : '已停止' }}
          </el-tag>
        </div>
        <div class="stat-grid">
          <div class="stat"><div class="num sm mono">{{ status.app_id || '-' }}</div><div class="label">AppID</div></div>
          <div class="stat"><div class="num sm">{{ status.uptime || '-' }}</div><div class="label">运行时长</div></div>
          <div class="stat"><div class="num sm">{{ status.rx_count ?? 0 }}</div><div class="label">累计接收</div></div>
          <div class="stat"><div class="num sm">{{ status.tx_count ?? 0 }}</div><div class="label">累计发送</div></div>
          <div class="stat"><div class="num sm">{{ status.reconnect_count ?? 0 }}</div><div class="label">断线重连</div></div>
          <div class="stat"><div class="num sm">{{ lastConnected }}</div><div class="label">最近连接时间</div></div>
        </div>
        <div class="muted" style="margin-top:10px">启动时间：{{ status.started_at || '-' }}</div>
      </div>

      <div class="card">
        <div class="card-title">生命周期控制</div>
        <div style="display:flex;gap:10px;flex-wrap:wrap">
          <el-button type="primary" :loading="acting" :disabled="status.running" @click="lifecycle('start', '启动')">启动</el-button>
          <el-button type="warning" :loading="acting" :disabled="!status.running" @click="lifecycle('stop', '停止')">停止</el-button>
          <el-button type="danger" plain :loading="acting" :disabled="!status.running" @click="lifecycle('restart', '重启')">重启</el-button>
        </div>
        <div class="muted" style="margin-top:10px">
          修改 QQ 凭证（<router-link to="/admin/config">AI 配置 → QQ 机器人分组</router-link>）后，点「重启」即生效。
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { adminReq } from '../../api/admin'

const status = ref({})
const unavailable = ref(false)
const acting = ref(false)
let timer = null

const lastConnected = computed(() => {
  const t = status.value.last_connected_at
  if (!t || t.startsWith('0001-')) return '-'
  return new Date(t).toLocaleString()
})

async function refresh() {
  try {
    status.value = await adminReq('/api/admin/bot')
    unavailable.value = false
  } catch (e) {
    if (e.status === 503) {
      unavailable.value = true
    }
    // 其它错误（如网络抖动）静默跳过，下轮再试
  }
}

async function lifecycle(action, label) {
  try {
    await ElMessageBox.confirm(`确认${label} QQ 机器人？`, '提示', { type: 'warning' })
  } catch {
    return
  }
  acting.value = true
  try {
    const r = await adminReq(`/api/admin/bot/${action}`, { method: 'POST' })
    ElMessage.success(r.message || `已${label}`)
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    acting.value = false
  }
  refresh()
}

onMounted(() => {
  refresh()
  timer = setInterval(refresh, 5000)
})
onUnmounted(() => clearInterval(timer))
</script>

<style scoped>
.stat-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(170px, 1fr)); gap: 14px; }
.stat {
  background: var(--bg); border: 1px solid var(--border);
  border-radius: 10px; padding: 14px;
}
.stat .num { font-size: 22px; font-weight: 700; color: var(--primary); }
.stat .num.sm { font-size: 16px; word-break: break-all; }
.stat .label { font-size: 12.5px; color: var(--text-secondary); margin-top: 4px; }
.dot {
  display: inline-block; width: 9px; height: 9px; border-radius: 50%;
  background: #c0c4cc; margin: 0 4px 0 10px; vertical-align: middle;
}
.dot.on { background: #22c55e; box-shadow: 0 0 0 3px rgba(34, 197, 94, .18); }
</style>
