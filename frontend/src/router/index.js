import { createRouter, createWebHistory } from 'vue-router'
import ChatView from '../views/ChatView.vue'
import AdminLayout from '../views/admin/AdminLayout.vue'
import DashboardView from '../views/admin/DashboardView.vue'
import BotView from '../views/admin/BotView.vue'
import WorldsView from '../views/admin/WorldsView.vue'
import WorldEditor from '../views/admin/WorldEditor.vue'
import ScriptsView from '../views/admin/ScriptsView.vue'
import ConfigView from '../views/admin/ConfigView.vue'
import CharactersView from '../views/admin/CharactersView.vue'
import MemoryView from '../views/admin/MemoryView.vue'
import LogsView from '../views/admin/LogsView.vue'

const routes = [
  { path: '/', redirect: '/chat' },
  { path: '/chat', name: 'chat', component: ChatView },
  {
    path: '/admin',
    component: AdminLayout,
    redirect: '/admin/dashboard',
    children: [
      { path: 'dashboard', name: 'admin-dashboard', component: DashboardView },
      { path: 'bot', name: 'admin-bot', component: BotView },
      { path: 'worlds', name: 'admin-worlds', component: WorldsView },
      { path: 'worlds/:id', name: 'admin-world-editor', component: WorldEditor },
      { path: 'scripts', name: 'admin-scripts', component: ScriptsView },
      { path: 'config', name: 'admin-config', component: ConfigView },
      { path: 'characters', name: 'admin-characters', component: CharactersView },
      { path: 'memory', name: 'admin-memory', component: MemoryView },
      { path: 'logs', name: 'admin-logs', component: LogsView },
    ],
  },
]

export default createRouter({
  history: createWebHistory('/'),
  routes,
})
