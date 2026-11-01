<template>
  <div class="flex-1 flex items-center justify-center p-6">
    <div class="bg-slate-900 border border-slate-800 rounded-lg p-8 max-w-md w-full space-y-6 shadow-2xl">
      <div class="border-b border-slate-800 pb-4">
        <h2 class="text-2xl font-bold">System Anmeldung</h2>
        <p class="text-xs text-slate-400 mt-1">Authentifizierung über Go-Backend API-Endpunkt</p>
      </div>

      <div class="space-y-4 text-xs">
        <div>
          <label class="block mb-1 text-slate-400 font-bold">Provider</label>
          <select v-model="loginForm.provider" class="w-full bg-slate-950 border border-slate-700 rounded p-2 text-slate-200">
            <option value="auth.local">auth.local (Lokaler Vorrang)</option>
            <option value="auth.ldap">auth.ldap (LDAP / AD)</option>
          </select>
        </div>
        <div>
          <label class="block mb-1 text-slate-400 font-bold">Benutzername</label>
          <input v-model="loginForm.username" type="text" class="w-full bg-slate-950 border border-slate-700 rounded p-2 text-slate-200" placeholder="admin" />
        </div>
        <div>
          <label class="block mb-1 text-slate-400 font-bold">Passwort</label>
          <input v-model="loginForm.password" type="password" class="w-full bg-slate-950 border border-slate-700 rounded p-2 text-slate-200" />
        </div>
        <div v-if="loginError" class="text-rose-400 font-bold">{{ loginError }}</div>
      </div>

      <button @click="performLogin" class="w-full py-2.5 bg-blue-600 hover:bg-blue-500 text-white text-xs font-bold rounded transition">
        Anmelden (API Call)
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'

const router = useRouter()
const loginForm = ref({ username: 'admin', password: 'EnterpriseWiki2026!', provider: 'auth.local' })
const loginError = ref('')

const performLogin = async () => {
  try {
    const res = await fetch('/api/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(loginForm.value)
    })
    if (res.ok) {
      const data = await res.json()
      localStorage.setItem('wiki_token', data.token)
      router.push('/wiki')
    } else {
      loginError.value = 'Ungültige Anmeldedaten'
    }
  } catch (e) {
    loginError.value = 'Verbindungsfehler zum Backend'
  }
}
</script>
