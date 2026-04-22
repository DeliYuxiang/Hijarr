<template>
  <div class="space-y-4">
    <!-- Header/Filter -->
    <div class="bg-gray-900 border border-gray-800 rounded-xl p-4 flex flex-wrap gap-3 items-center justify-between">
      <div class="flex gap-2 items-center">
        <input v-model="filter" placeholder="Filter series…"
          class="bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-gray-200 w-56 focus:outline-none focus:ring-1 focus:ring-indigo-500" />
        <button @click="load" :disabled="loading"
          class="bg-indigo-600 hover:bg-indigo-700 text-white px-4 py-2 rounded-lg text-sm transition disabled:opacity-50">
          {{ loading ? 'Loading…' : 'Refresh' }}
        </button>
      </div>
      <div class="flex items-center gap-4">
        <span v-if="!configured" class="text-sm text-red-400">Sonarr not configured</span>
        <span v-else class="text-xs text-gray-500 uppercase tracking-wider font-semibold">{{ filteredSeries.length }} series in library</span>
      </div>
    </div>

    <!-- Series List -->
    <div v-if="configured" class="grid grid-cols-1 gap-3">
      <div v-for="s in filteredSeries" :key="s.id" class="bg-gray-900 border border-gray-800 rounded-xl overflow-hidden">
        <!-- Series Row -->
        <div class="px-5 py-3 flex justify-between items-center cursor-pointer hover:bg-gray-800/40 transition select-none"
          @click="toggleSeries(s.id)">
          <div class="flex items-center gap-3">
            <span class="text-gray-600 text-xs w-4">{{ openSeries.has(s.id) ? '▼' : '▶' }}</span>
            <div>
              <div class="text-gray-200 font-medium">{{ s.title }}</div>
              <div class="text-[10px] text-gray-500 font-mono">{{ s.path }}</div>
            </div>
          </div>
          <div class="flex items-center gap-6">
            <div v-if="seriesData[s.id]" class="text-right">
              <div class="text-xs text-gray-400 font-medium">
                {{ seriesData[s.id].seasons.reduce((a: number, se: any) => a + se.has_sub, 0) }}
                / {{ seriesData[s.id].seasons.reduce((a: number, se: any) => a + se.total, 0) }} episodes
              </div>
              <div class="w-24 h-1 bg-gray-800 rounded-full mt-1 overflow-hidden">
                <div class="h-full bg-emerald-500 transition-all" 
                  :style="{ width: (seriesData[s.id].seasons.reduce((a: number, se: any) => a + se.has_sub, 0) / seriesData[s.id].seasons.reduce((a: number, se: any) => a + se.total, 0) * 100) + '%' }">
                </div>
              </div>
            </div>
            <span v-if="loadingSeries.has(s.id)" class="text-xs text-indigo-400 animate-pulse">Loading…</span>
          </div>
        </div>

        <!-- Seasons Container -->
        <div v-if="openSeries.has(s.id) && seriesData[s.id]" class="bg-black/20">
          <div v-for="season in seriesData[s.id].seasons" :key="season.season" class="border-t border-gray-800/50">
            <!-- Season Header -->
            <div class="px-8 py-2.5 bg-gray-800/10 flex justify-between items-center cursor-pointer hover:bg-gray-800/30 transition select-none"
              @click="toggleSeason(s.id, season.season)">
              <div class="flex items-center gap-3">
                <span class="text-gray-600 text-[10px]">{{ openSeasons.has(`${s.id}:${season.season}`) ? '▼' : '▶' }}</span>
                <span class="text-gray-300 text-sm font-medium">Season {{ season.season }}</span>
                <span :class="season.has_sub === season.total ? 'text-emerald-400' : 'text-amber-400'" class="text-[10px] px-1.5 py-0.5 bg-gray-800 rounded border border-gray-700 font-mono">
                  {{ season.has_sub }}/{{ season.total }}
                </span>
              </div>
              <button v-if="season.has_sub < season.total"
                @click.stop="searchSeason(s, season)"
                :disabled="searchingSeason.has(`${s.id}:${season.season}`)"
                class="text-[10px] bg-indigo-600 hover:bg-indigo-700 text-white px-2.5 py-1 rounded transition disabled:opacity-50 font-bold uppercase">
                {{ searchingSeason.has(`${s.id}:${season.season}`) ? 'Searching…' : 'Sync Missing' }}
              </button>
            </div>

            <!-- Episodes Table -->
            <div v-if="openSeasons.has(`${s.id}:${season.season}`)" class="overflow-x-auto">
              <table class="min-w-full text-xs">
                <thead class="bg-gray-800/40 border-y border-gray-800">
                  <tr>
                    <th class="pl-14 pr-4 py-2 text-left text-[10px] text-gray-500 uppercase font-bold">Ep</th>
                    <th class="px-4 py-2 text-left text-[10px] text-gray-500 uppercase font-bold">Local File</th>
                    <th class="px-4 py-2 text-left text-[10px] text-gray-500 uppercase font-bold">Current Sub MD5</th>
                    <th class="px-4 py-2 text-left text-[10px] text-gray-500 uppercase font-bold">Selection (Prevent Rollback)</th>
                    <th class="px-4 py-2 text-right text-[10px] text-gray-500 uppercase font-bold">Action</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-800/50">
                  <tr v-for="ep in season.episodes" :key="ep.ep" class="hover:bg-indigo-500/5 transition group">
                    <td class="pl-14 pr-4 py-3 font-mono text-indigo-300">E{{ pad(ep.ep) }}</td>
                    <td class="px-4 py-3">
                      <div class="text-gray-300 truncate max-w-xs" :title="ep.video_path">{{ ep.video_path.split('/').pop() }}</div>
                      <div v-if="ep.has_sub" class="text-[10px] text-emerald-500/80 mt-0.5 truncate max-w-xs font-mono">{{ ep.sub_path.split('/').pop() }}</div>
                    </td>
                    <td class="px-4 py-3 font-mono">
                      <div v-if="ep.sub_md5" class="flex items-center gap-1.5">
                        <span class="text-gray-500">{{ ep.sub_md5.slice(0, 8) }}</span>
                        <span v-if="ep.sub_md5 === ep.selected_sub_md5" class="text-[9px] bg-emerald-900/40 text-emerald-400 px-1 rounded border border-emerald-800/50">PINNED</span>
                      </div>
                      <span v-else class="text-gray-700 italic">No sub on disk</span>
                    </td>
                    <td class="px-4 py-3">
                      <div v-if="ep.selected_sub_md5" class="space-y-0.5">
                        <div class="flex items-center gap-2">
                          <span class="text-[10px] text-indigo-400 font-mono">SUB: {{ ep.selected_sub_md5.slice(0, 8) }}</span>
                        </div>
                        <div v-if="ep.archive_md5" class="text-[9px] text-gray-600 font-mono">ARC: {{ ep.archive_md5.slice(0, 8) }}</div>
                      </div>
                      <span v-else class="text-gray-700 italic text-[10px]">Auto-selection active</span>
                    </td>
                    <td class="px-4 py-3 text-right space-x-2">
                      <button @click="openSelectionModal(seriesData[s.id], season.season, ep)"
                        class="text-gray-400 hover:text-white transition p-1 hover:bg-gray-800 rounded">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
                      </button>
                      <button @click="searchEp(seriesData[s.id], season.season, ep)"
                        :disabled="searchingEp.has(ep.video_path)"
                        class="bg-indigo-600/20 hover:bg-indigo-600 text-indigo-400 hover:text-white border border-indigo-600/30 px-3 py-1 rounded text-[10px] font-bold transition disabled:opacity-40">
                        {{ searchingEp.has(ep.video_path) ? '...' : 'SYNC' }}
                      </button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Selection Modal -->
    <div v-if="modalShow" class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm">
      <div class="bg-gray-900 border border-gray-800 rounded-2xl w-full max-w-4xl max-h-[90vh] flex flex-col shadow-2xl">
        <div class="px-6 py-4 border-b border-gray-800 flex justify-between items-center">
          <div>
            <h3 class="text-lg font-bold text-gray-100">{{ modalSeries.local_title || modalSeries.title }}</h3>
            <p class="text-xs text-gray-500 font-mono">S{{ pad(modalSeason) }}E{{ pad(modalEp.ep) }} — Subtitle Selection</p>
          </div>
          <button @click="modalShow = false" class="text-gray-500 hover:text-white transition">
            <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>

        <div class="p-6 overflow-y-auto space-y-4">
          <div v-if="modalLoading" class="flex flex-col items-center justify-center py-20 space-y-4">
            <div class="w-10 h-10 border-4 border-indigo-500/20 border-t-indigo-500 rounded-full animate-spin"></div>
            <p class="text-sm text-gray-400 font-medium">Querying SRN Network relays…</p>
          </div>

          <div v-else-if="modalResults.length === 0" class="text-center py-20 bg-gray-800/20 rounded-xl border border-dashed border-gray-800">
            <p class="text-gray-500">No subtitles found in SRN for this episode.</p>
            <button @click="fetchResults" class="mt-4 text-indigo-400 hover:underline text-sm font-medium">Retry Search</button>
          </div>

          <div v-else class="grid grid-cols-1 gap-3">
            <div v-for="res in modalResults" :key="res.id" 
              class="bg-gray-800/40 border border-gray-800 rounded-xl p-4 hover:border-indigo-500/50 transition cursor-pointer flex justify-between items-start group"
              @click="applySubtitle(res)">
              <div class="space-y-1">
                <div class="flex items-center gap-2">
                  <span class="text-indigo-400 text-[10px] font-mono border border-indigo-500/30 px-1 rounded bg-indigo-500/10">{{ res.language }}</span>
                  <span class="text-gray-200 font-medium text-sm group-hover:text-indigo-300 transition">{{ res.native_name || res.filename }}</span>
                </div>
                <div class="flex flex-wrap gap-x-4 gap-y-1 text-[10px] text-gray-500 font-mono">
                  <span>MD5: {{ (res.id || '').slice(0, 16) }}</span>
                  <span v-if="res.archive_md5">ARC: {{ res.archive_md5.slice(0, 16) }}</span>
                  <span class="text-gray-600">Created: {{ new Date(res.created_at * 1000).toLocaleString() }}</span>
                </div>
              </div>
              <div class="flex items-center gap-3">
                <span v-if="res.id === modalEp.selected_sub_md5" class="text-[10px] bg-indigo-600 text-white px-2 py-0.5 rounded font-bold">CURRENTLY SELECTED</span>
                <button class="bg-gray-700 group-hover:bg-indigo-600 text-white px-3 py-1.5 rounded-lg text-xs font-bold transition">APPLY</button>
              </div>
            </div>
          </div>
        </div>

        <div class="px-6 py-4 border-t border-gray-800 bg-gray-800/20 rounded-b-2xl flex justify-between items-center">
          <p class="text-[10px] text-gray-500 max-w-md">Selecting a subtitle will lock this version for the video file. Future auto-syncs will preserve this choice using Archive/Sub MD5 mapping.</p>
          <button @click="modalShow = false" class="text-sm font-bold text-gray-400 hover:text-white transition">Cancel</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { api, type SRNEvent } from '@/api'

