<template>
  <div class="space-y-4">
    <div class="grid grid-cols-3 gap-4">
      <div class="bg-gray-900 border border-gray-800 rounded-xl p-5">
        <p class="text-xs text-gray-500 uppercase tracking-wider font-medium mb-1">Uptime</p>
        <p class="text-2xl font-bold text-indigo-400">{{ status.uptime ?? '…' }}</p>
      </div>
      <div class="bg-gray-900 border border-gray-800 rounded-xl p-5">
        <p class="text-xs text-gray-500 uppercase tracking-wider font-medium mb-1">Version</p>
        <p class="text-2xl font-bold text-indigo-400">{{ status.version ?? '…' }}</p>
      </div>
      <div class="bg-gray-900 border border-gray-800 rounded-xl p-5">
        <p class="text-xs text-gray-500 uppercase tracking-wider font-medium mb-1">Start Time</p>
        <p class="text-base font-semibold text-gray-300">{{ status.start_time ?? '…' }}</p>
      </div>
    </div>
    <div class="bg-gray-900 border border-gray-800 rounded-xl p-5">
      <p class="text-xs text-gray-500 uppercase tracking-wider mb-3">Quick Links</p>
      <div class="flex flex-wrap gap-2">
        <RouterLink v-for="l in navLinks" :key="l.to" :to="l.to"
          class="text-xs bg-gray-800 hover:bg-indigo-700 text-gray-300 hover:text-white px-3 py-1.5 rounded-lg transition">
          {{ l.icon }} {{ l.label }}
        </RouterLink>
      </div>
    </div>
    <div class="flex justify-end">
      <button @click="refresh" class="text-xs text-gray-500 hover:text-gray-300 transition">↻ Refresh</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api'

const status = ref<any>({})

const navLinks = [
  { to: '/config',      icon: '⚙️',  label: 'Config' },
  { to: '/srn',         icon: '📡', label: 'SRN' },
  { to: '/db',          icon: '🗄️', label: 'DB' },
  { to: '/media',       icon: '🎬', label: 'Media' },
  { to: '/preferences', icon: '⭐', label: 'Preferences' },
]

const refresh = async () => {
  status.value = await api.status()
}

onMounted(refresh)
</script>
