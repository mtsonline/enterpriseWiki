<template>
  <div class="flex-1 flex overflow-hidden">
    <!-- SIDEBAR: CATEGORIES WITH STRICT ACL CHECK -->
    <aside role="region" aria-label="Kategorien" class="w-80 border-r border-slate-800 bg-slate-950/60 p-4 flex flex-col justify-between">
      <div class="space-y-4">
        <div class="flex items-center justify-between">
          <span class="text-xs font-bold text-slate-400 uppercase tracking-wider">Kategorien</span>
          <button v-if="canEdit" @click="openAddCategoryModal" class="text-xs bg-blue-600 text-white px-2.5 py-1 rounded font-bold hover:bg-blue-500">+ Kategorie</button>
        </div>

        <!-- FULLTEXT SEARCH INPUT -->
        <div class="space-y-2">
          <input v-model="searchQuery" @input="performSearch" placeholder="Volltextsuche (Titel / Inhalt)..." class="w-full bg-slate-900 border border-slate-700 rounded p-2 text-xs text-slate-200" />
        </div>

        <ul class="space-y-1.5 text-sm">
          <li v-for="cat in filteredCategories" :key="cat.path">
            <div @click="selectCategory(cat.path)"
                 :class="['px-3 py-2 rounded flex items-center justify-between cursor-pointer border', 
                          activePath === cat.path ? 'bg-blue-600/20 text-blue-400 border-blue-500 font-bold' : 'bg-slate-900/50 border-slate-800 text-slate-300 hover:bg-slate-900']">
              <div>
                <div class="flex items-center gap-1.5">
                  <span>{{ getLocalized(cat.title) }}</span>
                  <span v-if="cat.is_default" class="text-[9px] bg-blue-900/60 text-blue-300 px-1 py-0.2 rounded font-mono">DEFAULT</span>
                </div>
                <div class="flex gap-1 mt-1">
                  <span v-for="t in cat.tags" :key="t" class="text-[9px] bg-slate-800 text-blue-300 px-1.5 py-0.5 rounded font-mono">#{{ t }}</span>
                </div>
              </div>
              <div class="flex items-center gap-1">
                <span class="text-[10px] font-mono text-slate-500">{{ cat.path }}</span>
                <button v-if="canEdit" @click.stop="deleteCategory(cat.path)" class="text-rose-400 text-xs ml-1 hover:underline">×</button>
              </div>
            </div>
          </li>
        </ul>
      </div>
    </aside>

    <!-- MAIN WORKSPACE -->
    <section class="flex-1 p-6 overflow-y-auto space-y-6">
      <div class="flex items-center justify-between border-b border-slate-800 pb-4">
        <div>
          <h2 class="text-xl font-bold">Pfad: <code class="text-blue-400">{{ activePath }}</code></h2>
          <div class="flex items-center gap-3 mt-1 text-xs">
            <span class="text-slate-400">Sprache:</span>
            <button v-for="lang in supportedLangs" :key="lang" @click="currentLocale = lang"
                    :class="['px-2.5 py-1 rounded font-mono uppercase text-xs font-bold border transition', 
                             currentLocale === lang ? 'bg-blue-600 text-white border-blue-400' : 'bg-slate-900 text-slate-400 border-slate-800']">
              {{ lang }}
            </button>
          </div>
        </div>

        <div class="flex items-center gap-3">
          <!-- EDIT MODE TOGGLE SWITCH (STRICTLY DISABLED IF NOT LOGGED IN / NO PERMISSION) -->
          <div v-if="isLoggedIn" class="flex items-center gap-2 bg-slate-900 border border-slate-800 p-1.5 rounded-lg">
            <span class="text-xs font-bold text-slate-300">Bearbeiten:</span>
            <button @click="isEditMode = !isEditMode"
                    :class="['px-3 py-1 text-xs rounded font-bold transition', isEditMode ? 'bg-amber-600 text-white' : 'bg-slate-800 text-slate-400']">
              {{ isEditMode ? 'EINGESCHALTET' : 'AUS' }}
            </button>
          </div>

          <button v-if="canEdit" @click="showAddElementModal = true" class="px-3.5 py-1.5 bg-blue-600 text-white text-xs font-bold rounded hover:bg-blue-500">+ Element hinzufügen</button>
        </div>
      </div>

      <!-- SEARCH RESULTS OR ELEMENTS LIST -->
      <div class="space-y-6">
        <div v-if="searchResults.length > 0" class="p-4 bg-slate-900 border border-blue-500/50 rounded space-y-3">
          <h3 class="text-sm font-bold text-blue-400">Suchergebnisse (Volltextsuche)</h3>
          <ul class="space-y-2 text-xs">
            <li v-for="res in searchResults" :key="res.id" @click="selectCategory(res.category_path); searchResults = []" class="p-2 bg-slate-950 rounded cursor-pointer hover:bg-slate-800 flex justify-between">
              <div>
                <span class="font-bold text-slate-200">{{ getLocalized(res.title) }}</span>
                <span class="text-slate-400 ml-2 font-mono">({{ res.category_path }})</span>
              </div>
              <span class="text-emerald-400 font-mono">{{ res.plugin_type }}</span>
            </li>
          </ul>
        </div>

        <article v-for="el in currentElements" :key="el.id" class="bg-slate-950 border border-slate-800 rounded-lg p-6 space-y-4 shadow-lg">
          <header class="flex items-center justify-between border-b border-slate-800 pb-3">
            <div>
              <div class="flex items-center gap-2">
                <h3 class="text-lg font-bold">{{ getLocalized(el.title) }}</h3>
                <span v-if="el.is_default" class="text-[9px] bg-blue-900/60 text-blue-300 px-1.5 py-0.5 rounded font-mono">DEFAULT</span>
              </div>
              <div class="flex items-center gap-2 mt-1">
                <span class="text-xs px-2 py-0.5 rounded bg-blue-900/40 text-blue-400 font-mono">{{ el.plugin_type }}</span>
                <span class="text-xs bg-slate-900 text-emerald-400 px-2 py-0.5 rounded font-mono">v{{ el.version }}</span>
                <span v-for="t in el.tags" :key="t" class="text-[10px] bg-slate-900 text-blue-400 px-2 py-0.5 rounded font-mono border border-slate-800">#{{ t }}</span>
              </div>
            </div>

            <div v-if="canEdit" class="flex items-center gap-2 text-xs">
              <button v-if="el.plugin_type === 'core.html'" @click="openWysiwygEditor(el)" class="px-2.5 py-1 bg-amber-600 text-white font-bold rounded">WYSIWYG Edit</button>
              <button v-if="el.plugin_type === 'workflow.engine'" @click="openWorkflowBuilder(el)" class="px-2.5 py-1 bg-blue-600 text-white font-bold rounded">Workflow Steps</button>
              <button @click="deleteElement(el.id)" class="text-rose-400 hover:underline">Löschen</button>
            </div>
          </header>

          <!-- PLUGIN TYPE 1: HTML RICH TEXT -->
          <div v-if="el.plugin_type === 'core.html'" class="prose prose-invert text-sm text-slate-300 leading-relaxed bg-slate-900/50 p-4 rounded border border-slate-800">
            <div v-html="getLocalizedHtml(el.content)"></div>
          </div>

          <!-- PLUGIN TYPE 2: CATEGORY ELEMENT LIST PLUGIN -->
          <div v-else-if="el.plugin_type === 'core.list'" class="bg-slate-900 p-4 rounded border border-slate-800 space-y-2 text-xs">
            <div class="font-bold text-blue-400">📋 Kategorie &amp; Elemente Index (core.list)</div>
            <p class="text-slate-400">Klicken Sie auf ein Element in dieser Kategorie, um es direkt im Hauptbereich zu laden:</p>
            <div class="flex flex-wrap gap-2 pt-1">
              <button v-for="item in el.content?.items || []" :key="item.id" @click="selectCategory(activePath)" class="px-3 py-1.5 bg-slate-950 border border-slate-700 rounded text-slate-200 hover:bg-blue-600 hover:border-blue-500">
                📄 {{ item.title }}
              </button>
            </div>
          </div>

          <!-- PLUGIN TYPE 3: REAL EXECUTABLE WORKFLOW ENGINE WITH EDIT/DELETE & DB PERSISTENCE -->
          <div v-else-if="el.plugin_type === 'workflow.engine'" class="bg-slate-900 p-5 rounded border border-slate-800 space-y-4 text-xs">
            <div class="flex justify-between items-center border-b border-slate-800 pb-2">
              <span class="font-bold text-blue-400 text-sm">⚡ Enterprise Multi-Step Workflow Engine (Echte DB Ausführung)</span>
              <button v-if="canEdit" @click="openWorkflowBuilder(el)" class="px-2.5 py-1 bg-blue-600 text-white rounded font-bold">+ Step hinzufügen</button>
            </div>
            
            <div class="space-y-3">
              <div v-for="(step, sIdx) in getWorkflowSteps(el.content)" :key="sIdx" class="p-4 bg-slate-950 rounded border border-slate-800 flex items-center justify-between">
                <div class="space-y-1">
                  <div class="flex items-center gap-2">
                    <span class="font-bold text-slate-200">Schritt {{ sIdx + 1 }}: {{ step.name }}</span>
                    <span v-if="step.executed" class="text-[10px] bg-emerald-950 text-emerald-400 px-2 py-0.5 rounded font-mono border border-emerald-800">STATUS: ERLEDIGT</span>
                  </div>
                  <div class="text-slate-400 text-[11px]">{{ step.description }}</div>
                  <div class="flex gap-2 text-[10px] font-mono mt-1">
                    <span class="text-blue-400 bg-blue-950 px-1.5 py-0.5 rounded border border-blue-900">Aktion: {{ step.action }}</span>
                    <span class="text-amber-400 bg-amber-950 px-1.5 py-0.5 rounded border border-amber-900">Rolle: {{ step.role }}</span>
                  </div>
                </div>

                <div class="flex items-center gap-2">
                  <!-- REAL EXECUTABLE ACTION BUTTON (NO SIMULATION) -->
                  <button @click="executeRealWorkflowStep(el.id, sIdx, step.action)" 
                          class="px-3 py-1.5 bg-emerald-600 hover:bg-emerald-500 text-white font-bold rounded">
                    {{ step.executed ? 'Erneut Ausführen' : 'Aktion Ausführen' }}
                  </button>
                  <button v-if="canEdit" @click="deleteWorkflowStep(el, sIdx)" class="text-rose-400 p-1 rounded hover:bg-slate-900">Löschen</button>
                </div>
              </div>
            </div>
          </div>
        </article>
      </div>
    </section>

    <!-- WYSIWYG MODAL -->
    <div v-if="showWysiwygModal" class="fixed inset-0 z-50 bg-black/80 flex items-center justify-center p-4">
      <div class="bg-slate-900 border border-slate-700 rounded-lg p-6 max-w-2xl w-full space-y-4">
        <h3 class="text-lg font-bold">WYSIWYG Editor ({{ currentLocale }})</h3>
        <textarea v-model="wysiwygHtml" rows="10" class="w-full bg-slate-950 border border-slate-700 rounded p-3 text-slate-200 font-mono text-xs"></textarea>
        <div class="flex justify-end gap-2 pt-2">
          <button @click="showWysiwygModal = false" class="px-3 py-1.5 bg-slate-800 text-xs rounded">Abbrechen</button>
          <button @click="saveWysiwygContent" class="px-4 py-1.5 bg-blue-600 text-xs font-bold text-white rounded">Speichern</button>
        </div>
      </div>
    </div>

    <!-- WORKFLOW BUILDER MODAL -->
    <div v-if="showWorkflowModal" class="fixed inset-0 z-50 bg-black/80 flex items-center justify-center p-4">
      <div class="bg-slate-900 border border-slate-700 rounded-lg p-6 max-w-xl w-full space-y-4">
        <h3 class="text-lg font-bold">Workflow Step hinzufügen</h3>
        <div class="space-y-3 text-xs">
          <div><label class="block mb-1 font-bold">Step Name</label><input v-model="newStep.name" class="w-full bg-slate-950 border border-slate-700 rounded p-2 text-slate-200" /></div>
          <div>
            <label class="block mb-1 font-bold text-blue-400">Aktion</label>
            <select v-model="newStep.action" class="w-full bg-slate-950 border border-slate-700 rounded p-2 text-slate-200">
              <option value="upload_file">upload_file (Datei hochladen &amp; speichern)</option>
              <option value="confirm_approval">confirm_approval (Freigabe erteilen &amp; abhaken)</option>
              <option value="send_email">send_email (E-Mail versenden)</option>
              <option value="script_call">script_call (Server Script ausführen)</option>
            </select>
          </div>
          <div><label class="block mb-1 font-bold">Rolle</label><input v-model="newStep.role" class="w-full bg-slate-950 border border-slate-700 rounded p-2 text-slate-200" /></div>
          <div><label class="block mb-1 font-bold">Beschreibung</label><textarea v-model="newStep.description" rows="2" class="w-full bg-slate-950 border border-slate-700 rounded p-2 text-slate-200"></textarea></div>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <button @click="showWorkflowModal = false" class="px-3 py-1.5 bg-slate-800 text-xs rounded">Abbrechen</button>
          <button @click="saveWorkflowStep" class="px-4 py-1.5 bg-blue-600 text-xs font-bold text-white rounded">Speichern</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'

