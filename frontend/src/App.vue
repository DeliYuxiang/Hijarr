<template>
  <div class="flex h-screen bg-gray-950 text-gray-100 font-sans overflow-hidden">
    <!-- Sidebar -->
    <aside class="w-60 flex-shrink-0 flex flex-col bg-gray-900 border-r border-gray-800">
      <div class="px-5 py-4 border-b border-gray-800">
        <span class="text-lg font-bold tracking-tight text-indigo-400">⚡ Hijarr</span>
      </div>
      <nav class="flex-1 p-3 space-y-1 overflow-y-auto">
        <RouterLink
          v-for="link in navLinks"
          :key="link.to"
          :to="link.to"
          class="flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors"
          :class="[$route.path === link.to
            ? 'bg-indigo-600 text-white font-medium'
            : 'text-gray-400 hover:bg-gray-800 hover:text-gray-200']"
        >
          <span class="text-base">{{ link.icon }}</span>
          <span>{{ link.label }}</span>
        </RouterLink>
      </nav>
      <div class="px-4 py-3 border-t border-gray-800 text-xs text-gray-600">
        hijarr admin
      </div>
    </aside>

    <!-- Main content -->
    <main class="flex-1 flex flex-col overflow-hidden">
      <header class="flex-shrink-0 h-14 bg-gray-900 border-b border-gray-800 flex items-center px-6">
        <h1 class="text-base font-semibold capitalize text-gray-200">
          {{ currentTitle }}
        </h1>
      </header>
      <div class="flex-1 overflow-y-auto p-6">
        <RouterView />
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, RouterLink, RouterView } from 'vue-router'

const route = useRoute()

const navLinks = [
  { to: '/',            icon: '🏠', label: 'Dashboard' },
  { to: '/config',      icon: '⚙️',  label: 'Config' },
  { to: '/jobs',        icon: '⏰', label: 'Jobs' },
  { to: '/stats',       icon: '📊', label: 'Stats' },
  { to: '/media',       icon: '🎬', label: 'Media Library' },
  { to: '/preferences', icon: '⭐', label: 'Preferences' },
  { to: '/db',          icon: '🗄️', label: 'DB Manager' },
]

const currentTitle = computed(() =>
  navLinks.find(l => l.to === route.path)?.label ?? route.path.slice(1)
)
</script>
