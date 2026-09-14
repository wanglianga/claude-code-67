<template>
  <div>
    <div class="page-title">{{ greeting }}，{{ user?.name }}</div>
    <div class="page-sub">{{ subtitle }}</div>

    <div v-if="weather.active" class="alert danger">⛈ {{ weather.content || '恶劣天气停运中：全市站点暂停借车，还车正常' }}</div>

    <div class="grid grid-4 mb">
      <div class="stat" v-for="s in stats" :key="s.label">
        <span class="label">{{ s.label }}</span>
        <span class="value" :style="{ color: s.color }">{{ s.value }}</span>
        <span class="extra">{{ s.extra }}</span>
      </div>
    </div>

    <div class="card">
      <h3><span class="dot"></span>快捷入口</h3>
      <div class="flex wrap">
        <router-link v-for="q in quickLinks" :key="q.path" :to="q.path" class="btn ghost">{{ q.icon }} {{ q.title }}</router-link>
      </div>
    </div>

    <div class="card" v-if="isStaff">
      <h3><span class="dot"></span>站点实时状态</h3>
      <StationMap :stations="stations" :trucks="trucks" />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { getUser, get } from '../api'
import StationMap from '../components/StationMap.vue'

const user = computed(() => getUser())
const isStaff = computed(() => user.value && user.value.role !== 'user')
const stations = ref([])
const trucks = ref([])
const weather = ref({})
const counts = ref({})
const myRides = ref([])

// 用户首页仅统计本人行程
const myOngoing = computed(() => myRides.value.filter(r => r.status === 'ongoing').length)
const myTotal = computed(() => myRides.value.length)

const greeting = computed(() => {
  const h = new Date().getHours()
  if (h < 6) return '凌晨好'
  if (h < 12) return '上午好'
  if (h < 14) return '中午好'
  if (h < 18) return '下午好'
  return '晚上好'
})

const subtitles = {
  user: '借车、还车、行程与申诉都在这里办理',
  cs: '处理协同事件与用户申诉，查看调度动作解释',
  dispatcher: '生成调拨计划、派发调拨车、处理协同事件',
  repair: '接收故障工单、登记维修档案与配件消耗',
  station_admin: '关注本站满桩/空桩状态与现场处置',
  operator: '全局运营判断：调拨策略、站点调整、停运与解释',
  city: '查看调度动作解释与站点调整影响评估',
}
const subtitle = computed(() => subtitles[user.value?.role] || '')

const stats = computed(() => {
  const c = counts.value
  if (!isStaff.value) {
    return [
      { label: '押金', value: '¥' + (user.value?.deposit ?? 0), extra: '借车前需缴满 ¥199', color: '#1668dc' },
      { label: '余额', value: '¥' + (user.value?.balance ?? 0), extra: '骑行费用自动扣减', color: '#18a058' },
      { label: '我的进行中行程', value: myOngoing.value, extra: myOngoing.value ? '还车后自动计费' : '当前无进行中行程', color: '#7c3aed' },
      { label: '我的累计行程', value: myTotal.value, extra: user.value?.restricted ? '账户受限，请联系客服' : '账户状态正常', color: user.value?.restricted ? '#d03050' : '#1668dc' },
    ]
  }
  return [
    { label: '满桩站点', value: c.full_stations ?? '-', extra: '需要调出车辆', color: '#d03050' },
    { label: '空桩站点', value: c.empty_stations ?? '-', extra: '需要调入车辆', color: '#e6a23c' },
    { label: '故障车辆', value: c.fault_bikes ?? '-', extra: '待维修 ' + (c.pending_faults ?? 0) + ' 单', color: '#7c3aed' },
    { label: '进行中事件', value: c.open_events ?? '-', extra: '待处理申诉 ' + (c.pending_appeals ?? 0), color: '#1668dc' },
  ]
})

const quickLinks = computed(() => {
  const u = user.value
  if (!u) return []
  if (u.role === 'user') {
    return [
      { path: '/ride', title: '借车 / 还车', icon: '🚲' },
      { path: '/events', title: '我的协同事件', icon: '🚨' },
      { path: '/appeals', title: '我的申诉', icon: '📝' },
    ]
  }
  const links = [{ path: '/dashboard', title: '实时运营看板', icon: '📊' }, { path: '/events', title: '协同事件', icon: '🚨' }]
  if (['dispatcher', 'operator'].includes(u.role)) links.push({ path: '/rebalance', title: '调拨中心', icon: '🚚' })
  if (['repair', 'operator'].includes(u.role)) links.push({ path: '/maintenance', title: '维修工单', icon: '🔧' })
  if (['operator', 'station_admin', 'city'].includes(u.role)) links.push({ path: '/stations', title: '站点调整分析', icon: '📍' })
  if (u.role === 'operator') links.push({ path: '/ops', title: '运营判断', icon: '🧭' })
  links.push({ path: '/appeals', title: '申诉处理', icon: '📝' })
  return links
})

onMounted(async () => {
  try {
    const d = await get('/dashboard')
    stations.value = d.stations || []
    trucks.value = d.trucks || []
    weather.value = d.weather_alert || {}
    counts.value = d
  } catch (e) { /* 首页静默失败 */ }
  // 用户角色：拉取本人行程用于首页统计（不用全平台数据）
  if (!isStaff.value) {
    try {
      myRides.value = await get('/rides/my')
    } catch (e) { /* 静默 */ }
  }
})
</script>
