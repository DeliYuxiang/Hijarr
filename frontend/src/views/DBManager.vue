<template>
  <div class="space-y-4">
    <!-- Tab bar -->
    <div class="bg-gray-900 border border-gray-800 rounded-xl p-1 flex flex-wrap gap-1">
      <button v-for="t in tabs" :key="t.key" @click="activeTab = t.key"
        :class="activeTab === t.key ? 'bg-indigo-600 text-white' : 'text-gray-400 hover:bg-gray-800'"
        class="px-3 py-1.5 rounded-lg text-sm font-medium transition">
        {{ t.label }}
      </button>
    </div>

    <!-- Metadata Cache -->
    <GenericTable v-if="activeTab === 'mc'"
      title="Metadata Cache"
      :columns="['raw_title','title','tmdb_id','season','episode','updated_at']"
      :fetch="(q, page, limit) => api.db.mc(`?q=${encodeURIComponent(q||'')}&page=${page||1}&limit=${limit||50}`)"
      :delete-fn="(keys: string[], all: boolean) => api.db.mcDelete({ keys, all })"
      id-field="raw_title"
      @update:selected="(keys) => mcSelected = keys"
      searchable />

    <!-- SRN Events -->
    <GenericTable v-if="activeTab === 'se'"
      title="SRN Events (DB)"
      :columns="['id','title','season','ep','lang','filename','size_bytes','created_at']"
      :fetch="(q, page, limit) => api.db.se(`?q=${encodeURIComponent(q||'')}&page=${page||1}&limit=${limit||50}`)"
      :delete-fn="(keys: string[], all: boolean) => api.db.seDelete({ keys, all })"
      id-field="id"
      searchable />

    <!-- Seen Files -->
    <GenericTable v-if="activeTab === 'sf'"
      title="Seen Files"
      :columns="['path','mtime_ns']"
      :fetch="(q, page, limit) => api.db.sf(`?q=${encodeURIComponent(q||'')}&page=${page||1}&limit=${limit||50}`)"
      :delete-fn="(keys: string[], all: boolean) => api.db.sfDelete({ keys, all })"
      id-field="path"
      searchable />

    <!-- Failed Files -->
    <GenericTable v-if="activeTab === 'ff'"
      title="Failed Files"
      :columns="['path','failed_at']"
      :fetch="(q, page, limit) => api.db.ff(`?q=${encodeURIComponent(q||'')}&page=${page||1}&limit=${limit||50}`)"
      :delete-fn="(keys: string[], all: boolean) => api.db.ffDelete({ keys, all })"
      id-field="path"
      searchable />

    <!-- Batch Modifier for Metadata Cache -->
    <div v-if="activeTab === 'mc' && mcSelected.length > 0" class="bg-gray-900 border border-gray-800 rounded-xl p-5 mt-4 sticky bottom-4 shadow-2xl z-20 overflow-visible">
      <h3 class="text-sm font-semibold text-gray-200 mb-3">Batch Override ({{ mcSelected.length }} Items)</h3>
      <div class="flex gap-4 items-end">
        
        <!-- TMDB Title Input (Combo box logic) -->
        <div class="flex-1 relative">
          <label class="block text-xs text-gray-500 mb-1">Search Linked TMDB Title</label>
          <input list="tmdbOptions" v-model="batchMC.titleSearch" @keyup.enter="onTitleEnter"
            placeholder="Type title… (Enter to search TMDB if no local results)"
            class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 focus:outline-none focus:ring-1 focus:ring-indigo-500" />
          <datalist id="tmdbOptions">
            <option v-for="opt in mcTitleOptions" :key="opt.tmdb_id" :value="opt.title">{{ opt.title }} (ID: {{ opt.tmdb_id }})</option>
          </datalist>
          <div class="text-[10px] mt-1 flex gap-2">
            <span v-if="batchMC.localMiss" class="text-yellow-500">⚠ 本地缓存无结果，按 Enter 查询 TMDB</span>
            <span v-else class="text-gray-500">Selected ID: {{ batchMC.tmdbId || 'None' }}</span>
          </div>
        </div>

        <!-- Season -->
        <div class="w-32">
          <label class="block text-xs text-gray-500 mb-1">Season</label>
          <select v-model.number="batchMC.season" 
            class="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 focus:outline-none focus:ring-1 focus:ring-indigo-500">
            <option v-for="s in mcSeasonOptions" :key="s" :value="s">Season {{ s }}</option>
          </select>
        </div>

        <!-- Update Button -->
        <div>
          <button @click="doBatchUpdate" :disabled="!batchMC.tmdbId || batchMC.isSubmitting"
            class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white font-medium px-5 py-2 rounded transition text-sm flex items-center h-[38px]">
            <svg v-if="batchMC.isSubmitting" class="animate-spin -ml-1 mr-2 h-4 w-4 text-white" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>
            {{ batchMC.isSubmitting ? 'Updating...' : 'Update' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { api } from '@/api'
import GenericTable from '@/components/GenericTable.vue'

const activeTab = ref('mc')

// Metadata Cache UI
const mcSelected = ref<string[]>([])
const mcTitleOptions = ref<Array<{ title: string, tmdb_id: number }>>([])
const mcSeasonOptions = ref<number[]>([])

const batchMC = ref({
  titleSearch: '',
  tmdbId: 0,
  season: 1,
  isSubmitting: false,
  localMiss: false,  // true = local cache 无结果，提示用户按 Enter 查 TMDB
})

// ─── 两阶段搜索 ──────────────────────────────────────────────────────────────
// Phase 1 (typing): query local metadata_cache
// Phase 2 (Enter, when Phase 1 was empty): query TMDB API
watch(() => batchMC.value.titleSearch, async (val) => {
  if (!val || val.length < 2) {
    batchMC.value.localMiss = false
    return
  }

  // Did they select an exact match from the datalist already?
  const matched = mcTitleOptions.value.find(o => o.title === val)
  if (matched) {
    await applyTMDBMatch(matched.tmdb_id, matched.title)
    return
  }

  // Phase 1: query local cache
  const res = await api.db.mc(`?q=${encodeURIComponent(val)}&page=1&limit=20`)
  const map = new Map<number, string>()
  res.rows?.forEach((r: any) => map.set(r.tmdb_id, r.title))
  mcTitleOptions.value = Array.from(map.entries()).map(([id, t]) => ({ tmdb_id: id, title: t }))

  batchMC.value.localMiss = mcTitleOptions.value.length === 0
})

// Phase 2: called on Enter key when local search returned nothing
const onTitleEnter = async () => {
  const val = batchMC.value.titleSearch.trim()
  if (!val) return

  // First check if user selected from datalist
  const matched = mcTitleOptions.value.find(o => o.title === val)
  if (matched) {
    await applyTMDBMatch(matched.tmdb_id, matched.title)
    return
  }

  // Fall through to TMDB
  try {
    const res = await api.tmdbSearch(val)
    const hits = res.results ?? []
    mcTitleOptions.value = hits.map(h => ({ tmdb_id: h.TMDBID, title: h.Title }))
    batchMC.value.localMiss = false
    if (mcTitleOptions.value.length === 1) {
      // Auto-select if only one result
      await applyTMDBMatch(mcTitleOptions.value[0].tmdb_id, mcTitleOptions.value[0].title)
    }
  } catch {
    // ignore
  }
}

// Apply a matched TMDB title: set ID and load seasons
async function applyTMDBMatch(tmdbId: number, title: string) {
  batchMC.value.tmdbId = tmdbId
  batchMC.value.titleSearch = title
  batchMC.value.localMiss = false
  try {
    const res = await api.tmdbSeasonCount(tmdbId)
    const n = res.count ?? 1
    mcSeasonOptions.value = Array.from({ length: n + 1 }, (_, i) => i)
  } catch {
    mcSeasonOptions.value = [0, 1]
  }
  if (!mcSeasonOptions.value.includes(batchMC.value.season)) {
    batchMC.value.season = 1
  }
}


const doBatchUpdate = async () => {
  batchMC.value.isSubmitting = true
  try {
    const promises = mcSelected.value.map(raw_title => {
      return api.db.mcUpsert({
        raw_title: raw_title,
        tmdb_id: batchMC.value.tmdbId,
        title: batchMC.value.titleSearch,
        season: batchMC.value.season,
        episode: 0,
        aliases: []
      })
    })
    await Promise.all(promises)
    alert(`Successfully synced ${mcSelected.value.length} keys to TV DB ID ${batchMC.value.tmdbId}`)
    mcSelected.value = []
    
    // Quick trick to force generic table to reload
    const tab = activeTab.value
    activeTab.value = ''
    setTimeout(() => activeTab.value = tab, 50)
  } catch (err: any) {
    alert("Batch update failed: " + err.message)
  } finally {
    batchMC.value.isSubmitting = false
  }
}

const tabs = [
  { key: 'mc', label: 'Metadata Cache' },
  { key: 'se', label: 'SRN Events' },
  { key: 'sf', label: 'Seen Files' },
  { key: 'ff', label: 'Failed Files' },
]
</script>
