<template>
  <div class="bg-gray-900 border border-gray-800 rounded-xl overflow-hidden">
    <div class="px-5 py-3 border-b border-gray-800 flex justify-between items-center">
      <span class="font-semibold text-gray-200">Runtime Stats</span>
      <button @click="load" class="text-xs text-gray-500 hover:text-indigo-400 transition">↻ Refresh</button>
    </div>
    <div class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-4 p-5">
      <div v-for="(v, k) in stats" :key="k" class="bg-gray-800 rounded-lg p-4">
        <p class="text-xs text-gray-500 uppercase tracking-wider font-medium">{{ k }}</p>
        <p class="text-xl font-bold text-indigo-300 mt-1">{{ v }}</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '@/api'
const stats = ref<Record<string, number>>({})
const load = async () => { stats.value = await api.stats() }
onMounted(load)
</script>
