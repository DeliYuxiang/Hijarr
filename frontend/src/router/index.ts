import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import Dashboard from '@/views/Dashboard.vue'
import Config from '@/views/Config.vue'
import Jobs from '@/views/Jobs.vue'
import Stats from '@/views/Stats.vue'
import MediaLibrary from '@/views/MediaLibrary.vue'
import DBManager from '@/views/DBManager.vue'
import SubtitlePreferences from '@/views/SubtitlePreferences.vue'

const routes: RouteRecordRaw[] = [
  { path: '/',            name: 'Dashboard',   component: Dashboard },
  { path: '/config',      name: 'Config',      component: Config },
  { path: '/jobs',        name: 'Jobs',        component: Jobs },
  { path: '/stats',       name: 'Stats',       component: Stats },
  { path: '/media',       name: 'Media',       component: MediaLibrary },
  { path: '/preferences', name: 'Preferences', component: SubtitlePreferences },
  { path: '/db',          name: 'DB Manager',  component: DBManager },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

export default router
