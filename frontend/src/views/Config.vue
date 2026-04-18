<template>
  <div class="bg-gray-900 border border-gray-800 rounded-xl overflow-hidden">
    <div class="px-5 py-3 border-b border-gray-800 flex justify-between items-center">
      <span class="font-semibold text-gray-200">Runtime Config</span>
      <button @click="load" class="text-xs text-gray-500 hover:text-indigo-400 transition">↻ Refresh</button>
    </div>
    <table class="w-full text-sm">
      <thead class="bg-gray-800/50">
        <tr>
          <th class="px-5 py-2 text-left text-xs font-medium text-gray-500 uppercase">Key</th>
          <th class="px-5 py-2 text-left text-xs font-medium text-gray-500 uppercase">Value</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-gray-800">
        <tr v-for="(v, k) in config" :key="k" class="hover:bg-gray-800/30 transition">
          <td class="px-5 py-2.5 text-gray-400 font-mono text-xs">{{ k }}</td>
          <td class="px-5 py-2.5 text-gray-200 break-all">{{ v || '—' }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '@/api'
const config = ref<Record<string, string>>({})
const load = async () => { config.value = await api.config() }
onMounted(load)
</script>
