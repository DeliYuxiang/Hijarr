<template>
  <div class="bg-gray-900 border border-gray-800 rounded-xl overflow-hidden">
    <div class="px-5 py-3 border-b border-gray-800 flex flex-wrap gap-3 items-center justify-between">
      <span class="font-semibold text-gray-200">{{ title }}</span>
      <div class="flex gap-2 items-center">
        <input v-if="searchable" v-model="q" @keyup.enter="load"
          placeholder="Search…"
          class="bg-gray-800 border border-gray-700 rounded px-2 py-1.5 text-xs text-gray-200 w-44 focus:outline-none focus:ring-1 focus:ring-indigo-500" />
        <button @click="load" class="text-xs bg-gray-700 hover:bg-gray-600 text-gray-200 px-3 py-1.5 rounded transition">Search</button>
        <button v-if="selected.size > 0" @click="deleteSelected"
          class="text-xs bg-red-700 hover:bg-red-600 text-white px-3 py-1.5 rounded transition">
          Delete ({{ selected.size }})
        </button>
        <button @click="deleteAll" class="text-xs bg-red-900 hover:bg-red-800 text-white px-3 py-1.5 rounded transition">
          Clear All
        </button>
      </div>
    </div>
    <div class="overflow-x-auto max-h-96 overflow-y-auto">
      <table class="min-w-full text-xs">
        <thead class="bg-gray-800/50 sticky top-0 z-10">
          <tr>
            <th class="px-3 py-2 w-8">
              <input type="checkbox" :checked="allSelected" @change="toggleAll" class="rounded" />
            </th>
            <th v-for="col in columns" :key="col"
              class="px-3 py-2 text-left text-gray-500 uppercase font-medium">{{ col }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-800">
          <tr v-if="rows.length === 0">
            <td :colspan="columns.length + 1" class="px-4 py-6 text-center text-gray-600">No entries.</td>
          </tr>
          <tr v-for="row in rows" :key="rowKey(row)"
            :class="selected.has(rowKey(row)) ? 'bg-indigo-900/20' : 'hover:bg-gray-800/30'"
            class="transition">
            <td class="px-3 py-2">
              <input type="checkbox" :checked="selected.has(rowKey(row))"
                @change="toggleRow(rowKey(row))" class="rounded" />
            </td>
            <td v-for="col in columns" :key="col"
              class="px-3 py-2 text-gray-300 max-w-xs truncate"
              :title="String(row[col] ?? '')">
              {{ fmtCell(col, row[col]) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <!-- Pagination -->
    <div class="px-5 py-2.5 border-t border-gray-800 flex justify-between items-center text-xs text-gray-500">
      <span>{{ total }} records</span>
      <div class="flex gap-1">
        <button @click="prevPage" :disabled="page <= 1" class="px-2 py-1 border border-gray-700 rounded hover:bg-gray-800 disabled:opacity-30">&lt;</button>
        <span class="px-2 py-1">{{ page }} / {{ totalPages }}</span>
        <button @click="nextPage" :disabled="page >= totalPages" class="px-2 py-1 border border-gray-700 rounded hover:bg-gray-800 disabled:opacity-30">&gt;</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'

const props = defineProps<{
  title: string
  columns: string[]
  fetch: (q?: string, page?: number, limit?: number) => Promise<any>
  deleteFn: (keys: string[], all: boolean) => Promise<any>
  idField: string
  searchable?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:selected', keys: string[]): void
}>()

const q = ref('')
const rows = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const limit = 50
const selected = ref(new Set<string>())

watch(selected, (s) => emit('update:selected', Array.from(s)), { deep: true })

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / limit)))
const allSelected = computed(() => rows.value.length > 0 && rows.value.every(r => selected.value.has(rowKey(r))))

const rowKey = (row: any) => String(row[props.idField] ?? '')

const load = async () => {
  // Pass page/limit/q to fetch
  const res = await props.fetch(q.value, page.value, limit)
  rows.value = res.rows ?? res.events ?? res.guids ?? []
  total.value = res.total ?? rows.value.length
}

const toggleRow = (k: string) => {
  const s = new Set(selected.value)
  if (s.has(k)) s.delete(k)
  else s.add(k)
  selected.value = s
}
const toggleAll = () => {
  if (allSelected.value) selected.value = new Set()
  else selected.value = new Set(rows.value.map(rowKey))
}

const deleteSelected = async () => {
  if (!confirm(`Delete ${selected.value.size} entries?`)) return
  await props.deleteFn([...selected.value], false)
  selected.value = new Set()
  await load()
}

const deleteAll = async () => {
  if (!confirm('Delete ALL entries?')) return
  await props.deleteFn([], true)
  selected.value = new Set()
  await load()
}

const prevPage = () => { if (page.value > 1) { page.value--; load() } }
const nextPage = () => { if (page.value < totalPages.value) { page.value++; load() } }

const fmtCell = (col: string, val: unknown) => {
  if (val == null) return '—'
  if (typeof val === 'number' && (col.endsWith('_at') || col.endsWith('_ns'))) {
    const ms = col.endsWith('_ns') ? val / 1e6 : val * 1000
    return new Date(ms).toLocaleString()
  }
  return String(val)
}

onMounted(load)
</script>
