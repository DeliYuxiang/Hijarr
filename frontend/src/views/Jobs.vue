<template>
  <div class="bg-gray-900 border border-gray-800 rounded-xl overflow-hidden">
    <div class="px-5 py-3 border-b border-gray-800 flex justify-between items-center">
      <span class="font-semibold text-gray-200">Scheduled Jobs</span>
      <button @click="load" class="text-xs text-gray-500 hover:text-indigo-400 transition">↻ Refresh</button>
    </div>
    <table class="w-full text-sm">
      <thead class="bg-gray-800/50">
        <tr>
          <th class="px-5 py-2 text-left text-xs font-medium text-gray-500 uppercase">Job Name</th>
          <th class="px-5 py-2 text-left text-xs font-medium text-gray-500 uppercase">Interval</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-gray-800">
        <tr v-for="job in jobs" :key="job.name" class="hover:bg-gray-800/30 transition">
          <td class="px-5 py-2.5 text-gray-200">{{ job.name }}</td>
          <td class="px-5 py-2.5 text-indigo-400 font-mono text-xs">{{ job.interval }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '@/api'
const jobs = ref<{ name: string; interval: string }[]>([])
const load = async () => { const r = await api.jobs(); jobs.value = r.jobs }
onMounted(load)
</script>
