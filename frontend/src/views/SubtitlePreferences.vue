<template>
  <div class="space-y-6">
    <div class="flex justify-between items-center">
      <button @click="load" :disabled="loading"
        class="bg-indigo-600 hover:bg-indigo-700 text-white px-4 py-2 rounded-lg text-sm transition disabled:opacity-50">
        {{ loading ? 'Loading…' : 'Refresh' }}
      </button>
    </div>

    <!-- Blacklist -->
    <div class="bg-gray-900 border border-gray-800 rounded-xl overflow-hidden">
      <div class="px-5 py-3 border-b border-gray-800 flex items-center justify-between">
        <h2 class="text-sm font-semibold text-gray-200">Blacklisted Subtitles</h2>
        <span class="text-xs text-gray-500">{{ blacklist.length }} entries</span>
      </div>
      <div v-if="blacklist.length === 0" class="px-5 py-4 text-sm text-gray-500">
        No blacklisted subtitles.
      </div>
      <table v-else class="min-w-full text-sm">
        <thead class="bg-gray-800/30">
          <tr>
            <th class="px-5 py-2 text-left text-xs text-gray-500 uppercase">Event Hash</th>
            <th class="px-5 py-2 text-left text-xs text-gray-500 uppercase">Cache Key</th>
            <th class="px-5 py-2 text-left text-xs text-gray-500 uppercase">Reason</th>
            <th class="px-5 py-2 text-left text-xs text-gray-500 uppercase">Blocked At</th>
            <th class="px-5 py-2"></th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-800">
          <tr v-for="e in blacklist" :key="e.event_hash" class="hover:bg-gray-800/20 transition">
            <td class="px-5 py-2 font-mono text-xs text-indigo-300 max-w-xs truncate" :title="e.event_hash">
              {{ e.event_hash.slice(0, 16) }}…
            </td>
            <td class="px-5 py-2 text-xs text-gray-400 max-w-xs truncate" :title="e.cache_key">
              {{ e.cache_key || '—' }}
            </td>
            <td class="px-5 py-2 text-xs text-gray-400">{{ e.reason || '—' }}</td>
            <td class="px-5 py-2 text-xs text-gray-500">{{ fmtTime(e.created_at) }}</td>
            <td class="px-5 py-2 text-right">
              <button @click="removeBlacklist(e.event_hash)"
                :disabled="removing.has(e.event_hash)"
                class="text-xs text-red-400 hover:text-red-300 transition disabled:opacity-40">
                Remove
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Pins -->
    <div class="bg-gray-900 border border-gray-800 rounded-xl overflow-hidden">
      <div class="px-5 py-3 border-b border-gray-800 flex items-center justify-between">
        <h2 class="text-sm font-semibold text-gray-200">Pinned Subtitles</h2>
        <span class="text-xs text-gray-500">{{ pins.length }} entries</span>
      </div>
      <div v-if="pins.length === 0" class="px-5 py-4 text-sm text-gray-500">
        No pinned subtitles.
      </div>
      <table v-else class="min-w-full text-sm">
        <thead class="bg-gray-800/30">
          <tr>
            <th class="px-5 py-2 text-left text-xs text-gray-500 uppercase">Cache Key</th>
            <th class="px-5 py-2 text-left text-xs text-gray-500 uppercase">Pinned Event ID</th>
            <th class="px-5 py-2 text-left text-xs text-gray-500 uppercase">Pinned At</th>
            <th class="px-5 py-2"></th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-800">
          <tr v-for="p in pins" :key="p.cache_key" class="hover:bg-gray-800/20 transition">
            <td class="px-5 py-2 text-xs text-gray-300 max-w-sm truncate" :title="p.cache_key">
              {{ p.cache_key }}
            </td>
            <td class="px-5 py-2 font-mono text-xs text-indigo-300 max-w-xs truncate" :title="p.event_id">
              {{ p.event_id }}
            </td>
            <td class="px-5 py-2 text-xs text-gray-500">{{ fmtTime(p.created_at) }}</td>
            <td class="px-5 py-2 text-right">
              <button @click="removePin(p.cache_key)"
                :disabled="removing.has(p.cache_key)"
                class="text-xs text-red-400 hover:text-red-300 transition disabled:opacity-40">
                Unpin
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Error -->
    <p v-if="error" class="text-xs text-red-400">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '@/api'

const loading = ref(false)
const error = ref('')
const blacklist = ref<any[]>([])
const pins = ref<any[]>([])
const removing = ref(new Set<string>())

const load = async () => {
  loading.value = true
  error.value = ''
  try {
    const r = await api.preferences.list()
    blacklist.value = r.blacklist ?? []
    pins.value = r.pins ?? []
  } catch (e: any) {
    error.value = e.message
  } finally {
    loading.value = false }
}

const removeBlacklist = async (hash: string) => {
  removing.value = new Set([...removing.value, hash])
  try {
    await api.preferences.removeBlacklist(hash)
    blacklist.value = blacklist.value.filter(e => e.event_hash !== hash)
  } catch (e: any) {
    error.value = e.message
  } finally {
    removing.value.delete(hash)
    removing.value = new Set(removing.value)
  }
}

const removePin = async (cacheKey: string) => {
  removing.value = new Set([...removing.value, cacheKey])
  try {
    await api.preferences.removePin(cacheKey)
    pins.value = pins.value.filter(p => p.cache_key !== cacheKey)
  } catch (e: any) {
    error.value = e.message
  } finally {
    removing.value.delete(cacheKey)
    removing.value = new Set(removing.value)
  }
}

const fmtTime = (unix: number) =>
  unix ? new Date(unix * 1000).toLocaleString() : '—'

onMounted(load)
</script>
