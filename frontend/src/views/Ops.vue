<template>
  <div>
    <div class="page-title">运营判断</div>
    <div class="page-sub">司机班次 · 站点电源 · 车辆清洗 · 骑行禁区 · 恶劣天气停运 · 地铁突发客流</div>

    <div class="grid grid-4 mb" v-if="ov">
      <div class="stat"><span class="label">在班司机</span><span class="value">{{ ov.active_shifts }}</span><span class="extra">今日班次</span></div>
      <div class="stat"><span class="label">待清洗车辆</span><span class="value" style="color:#e6a23c">{{ ov.cleaning_pending }}</span><span class="extra">清洗计划</span></div>
      <div class="stat"><span class="label">电源故障站点</span><span class="value" :style="{ color: ov.power_outage_stations ? '#d03050' : '#18a058' }">{{ ov.power_outage_stations }}</span><span class="extra">暂停借车</span></div>
      <div class="stat"><span class="label">突发客流预警</span><span class="value" :style="{ color: ov.surge_alerts ? '#d03050' : '#18a058' }">{{ ov.surge_alerts }}</span><span class="extra">地铁口</span></div>
    </div>

    <div v-if="ov?.weather_suspended" class="alert danger">⛈ 恶劣天气停运中：全市站点暂停借车，还车正常</div>

    <div class="grid grid-2">
      <!-- 恶劣天气 -->
      <div class="card" v-if="isOperator">
        <h3><span class="dot"></span>恶劣天气停运</h3>
        <div class="form-row">
          <div class="form-item">
            <label>预警级别</label>
            <select class="input" v-model="weatherForm.level">
              <option>黄色</option><option>橙色</option><option>红色</option>
            </select>
          </div>
          <div class="form-item">
            <label>预警内容</label>
            <input class="input" v-model="weatherForm.content" placeholder="如：暴雨橙色预警，全市暂停借车" />
          </div>
        </div>
        <div class="flex">
          <button class="btn danger" @click="setWeather(true)">发布停运</button>
          <button class="btn ok" @click="setWeather(false)">解除恢复</button>
        </div>
      </div>

      <!-- 骑行禁区 -->
      <div class="card">
        <h3><span class="dot"></span>骑行禁区</h3>
        <div v-for="z in zones" :key="z.id" class="between" style="padding:8px 0;border-bottom:1px solid var(--line)">
          <div>
            <b>{{ z.name }}</b>
            <div class="small muted">{{ z.description }}</div>
          </div>
          <div class="flex">
            <span class="badge" :class="z.active ? 'ok' : 'gray'">{{ z.active ? '生效中' : '已停用' }}</span>
            <button v-if="isOperator" class="btn sm ghost" @click="toggleZone(z)">{{ z.active ? '停用' : '启用' }}</button>
          </div>
        </div>
      </div>

      <!-- 司机班次 -->
      <div class="card">
        <h3><span class="dot"></span>调拨车司机班次</h3>
        <table class="tbl">
          <thead><tr><th>司机</th><th>日期</th><th>时段</th><th>车辆</th><th>状态</th></tr></thead>
          <tbody>
            <tr v-for="s in shifts" :key="s.id">
              <td>{{ s.driver }}</td><td>{{ s.date }}</td>
              <td>{{ s.start }} - {{ s.end }}</td><td>{{ s.truck || '—' }}</td>
              <td><span class="badge" :class="s.status === 'active' ? 'ok' : 'gray'">{{ s.status === 'active' ? '在班' : '已排班' }}</span></td>
            </tr>
          </tbody>
        </table>
        <div v-if="canShift" class="form-row mt">
          <div class="form-item">
            <select class="input" v-model.number="shiftForm.driver_id">
              <option :value="0" disabled>选择司机</option>
              <option v-for="d in drivers" :key="d.id" :value="d.id">{{ d.name }}</option>
            </select>
          </div>
          <div class="form-item"><input class="input" type="date" v-model="shiftForm.date" /></div>
          <div class="form-item"><input class="input" v-model="shiftForm.start" placeholder="开始 06:30" /></div>
          <div class="form-item"><input class="input" v-model="shiftForm.end" placeholder="结束 14:30" /></div>
          <button class="btn sm" @click="addShift">排班</button>
        </div>
      </div>

      <!-- 车辆清洗 -->
      <div class="card">
        <h3><span class="dot"></span>车辆清洗计划</h3>
        <table class="tbl">
          <thead><tr><th>车辆</th><th>站点</th><th>计划日期</th><th>保洁</th><th>状态</th><th></th></tr></thead>
          <tbody>
            <tr v-for="c in cleaning" :key="c.id">
              <td class="mono">{{ c.bike_code }}</td><td>{{ c.station }}</td><td>{{ c.date }}</td><td>{{ c.cleaner }}</td>
              <td><span class="badge" :class="c.status === 'done' ? 'ok' : 'warn'">{{ c.status === 'done' ? '已完成' : '待清洗' }}</span></td>
              <td><button v-if="c.status === 'pending' && canClean" class="btn sm ok" @click="cleanDone(c)">完成</button></td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- 站点电源 -->
      <div class="card">
        <h3><span class="dot"></span>站点电源状态</h3>
        <table class="tbl">
          <thead><tr><th>站点</th><th>电源</th><th>操作</th></tr></thead>
          <tbody>
            <tr v-for="s in stations" :key="s.id">
              <td>{{ s.name }}</td>
              <td><span class="badge" :class="s.power_status === 'normal' ? 'ok' : 'danger'">{{ s.power_status === 'normal' ? '供电正常' : '电源故障' }}</span></td>
              <td>
                <template v-if="canPower">
                  <button v-if="s.power_status === 'normal'" class="btn sm danger" @click="setPower(s, 'outage')">标记故障</button>
                  <button v-else class="btn sm ok" @click="setPower(s, 'normal')">恢复供电</button>
                </template>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- 地铁突发客流 -->
      <div class="card">
        <h3><span class="dot"></span>地铁口客流（今日）</h3>
        <table class="tbl">
          <thead><tr><th>站点</th><th>时段</th><th>客流</th><th>预警</th></tr></thead>
          <tbody>
            <tr v-for="(f, i) in peakFlows" :key="i" :class="{ 'row-alert': f.surge }">
              <td>{{ f.station }}</td><td>{{ f.hour }}:00</td><td>{{ f.flow }} 人次</td>
              <td><span v-if="f.surge" class="badge danger">⚡ 突发客流</span><span v-else class="badge gray">常规高峰</span></td>
            </tr>
          </tbody>
        </table>
        <p class="small muted mt">突发客流将自动计入调拨策略（借车需求上调），请在调拨中心重新生成计划。</p>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { get, post, getUser } from '../api'
