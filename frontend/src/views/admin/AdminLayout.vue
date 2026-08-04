<template>
  <div class="admin-page">
    <nav class="nav">
      <div class="logo">AI <span>TRPG</span></div>
      <router-link
        v-for="item in navItems"
        :key="item.path"
        class="nav-item"
        :class="{ active: isActive(item.path) }"
        :to="item.path"
      >
        {{ item.icon }} <span>{{ item.label }}</span>
      </router-link>
      <div class="spacer"></div>
      <router-link class="chat-link" to="/chat">← 返回聊天</router-link>
    </nav>

    <div class="content">
      <router-view />
    </div>
  </div>
</template>

<script setup>
import { onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { getAdminToken } from '../../api/admin'

const route = useRoute()

const navItems = [
  { path: '/admin/dashboard', icon: '📊', label: '仪表盘' },
  { path: '/admin/bot', icon: '🤖', label: '机器人' },
  { path: '/admin/worlds', icon: '🌍', label: '世界管理' },
  { path: '/admin/scripts', icon: '📜', label: '剧本管理' },
  { path: '/admin/config', icon: '⚙️', label: 'AI 配置' },
  { path: '/admin/characters', icon: '🎭', label: '角色卡' },
  { path: '/admin/assets', icon: '🧩', label: '素材库' },
  { path: '/admin/memory', icon: '🧠', label: '记忆查看' },
  { path: '/admin/logs', icon: '💬', label: '聊天记录' },
]

function isActive(path) {
  return route.path === path
}

// 进入管理后台即确保已持有令牌（与原 admin.html 行为一致）
onMounted(() => getAdminToken())
</script>

<style scoped>
.admin-page { display: flex; min-height: 100vh; }

.nav {
  width: 208px; flex: none; background: var(--surface);
  border-right: 1px solid var(--border);
  padding: 20px 14px; display: flex; flex-direction: column; gap: 2px;
}
.nav .logo { font-size: 17px; font-weight: 700; padding: 4px 10px 18px; }
.nav .logo span { color: var(--primary); }
.nav-item {
  padding: 10px 12px; border-radius: 9px; font-size: 14px; cursor: pointer;
  color: var(--text-secondary); transition: all .15s; text-decoration: none;
  display: flex; align-items: center; gap: 9px;
}
.nav-item:hover { background: var(--bg); color: var(--text); }
.nav-item.active { background: var(--primary-soft); color: var(--primary); font-weight: 600; }
.nav .spacer { flex: 1; }
.nav a.chat-link {
  font-size: 13px; color: var(--text-secondary); text-decoration: none; padding: 10px 12px;
}
.nav a.chat-link:hover { color: var(--text); }

.content { flex: 1; padding: 26px 30px; overflow-y: auto; min-width: 0; }

@media (max-width: 860px) {
  .nav { width: 64px; }
  .nav .logo, .nav-item span, .nav a.chat-link { display: none; }
}
</style>
