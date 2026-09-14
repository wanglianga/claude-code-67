<template>
  <router-view v-if="isLoginPage" />
  <div v-else class="layout">
    <aside class="sidebar">
      <div class="logo">
        <span class="bike-ico">🚲</span>
        <span>公共自行车运营平台</span>
      </div>
      <div class="role-tag" v-if="user">
        <b>{{ user.name }}</b>
        {{ user.role_name }}
      </div>
      <nav class="nav">
        <div v-for="item in menu" :key="item.path"
             class="nav-item" :class="{ active: isActive(item.path) }"
             @click="$router.push(item.path)">
          <span class="ico">{{ item.icon }}</span>{{ item.title }}
        </div>
      </nav>
      <div class="logout" @click="logout">⏻ 退出登录</div>
    </aside>
    <main class="main">
      <router-view />
    </main>
    <Toast />
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getUser, clearAuth, post } from './api'
import Toast, { toast } from './components/Toast.vue'

const route = useRoute()
const router = useRouter()
const user = computed(() => getUser())
const isLoginPage = computed(() => route.path === '/login')

const MENUS = [
  { path: '/', title: '首页', icon: '🏠', roles: 'all' },
  { path: '/ride', title: '借车 / 还车', icon: '🚲', roles: ['user'] },
  { path: '/dashboard', title: '实时运营看板', icon: '📊', roles: 'staff' },
  { path: '/events', title: '协同事件', icon: '🚨', roles: 'all' },
  { path: '/rebalance', title: '调拨中心', icon: '🚚', roles: ['dispatcher', 'operator', 'city', 'cs'] },
  { path: '/peak-routes', title: '高峰调拨路线', icon: '🗺️', roles: ['dispatcher', 'operator', 'city', 'cs'] },
  { path: '/maintenance', title: '维修与车辆档案', icon: '🔧', roles: ['repair', 'operator', 'dispatcher'] },
  { path: '/appeals', title: '申诉处理', icon: '📝', roles: 'all' },
  { path: '/stations', title: '站点与调整分析', icon: '📍', roles: ['operator', 'station_admin', 'city', 'dispatcher'] },
  { path: '/ops', title: '运营判断', icon: '🧭', roles: ['operator', 'dispatcher', 'station_admin', 'repair'] },
]
const STAFF = ['cs', 'dispatcher', 'repair', 'station_admin', 'operator', 'city', 'driver']

const menu = computed(() => {
  const u = user.value
  if (!u) return []
  return MENUS.filter(m => {
    if (m.roles === 'all') {
      if (m.path === '/events') return true
      if (m.path === '/appeals') return true
      if (m.path === '/') return true
      return false
    }
    if (m.roles === 'staff') return STAFF.includes(u.role)
    return m.roles.includes(u.role)
  })
})

function isActive(p) {
  if (p === '/') return route.path === '/'
  return route.path.startsWith(p)
}

async function logout() {
  try { await post('/logout') } catch (e) { /* ignore */ }
  clearAuth()
  toast('已退出登录')
  router.push('/login')
}
</script>
