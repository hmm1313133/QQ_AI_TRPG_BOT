import { createRouter, createWebHistory } from 'vue-router'
import ChatView from '../views/ChatView.vue'
import AdminLayout from '../views/admin/AdminLayout.vue'
import DashboardView from '../views/admin/DashboardView.vue'
import WorldsView from '../views/admin/WorldsView.vue'
import WorldEditor from '../views/admin/WorldEditor.vue'
import ConfigView from '../views/admin/ConfigView.vue'
import MemoryView from '../views/admin/MemoryView.vue'
import AssetsView from '../views/admin/AssetsView.vue'

const routes = [
  { path: '/', redirect: '/chat' },
  { path: '/chat', name: 'chat', component: ChatView },
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
      { path: 'memory', name: 'admin-memory', component: MemoryView },
    ],
  },
]

export default createRouter({
  history: createWebHistory('/'),
  routes,
})
