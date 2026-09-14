<template>
  <div>
    <div class="between mb">
      <div>
        <div class="page-title">实时运营看板</div>
        <div class="page-sub">满桩 / 空桩 / 故障 / 调拨车位置 / 早晚高峰 / 地铁口客流（每 5 秒自动刷新）</div>
      </div>
      <span class="badge info">🕐 {{ now }}</span>
    </div>

    <div v-if="d.weather_alert?.active" class="alert danger">⛈ {{ d.weather_alert.content }}</div>

    <div class="grid grid-4 mb">
      <div class="stat"><span class="label">满桩站点</span><span class="value" style="color:#d03050">{{ d.full_stations ?? '-' }}</span><span class="extra">需调出</span></div>
      <div class="stat"><span class="label">空桩站点</span><span class="value" style="color:#e6a23c">{{ d.empty_stations ?? '-' }}</span><span class="extra">需调入</span></div>
      <div class="stat"><span class="label">故障车辆</span><span class="value" style="color:#7c3aed">{{ d.fault_bikes ?? '-' }}</span><span class="extra">待处理工单 {{ d.pending_faults ?? 0 }}</span></div>
      <div class="stat"><span class="label">进行中行程</span><span class="value" style="color:#1668dc">{{ d.ongoing_rides ?? '-' }}</span><span class="extra">打开事件 {{ d.open_events ?? 0 }} · 待申诉 {{ d.pending_appeals ?? 0 }}</span></div>
    </div>

    <div class="grid grid-2">
      <div class="card">
        <h3><span class="dot"></span>站点分布与调拨车位置</h3>
        <StationMap :stations="d.stations || []" :trucks="d.trucks || []" />
      </div>
      <div class="card">
        <h3><span class="dot"></span>今日借还量（早晚高峰）</h3>
        <div ref="hourlyChart" class="chart"></div>
        <div class="small muted" v-if="d.forecast?.length">
          未来 3 小时预测：
          <span v-for="f in d.forecast" :key="f.hour" class="badge gray" style="margin-right:6px">
            {{ f.hour }}时 借{{ f.borrow_need }}/还{{ f.return_need }}
          </span>
        </div>
      </div>
    </div>

    <div class="grid grid-2">
      <div class="card">
        <h3><span class="dot"></span>地铁口客流（今日）</h3>
        <div ref="flowChart" class="chart"></div>
        <div v-if="surgeStations.length" class="alert warn mt" style="margin-bottom:0">
          ⚡ 地铁突发客流：{{ surgeStations.join('、') }}，请关注周边站点借车需求
        </div>
      </div>
      <div class="card">
        <h3><span class="dot"></span>站点状态明细</h3>
        <table class="tbl">
          <thead><tr><th>站点</th><th>在桩</th><th>可借</th><th>故障</th><th>状态</th></tr></thead>
          <tbody>
            <tr v-for="s in d.stations" :key="s.id">
              <td>{{ s.name }}</td>
              <td>
                <div class="flex" style="gap:8px">
                  <div class="progress-outer" style="width:70px">
                    <div class="progress-inner" :style="{ width: (s.fill_ratio*100)+'%', background: barColor(s) }"></div>
                  </div>
                  <span class="small">{{ s.used_docks }}/{{ s.capacity }}</span>
                </div>
              </td>
              <td>{{ s.available_bikes }}</td>
              <td>{{ s.fault_bikes }}</td>
              <td><span class="badge" :class="badgeOf(s.state)">{{ stateName(s.state) }}</span></td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { get } from '../api'
import StationMap from '../components/StationMap.vue'
import * as echarts from 'echarts'

const d = ref({})
const now = ref('')
const hourlyChart = ref(null)
const flowChart = ref(null)
let hourlyInst = null
let flowInst = null
let timer = null

const surgeStations = computed(() => {
  const set = new Set()
  ;(d.value.subway_flows || []).forEach(f => { if (f.surge) set.add(f.station) })
  return [...set]
})

function stateName(st) {
  return { normal: '正常', full: '满桩', empty: '空桩', nearly_full: '将满', nearly_empty: '将空', power_outage: '电源故障', weather_suspended: '天气停运' }[st] || st
}
function badgeOf(st) {
  return { normal: 'ok', full: 'danger', empty: 'danger', nearly_full: 'warn', nearly_empty: 'warn', power_outage: 'purple', weather_suspended: 'gray' }[st] || 'gray'
}
function barColor(s) {
  if (s.fill_ratio >= 0.85 || s.fill_ratio <= 0.15) return '#d03050'
  if (s.fill_ratio >= 0.7 || s.fill_ratio <= 0.3) return '#e6a23c'
  return '#18a058'
}

function renderCharts() {
  if (hourlyChart.value && d.value.hourly) {
    if (!hourlyInst) hourlyInst = echarts.init(hourlyChart.value)
    hourlyInst.setOption({
      tooltip: { trigger: 'axis' },
      legend: { data: ['借车', '还车'], top: 0, textStyle: { fontSize: 11 } },
      grid: { left: 36, right: 12, top: 30, bottom: 24 },
      xAxis: { type: 'category', data: d.value.hourly.map(h => h.hour + '时'), axisLabel: { fontSize: 10 } },
      yAxis: { type: 'value', axisLabel: { fontSize: 10 } },
      series: [
        { name: '借车', type: 'bar', data: d.value.hourly.map(h => h.borrows), itemStyle: { color: '#1668dc', borderRadius: [3, 3, 0, 0] } },
        { name: '还车', type: 'bar', data: d.value.hourly.map(h => h.returns), itemStyle: { color: '#18a058', borderRadius: [3, 3, 0, 0] } },
      ],
    })
  }
  if (flowChart.value && d.value.subway_flows) {
    if (!flowInst) flowInst = echarts.init(flowChart.value)
    const stations = [...new Set(d.value.subway_flows.map(f => f.station))]
    const hours = [...new Set(d.value.subway_flows.map(f => f.hour))].sort((a, b) => a - b)
    flowInst.setOption({
      tooltip: { trigger: 'axis' },
      legend: { data: stations, top: 0, textStyle: { fontSize: 11 } },
      grid: { left: 44, right: 12, top: 30, bottom: 24 },
      xAxis: { type: 'category', data: hours.map(h => h + '时'), axisLabel: { fontSize: 10 } },
      yAxis: { type: 'value', name: '人次', nameTextStyle: { fontSize: 10 }, axisLabel: { fontSize: 10 } },
      series: stations.map((name, i) => ({
        name, type: 'line', smooth: true, symbol: 'none',
        data: hours.map(h => {
          const f = d.value.subway_flows.find(x => x.station === name && x.hour === h)
          return f ? f.flow : 0
        }),
        lineStyle: { width: 2 },
        itemStyle: { color: i === 0 ? '#1668dc' : '#e6a23c' },
        areaStyle: { opacity: 0.08 },
        markPoint: {
          data: d.value.subway_flows.filter(f => f.surge && f.station === name)
            .map(f => ({ coord: [f.hour + '时', f.flow], value: '突发', itemStyle: { color: '#d03050' } })),
        },
      })),
    })
  }
}

async function load() {
  try {
    d.value = await get('/dashboard')
    now.value = new Date().toLocaleTimeString('zh-CN')
    await nextTick()
    renderCharts()
  } catch (e) { /* 轮询静默 */ }
}

onMounted(() => {
  load()
  timer = setInterval(load, 5000)
  window.addEventListener('resize', renderCharts)
})
onUnmounted(() => {
  clearInterval(timer)
  window.removeEventListener('resize', renderCharts)
  hourlyInst?.dispose()
  flowInst?.dispose()
})
</script>
