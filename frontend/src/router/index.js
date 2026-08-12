import { createRouter, createWebHistory } from 'vue-router'
import ChatView from '../views/ChatView.vue'
import AdminLayout from '../views/admin/AdminLayout.vue'
import DashboardView from '../views/admin/DashboardView.vue'
import WorldsView from '../views/admin/WorldsView.vue'
import WorldEditor from '../views/admin/WorldEditor.vue'
import ConfigView from '../views/admin/ConfigView.vue'
import AssetsView from '../views/admin/AssetsView.vue'
import DirectorView from '../views/admin/DirectorView.vue'

const routes = [
  { path: '/', redirect: '/chat' },
  { path: '/chat', name: 'chat', component: ChatView },
  // 玩家侧世界状态编辑：复用 WorldEditor 的 player 模式（chat token 鉴权，无需 admin token）
  { path: '/world', name: 'player-world', component: WorldEditor, props: { player: true } },
  {
    path: '/admin',
    component: AdminLayout,
    redirect: '/admin/dashboard',
    children: [
      { path: 'dashboard', name: 'admin-dashboard', component: DashboardView },
      { path: 'worlds', name: 'admin-worlds', component: WorldsView },
      { path: 'worlds/:id', name: 'admin-world-editor', component: WorldEditor },
      { path: 'config', name: 'admin-config', component: ConfigView },
      { path: 'assets', name: 'admin-assets', component: AssetsView },
      { path: 'director', name: 'admin-director', component: DirectorView, meta: { title: '导演系统' } },
    ],
  },
]

export default createRouter({
  history: createWebHistory('/'),
  routes,
})