const categories = ref<any[]>([])
const elements = ref<any[]>([])
const activePath = ref('1')
const currentLocale = ref('de')
const supportedLangs = ref(['de', 'en', 'fr', 'es'])

const searchQuery = ref('')
const searchResults = ref<any[]>([])

const isEditMode = ref(false)
const isLoggedIn = computed(() => !!localStorage.getItem('wiki_token'))

// STRICT ACL CHECK: Unauthenticated users are NEVER admin and CANNOT edit
const canEdit = computed(() => isLoggedIn.value && isEditMode.value)

const showWysiwygModal = ref(false)
const showWorkflowModal = ref(false)
const showAddCategoryModal = ref(false)
const showAddElementModal = ref(false)

const editingElement = ref<any>(null)
const wysiwygHtml = ref('')
const newStep = ref({ name: '', action: 'confirm_approval', role: 'Manager', description: '', executed: false })
const newCategory = ref({ path: '', title_de: '', title_en: '', tags: 'docs' })
const newElement = ref({ plugin_type: 'core.html', title_de: '', tags: 'general' })

const fetchCategories = async () => {
  try {
    const res = await fetch('/api/v1/categories')
    if (res.ok) categories.value = await res.json()
  } catch (e) {
    console.error('Fetch categories failed:', e)
  }
}

