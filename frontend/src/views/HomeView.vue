<template>
  <div class="flex-1 p-8 space-y-6 max-w-4xl mx-auto">
    <div class="bg-slate-900 border border-slate-800 rounded-lg p-8 space-y-4 shadow-xl">
      <h2 class="text-3xl font-bold">Enterprise Wiki Platform v6.0.0</h2>
      <p class="text-slate-300 text-sm leading-relaxed">
        Strikte Rechteprüfung, echte Workflow-Ausführungen mit DB-Persistenz, Kategorie-Übersichtslisten, Volltextsuche und erweiterbare Templates.
      </p>
      <div class="flex gap-3 pt-2">
        <router-link to="/wiki" class="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white text-xs font-bold rounded">Zum Wiki Workspace</router-link>
        <router-link to="/login" class="px-4 py-2 bg-slate-800 hover:bg-slate-700 text-slate-200 text-xs font-bold rounded">Anmelden</router-link>
      </div>
    </div>
  </div>
</template>
<script setup lang="ts"></script>
