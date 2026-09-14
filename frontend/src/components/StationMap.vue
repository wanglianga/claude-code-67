<template>
  <div class="map-box" ref="box">
    <svg viewBox="0 0 100 100" preserveAspectRatio="none" style="background:#eaf0f8">
      <!-- 网格 -->
      <g stroke="#dce4f0" stroke-width="0.15">
        <line v-for="i in 9" :key="'h'+i" x1="0" :y1="i*10" x2="100" :y2="i*10" />
        <line v-for="i in 9" :key="'v'+i" :x1="i*10" y1="0" :x2="i*10" y2="100" />
      </g>
      <!-- 站点 -->
      <g v-for="s in stations" :key="s.id" @mouseenter="tip = s" @mouseleave="tip = null" style="cursor:pointer">
        <circle :cx="s.x" :cy="s.y" r="4.2" :fill="colorOf(s)" fill-opacity="0.18" />
        <circle :cx="s.x" :cy="s.y" r="2.6" :fill="colorOf(s)" />
        <text :x="s.x" :y="s.y + 0.9" text-anchor="middle" font-size="1.6" fill="#fff" font-weight="bold">
          {{ s.available_bikes }}
        </text>
        <text :x="s.x" :y="s.y + 6.4" text-anchor="middle" font-size="2" fill="#33415c">{{ s.name }}</text>
      </g>
      <!-- 调拨车 -->
      <g v-for="t in trucks" :key="'t'+t.id">
        <rect :x="t.x - 2.4" :y="t.y - 1.7" width="4.8" height="3.4" rx="0.8"
              :fill="t.status === 'idle' ? '#8a97ad' : (t.status === 'stuck' ? '#d03050' : '#1668dc')" />
        <text :x="t.x" :y="t.y + 0.7" text-anchor="middle" font-size="1.5" fill="#fff">🚚</text>
        <text :x="t.x" :y="t.y - 2.4" text-anchor="middle" font-size="1.7" fill="#5a6478">{{ t.plate }}</text>
      </g>
    </svg>
    <div class="flex wrap mt small" style="gap:14px">
      <span><i class="legend-dot" style="background:#18a058"></i>正常</span>
      <span><i class="legend-dot" style="background:#e6a23c"></i>将满/将空</span>
      <span><i class="legend-dot" style="background:#d03050"></i>满桩/空桩/异常</span>
      <span><i class="legend-dot" style="background:#1668dc"></i>调拨车(在途)</span>
      <span><i class="legend-dot" style="background:#8a97ad"></i>调拨车(空闲)</span>
    </div>
    <div v-if="tip" class="station-tip" :style="tipStyle">
      <b>{{ tip.name }}</b>
      <div class="small muted">{{ tip.address }}</div>
      <div class="small mt">在桩 {{ tip.used_docks }}/{{ tip.capacity }} · 可借 {{ tip.available_bikes }} · 故障 {{ tip.fault_bikes }}</div>
      <div class="small">状态：{{ stateName(tip.state) }}</div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'

const props = defineProps({
  stations: { type: Array, default: () => [] },
  trucks: { type: Array, default: () => [] },
})
const tip = ref(null)
const box = ref(null)

const tipStyle = computed(() => {
  if (!tip.value) return {}
  const left = Math.min(tip.value.x, 70)
  const top = Math.max(tip.value.y - 14, 2)
  return { left: left + '%', top: top + '%' }
})

function colorOf(s) {
  switch (s.state) {
    case 'full': case 'empty': case 'power_outage': case 'weather_suspended': return '#d03050'
    case 'nearly_full': case 'nearly_empty': return '#e6a23c'
    default: return '#18a058'
  }
}
function stateName(st) {
  return {
    normal: '正常', full: '满桩', empty: '空桩', nearly_full: '即将满桩',
    nearly_empty: '即将空桩', power_outage: '电源故障', weather_suspended: '天气停运',
  }[st] || st
}
</script>