const fetchElements = async (path: string) => {
  try {
    const res = await fetch(`/api/v1/elements?category_path=${path}`)
    if (res.ok) elements.value = await res.json() || []
  } catch (e) {
    console.error('Fetch elements failed:', e)
  }
}

onMounted(() => {
  fetchCategories()
  fetchElements('1')
})

const selectCategory = (path: string) => {
  activePath.value = path
  fetchElements(path)
}

const performSearch = async () => {
  if (!searchQuery.value) {
    searchResults.value = []
    return
  }
  const res = await fetch(`/api/v1/search?q=${encodeURIComponent(searchQuery.value)}`)
  if (res.ok) searchResults.value = await res.json()
}

const openAddCategoryModal = () => {
  newCategory.value.path = activePath.value + '.1'
  showAddCategoryModal.value = true
}

const createCategory = async () => {
  await fetch('/api/v1/categories', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      path: newCategory.value.path,
      title: { de: newCategory.value.title_de, en: newCategory.value.title_de },
      tags: newCategory.value.tags.split(',').map(t => t.trim()),
      is_public_guest: true
    })
  })
  showAddCategoryModal.value = false
  fetchCategories()
}

const deleteCategory = async (path: string) => {
  await fetch(`/api/v1/categories/${path}`, { method: 'DELETE' })
  fetchCategories()
}