// The Go /srn/api proxy adds native_name (localized title) to each event.
type SRNSearchResult = SRNEvent & { native_name?: string }

const filter = ref('')
const loading = ref(false)
const configured = ref(true)
const seriesList = ref<any[]>([])
const seriesData = ref<Record<number, any>>({})
const openSeries = ref(new Set<number>())
const openSeasons = ref(new Set<string>())
const loadingSeries = ref(new Set<number>())
const searchingEp = ref(new Set<string>())
const searchingSeason = ref(new Set<string>())

// Modal state
const modalShow = ref(false)
const modalLoading = ref(false)
const modalSeries = ref<any>({})
const modalSeason = ref(0)
const modalEp = ref<any>({})
const modalResults = ref<SRNSearchResult[]>([])

const filteredSeries = computed(() =>
  filter.value
    ? seriesList.value.filter(s => s.title.toLowerCase().includes(filter.value.toLowerCase()))
    : seriesList.value
)

const load = async () => {
  loading.value = true
  try {
    const r = await api.mediaLibrary()
    configured.value = r.configured ?? true
    seriesList.value = r.series ?? []
  } finally { loading.value = false }
}

const toggleSeries = async (id: number) => {
  const s = new Set(openSeries.value)
  if (s.has(id)) { s.delete(id) }
  else {
    s.add(id)
    if (!seriesData.value[id]) {
      await refreshSeries(id)
    }
  }
  openSeries.value = s
}

