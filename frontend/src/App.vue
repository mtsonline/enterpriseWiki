<template>
  <div class="min-h-screen flex flex-col font-sans bg-slate-950 text-slate-100">
    <header role="banner" class="border-b border-slate-800 bg-slate-950 px-6 py-3 flex items-center justify-between">
      <div class="flex items-center gap-3">
        <div class="w-9 h-9 rounded bg-blue-600 flex items-center justify-center font-bold text-white text-lg">W</div>
        <div>
          <h1 class="text-xl font-bold tracking-wide leading-none">Enterprise Wiki</h1>
          <span class="text-[10px] text-slate-400">Production v6.0.0</span>
        </div>
      </div>

      <nav role="navigation" aria-label="Hauptnavigation" class="flex items-center gap-2 bg-slate-900 p-1 rounded-lg border border-slate-800 text-sm">
        <router-link to="/" v-slot="{ isActive }">
          <button :aria-selected="isActive" role="tab" :class="['px-4 py-1.5 rounded font-semibold transition', isActive ? 'bg-blue-600 text-white font-bold' : 'text-slate-300 hover:bg-slate-800']">Home</button>
        </router-link>
        <router-link to="/wiki" v-slot="{ isActive }">
          <button :aria-selected="isActive" role="tab" :class="['px-4 py-1.5 rounded font-semibold transition', isActive ? 'bg-blue-600 text-white font-bold' : 'text-slate-300 hover:bg-slate-800']">Wiki Workspace</button>
        </router-link>
        <router-link to="/admin" v-slot="{ isActive }">
          <button :aria-selected="isActive" role="tab" :class="['px-4 py-1.5 rounded font-semibold transition', isActive ? 'bg-blue-600 text-white font-bold' : 'text-slate-300 hover:bg-slate-800']">Admin Panel</button>
        </router-link>
        <router-link v-if="!isLoggedIn" to="/login" v-slot="{ isActive }">
          <button :aria-selected="isActive" role="tab" :class="['px-4 py-1.5 rounded font-semibold transition', isActive ? 'bg-blue-600 text-white font-bold' : 'text-slate-300 hover:bg-slate-800']">Login</button>
        </router-link>
        <button v-else @click="logout" class="px-4 py-1.5 rounded font-semibold text-rose-400 hover:bg-slate-800">Abmelden</button>
      </nav>
    </header>

    <main class="flex-1 flex overflow-hidden">
      <router-view />
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'

const router = useRouter()
const isLoggedIn = computed(() => !!localStorage.getItem('wiki_token'))

const logout = () => {
  localStorage.removeItem('wiki_token')
  router.push('/')
}
</script>