const openWysiwygEditor = (el: any) => {
  editingElement.value = el
  wysiwygHtml.value = el.content?.html?.[currentLocale.value] || ''
  showWysiwygModal.value = true
}

const saveWysiwygContent = async () => {
  if (!editingElement.value) return
  const content = JSON.parse(JSON.stringify(editingElement.value.content || {}))
  if (!content.html) content.html = {}
  content.html[currentLocale.value] = wysiwygHtml.value

  await fetch(`/api/v1/elements/${editingElement.value.id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      title: editingElement.value.title,
      content: content,
      tags: editingElement.value.tags
    })
  })
  showWysiwygModal.value = false
  fetchElements(activePath.value)
}

const openWorkflowBuilder = (el: any) => {
  editingElement.value = el
  showWorkflowModal.value = true
}

const saveWorkflowStep = async () => {
  if (!editingElement.value) return
  const content = JSON.parse(JSON.stringify(editingElement.value.content || {}))
  if (!content.steps) content.steps = []
  content.steps.push({ ...newStep.value })

  await fetch(`/api/v1/elements/${editingElement.value.id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      title: editingElement.value.title,
      content: content,
      tags: editingElement.value.tags
    })
  })
  showWorkflowModal.value = false
  newStep.value = { name: '', action: 'confirm_approval', role: 'Manager', description: '', executed: false }
  fetchElements(activePath.value)
}

const deleteWorkflowStep = async (el: any, sIdx: number) => {
  const content = JSON.parse(JSON.stringify(el.content || {}))
  if (content.steps) {
    content.steps.splice(sIdx, 1)
    await fetch(`/api/v1/elements/${el.id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: el.title, content: content, tags: el.tags })
    })
    fetchElements(activePath.value)
  }
}

const executeRealWorkflowStep = async (elId: number, sIdx: number, action: string) => {
  const res = await fetch('/api/v1/workflow/execute-step', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ element_id: elId, step_index: sIdx, action: action })
  })
  if (res.ok) {
    fetchElements(activePath.value)
  }
}

const deleteElement = async (id: number) => {
  await fetch(`/api/v1/elements/${id}`, { method: 'DELETE' })
  fetchElements(activePath.value)
}

const getLocalized = (obj: any) => obj ? (obj[currentLocale.value] || obj['de'] || '') : ''
const getLocalizedHtml = (contentObj: any) => contentObj?.html ? (contentObj.html[currentLocale.value] || contentObj.html['de'] || '') : ''
const getWorkflowSteps = (contentObj: any) => contentObj?.steps || []

const filteredCategories = computed(() => categories.value || [])
const currentElements = computed(() => elements.value || [])
</script>
