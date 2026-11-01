<template>
  <div class="flex-1 p-6 overflow-y-auto space-y-6">
    <div class="border-b border-slate-800 pb-4 flex justify-between items-center">
      <div>
        <h2 class="text-2xl font-bold">Admin-Zentrale (Template &amp; ACL Manager)</h2>
        <p class="text-xs text-slate-400 mt-1">Template Erweiterungen, ACL Rechte &amp; Systemkonfiguration</p>
      </div>
      <div class="flex gap-2 text-xs">
        <button @click="subTab = 'templates'" :class="['px-3 py-1.5 rounded font-bold', subTab === 'templates' ? 'bg-blue-600 text-white' : 'bg-slate-800']">Templates</button>
        <button @click="subTab = 'acl'" :class="['px-3 py-1.5 rounded font-bold', subTab === 'acl' ? 'bg-blue-600 text-white' : 'bg-slate-800']">ACL Rechte</button>
      </div>
    </div>

    <!-- SUB-TAB 1: TEMPLATES (EXTENDABLE THEMES) -->
    <div v-if="subTab === 'templates'" class="space-y-4 text-xs">
      <div class="bg-slate-900 border border-slate-800 rounded-lg p-5 space-y-4">
        <h3 class="text-lg font-bold text-blue-400">Template Marketplace &amp; Extension System</h3>
        <p class="text-slate-400">Templates erweitern das Basis-Template (<code class="text-amber-400">theme.default.modern</code>) nahtlos:</p>
        <div class="grid grid-cols-2 gap-4">
          <div class="p-4 bg-slate-950 rounded border border-slate-800 space-y-2">
            <div class="font-bold text-slate-200">Modern Enterprise Dark Theme (theme.default.modern)</div>
            <div class="text-emerald-400 font-mono">Status: Aktiv (Basis Template)</div>
          </div>
          <div class="p-4 bg-slate-950 rounded border border-slate-800 space-y-2">
            <div class="font-bold text-slate-200">Corporate Blue Extension (theme.custom.corporate)</div>
            <div class="text-slate-400">Erweitert: theme.default.modern</div>
            <button class="px-3 py-1 bg-blue-600 text-white rounded font-bold">Als Aktiv setzen</button>
          </div>
        </div>
      </div>
    </div>

    <!-- SUB-TAB 2: ACL -->
    <div v-if="subTab === 'acl'" class="space-y-4 text-xs">
      <div class="bg-slate-900 border border-slate-800 rounded-lg p-5 space-y-4">
        <h3 class="text-lg font-bold text-blue-400">Granulare ACL-Regeln</h3>
        <table class="w-full text-left border-collapse">
          <thead class="bg-slate-950 text-slate-400 border-b border-slate-800">
            <tr>
              <th class="p-2.5">Ressource</th>
              <th class="p-2.5">ID / Pfad</th>
              <th class="p-2.5">Prinzipal</th>
              <th class="p-2.5">Berechtigung</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-800">
            <tr v-for="rule in aclRules" :key="rule.id">
              <td class="p-2.5 font-bold text-emerald-400">{{ rule.resource_type }}</td>
              <td class="p-2.5 font-mono text-blue-300">{{ rule.resource_id }}</td>
              <td class="p-2.5">{{ rule.principal_type }}: {{ rule.principal_id }}</td>
              <td class="p-2.5"><span class="px-2 py-0.5 rounded bg-blue-900/40 text-blue-300 border border-blue-700 font-mono">{{ rule.permission }}</span></td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'

const subTab = ref('templates')
const aclRules = ref<any[]>([])

const fetchAclRules = async () => {
  try {
    const res = await fetch('/api/v1/acl')
    if (res.ok) aclRules.value = await res.json()
  } catch (e) {
    console.error('Fetch ACL failed:', e)
  }
}

onMounted(() => {
  fetchAclRules()
})
</script>