const refreshSeries = async (id: number) => {
  loadingSeries.value = new Set([...loadingSeries.value, id])
  try { seriesData.value[id] = await api.mediaSeries(id) }
  finally { loadingSeries.value.delete(id); loadingSeries.value = new Set(loadingSeries.value) }
}

const toggleSeason = (sid: number, season: number) => {
  const k = `${sid}:${season}`
  const s = new Set(openSeasons.value)
  if (s.has(k)) s.delete(k); else s.add(k)
  openSeasons.value = s
}

const searchSeason = async (series: any, season: any) => {
  const k = `${series.id}:${season.season}`
  searchingSeason.value = new Set([...searchingSeason.value, k])
  try {
    const detail = seriesData.value[series.id]
    for (const ep of season.episodes.filter((e: any) => !e.has_sub)) {
      await api.searchEpisode({ 
        local_title: detail.local_title, 
        tmdb_id: detail.tmdb_id, 
        season: season.season, 
        ep: ep.ep, 
        video_path: ep.video_path 
      })
    }
    await refreshSeries(series.id)
  } finally {
    searchingSeason.value.delete(k)
    searchingSeason.value = new Set(searchingSeason.value)
  }
}

const searchEp = async (series: any, season: number, ep: any) => {
  searchingEp.value = new Set([...searchingEp.value, ep.video_path])
  try {
    await api.searchEpisode({ 
      local_title: series.local_title, 
      tmdb_id: series.tmdb_id, 
      season, 
      ep: ep.ep, 
      video_path: ep.video_path 
    })
    await refreshSeries(series.series_id)
  } finally {
    searchingEp.value.delete(ep.video_path)
    searchingEp.value = new Set(searchingEp.value)
  }
}

const openSelectionModal = (series: any, season: number, ep: any) => {
  modalSeries.value = series
  modalSeason.value = season
  modalEp.value = ep
  modalShow.value = true
  fetchResults()
}

const fetchResults = async () => {
  modalLoading.value = true
  modalResults.value = []
  try {
    const q = `?tmdb_id=${modalSeries.value.tmdb_id}&s=${modalSeason.value}&e=${modalEp.value.ep}`
    const r = await api.searchSRN(q)
    modalResults.value = r || []
  } finally {
    modalLoading.value = false
  }
}

const applySubtitle = async (res: SRNSearchResult) => {
  try {
    await api.applySubtitle({
      video_path: modalEp.value.video_path,
      event_id: res.id,
      sub_md5: res.id, // using event ID as primary sub MD5
      archive_md5: res.archive_md5 || '',
      filename: res.native_name || res.filename,
      language: res.language
    })
    modalShow.value = false
    await refreshSeries(modalSeries.value.series_id)
  } catch (e: any) {
    alert('Failed to apply: ' + e.message)
  }
}

const pad = (n: number) => String(n).padStart(2, '0')

onMounted(load)
</script>