import { toast } from '../components/Toast.vue'

const ov = ref(null)
const zones = ref([])
const shifts = ref([])
const cleaning = ref([])
const stations = ref([])
const flows = ref([])
const drivers = ref([])
const weatherForm = ref({ level: '橙色', content: '' })
const shiftForm = ref({ driver_id: 0, date: new Date().toISOString().slice(0, 10), start: '06:30', end: '14:30' })

const role = computed(() => getUser()?.role)
const isOperator = computed(() => role.value === 'operator')
const canShift = computed(() => ['operator', 'dispatcher'].includes(role.value))
const canClean = computed(() => ['operator', 'repair', 'station_admin'].includes(role.value))
const canPower = computed(() => ['operator', 'station_admin'].includes(role.value))

const peakFlows = computed(() =>
  flows.value.filter(f => f.flow >= 2000 || f.surge).sort((a, b) => a.hour - b.hour)
)

async function load() {
  const [o, z, s, c, st, d] = await Promise.all([
    get('/ops/overview'), get('/ops/forbidden-zones'), get('/ops/shifts'),
    get('/ops/cleaning'), get('/stations'), get('/dashboard'),
  ])
  ov.value = o
  zones.value = z
  shifts.value = s
  cleaning.value = c
  stations.value = st
  flows.value = d.subway_flows || []
  if (canShift.value) drivers.value = await get('/users?role=driver')
}

async function setWeather(active) {
  try {
    const res = await post('/ops/weather', { active, ...weatherForm.value })
    toast(res.message)
    await load()
  } catch (e) { toast(e.message, true) }
}

async function toggleZone(z) {
  try { await post(`/ops/forbidden-zones/${z.id}/toggle`); await load() } catch (e) { toast(e.message, true) }
}

async function addShift() {
  if (!shiftForm.value.driver_id) return toast('请选择司机', true)
  try {
    await post('/ops/shifts', shiftForm.value)
    toast('排班成功')
    await load()
  } catch (e) { toast(e.message, true) }
}

async function cleanDone(c) {
  try {
    const res = await post(`/ops/cleaning/${c.id}/done`)
    toast(res.message)
    await load()
  } catch (e) { toast(e.message, true) }
}

async function setPower(s, status) {
  try {
    const res = await post(`/stations/${s.id}/power`, { status })
    toast(res.message)
    await load()
  } catch (e) { toast(e.message, true) }
}

onMounted(() => load().catch(e => toast(e.message, true)))
</script>

<style scoped>
.row-alert td { background: #fdf0f2; }
</style>
