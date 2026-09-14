<template>
  <div>
    <div class="page-title">借车 / 还车</div>
    <div class="page-sub">借车核验：账户 · 押金 · 车辆编号 · 站点状态 · 骑行限制；还车记录：桩位 · 锁止 · 费用 · 故障反馈 · 位置</div>

    <div v-if="weather.active" class="alert danger">⛈ {{ weather.content || '恶劣天气停运中，暂停借车（还车正常）' }}</div>

    <!-- 进行中行程 → 还车 -->
    <div v-if="ongoing" class="card">
      <h3><span class="dot"></span>进行中行程</h3>
      <dl class="kv mb">
        <dt>车辆编号</dt><dd class="mono">{{ ongoing.bike_code }}</dd>
        <dt>借车站点</dt><dd>{{ ongoing.from_station }}（{{ ongoing.borrow_time && fmtTime(ongoing.borrow_time) }} 取车）</dd>
        <dt>已骑行</dt><dd>{{ elapsed }}</dd>
      </dl>
      <div class="form-row">
        <div class="form-item">
          <label>还车站点</label>
          <select class="input" v-model.number="ret.station_id">
            <option :value="0" disabled>请选择还车站点</option>
            <option v-for="s in stations" :key="s.id" :value="s.id" :disabled="s.free_docks === 0">
              {{ s.name }}（空桩 {{ s.free_docks }}）
            </option>
          </select>
        </div>
        <div class="form-item">
          <label>当前位置（还车记录）</label>
          <input class="input" v-model="ret.location" placeholder="如：万象城西门公交站" />
        </div>
      </div>
      <div class="form-row">
        <div class="form-item">
          <label>故障反馈（选填）</label>
          <select class="input" v-model="ret.fault_type">
            <option value="">车辆正常</option>
            <option value="brake">刹车异常</option>
            <option value="lock">锁具问题</option>
            <option value="chain">链条问题</option>
            <option value="tire">轮胎问题</option>
            <option value="seat">坐垫损坏</option>
            <option value="other">其他</option>
          </select>
        </div>
        <div class="form-item" v-if="ret.fault_type">
          <label>故障描述</label>
          <input class="input" v-model="ret.fault_feedback" placeholder="请描述故障情况" />
        </div>
      </div>
      <div class="flex wrap">
        <button class="btn ok" @click="doReturn" :disabled="!ret.station_id || busy">🔒 还车并锁止</button>
        <button class="btn ghost" @click="lockStuck" :disabled="busy">🔧 锁具打不开？</button>
      </div>
      <div v-if="fullErr" class="alert warn mt">
        ⚠ {{ fullErr }}
        <button class="btn sm" style="margin-left:10px" @click="createCannotReturn">发起「无法还车」协同处理</button>
      </div>
    </div>

    <!-- 借车 -->
    <div v-else class="card">
      <h3><span class="dot"></span>扫码借车</h3>
      <div class="form-row">
        <div class="form-item">
          <label>选择站点</label>
          <select class="input" v-model.number="borrow.station_id" @change="loadStation">
            <option :value="0" disabled>请选择站点</option>
            <option v-for="s in stations" :key="s.id" :value="s.id" :disabled="s.available_bikes === 0 || s.status !== 'normal'">
              {{ s.name }}（可借 {{ s.available_bikes }}）{{ s.status !== 'normal' ? '·停运' : '' }}
            </option>
          </select>
        </div>
        <div class="form-item">
          <label>车辆编号</label>
          <input class="input mono" v-model.trim="borrow.bike_code" placeholder="如 BK0007" />
        </div>
      </div>
      <div v-if="stationDetail" class="mb">
        <div class="small muted mb">桩位状态（点击可选中车辆编号）：</div>
        <div class="dock-grid">
          <div v-for="dk in stationDetail.docks" :key="dk.id" class="dock"
               :class="{ occupied: dk.status === 'occupied', faulty: dk.bike_status === 'fault' }"
               @click="dk.bike_code && dk.bike_status === 'docked' && (borrow.bike_code = dk.bike_code)">
            <span class="no">{{ dk.dock_no }}</span>
            <span>{{ dk.bike_code ? (dk.bike_status === 'fault' ? '故障' : dk.bike_code.slice(-4)) : '空' }}</span>
          </div>
        </div>
      </div>
      <button class="btn" @click="doBorrow" :disabled="!borrow.station_id || !borrow.bike_code || busy">🔓 开锁借车</button>
      <div v-if="borrowWarning" class="alert info mt">{{ borrowWarning }}</div>
    </div>

    <!-- 我的行程 -->
    <div class="card">
      <h3><span class="dot"></span>我的行程</h3>
      <table class="tbl" v-if="rides.length">
        <thead><tr><th>车辆</th><th>借车站点</th><th>借车时间</th><th>还车站点</th><th>费用</th><th>状态</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="r in rides" :key="r.id">
            <td class="mono">{{ r.bike_code }}</td>
            <td>{{ r.from_station }}</td>
            <td class="small">{{ fmtTime(r.borrow_time) }}</td>
            <td>{{ r.to_station || '—' }}</td>
            <td>{{ r.status === 'completed' ? '¥' + r.fee.toFixed(2) : '—' }}</td>
            <td>
              <span class="badge" :class="r.status === 'ongoing' ? 'info' : 'ok'">
                {{ r.status === 'ongoing' ? '进行中' : '已完成' }}
              </span>
              <span v-if="r.fault_type" class="badge warn" style="margin-left:4px">已报障</span>
            </td>
            <td>
              <button v-if="r.status === 'completed' && !r.has_appeal" class="btn sm ghost" @click="openAppeal(r)">申诉</button>
              <span v-if="r.has_appeal" class="badge purple">已申诉</span>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">暂无行程记录</div>
    </div>

    <!-- 申诉弹窗 -->
    <div v-if="appealRide" class="modal-mask" @click.self="appealRide = null">
      <div class="modal">
        <h3>行程申诉</h3>
        <dl class="kv mb">
          <dt>行程</dt><dd>{{ appealRide.from_station }} → {{ appealRide.to_station }}（{{ appealRide.bike_code }}）</dd>
          <dt>费用</dt><dd>¥{{ appealRide.fee.toFixed(2) }}</dd>
        </dl>
        <div class="form-item mb">
          <label>申诉原因</label>
          <textarea class="input" rows="3" v-model="appealReason" placeholder="请描述费用/车辆/站点问题"></textarea>
        </div>
        <div class="flex">
          <button class="btn" @click="submitAppeal" :disabled="!appealReason">提交申诉</button>
          <button class="btn ghost" @click="appealRide = null">取消</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { get, post } from '../api'
import { toast } from '../components/Toast.vue'

const router = useRouter()
const stations = ref([])
const stationDetail = ref(null)
const rides = ref([])
const weather = ref({})
const busy = ref(false)
const fullErr = ref('')
const borrowWarning = ref('')
const borrow = ref({ station_id: 0, bike_code: '' })
const ret = ref({ station_id: 0, location: '', fault_type: '', fault_feedback: '' })
const appealRide = ref(null)
const appealReason = ref('')
const nowTick = ref(Date.now())
let tickTimer = null

const ongoing = computed(() => rides.value.find(r => r.status === 'ongoing'))
const elapsed = computed(() => {
  if (!ongoing.value) return ''
  const mins = Math.floor((nowTick.value - new Date(ongoing.value.borrow_time)) / 60000)
  return mins + ' 分钟（预估费用 ¥' + (Math.max(1, Math.ceil(mins / 30)) * 1.5).toFixed(2) + '）'
})

function fmtTime(t) { return new Date(t).toLocaleString('zh-CN', { hour12: false }) }

async function loadAll() {
  const [s, r, d] = await Promise.all([get('/stations'), get('/rides/my'), get('/dashboard')])
  stations.value = s
  rides.value = r
  weather.value = d.weather_alert || {}
}

async function loadStation() {
  stationDetail.value = null
  if (!borrow.value.station_id) return
  stationDetail.value = await get('/stations/' + borrow.value.station_id)
}

async function doBorrow() {
  busy.value = true
  borrowWarning.value = ''
  try {
    const res = await post('/rides/borrow', borrow.value)
    toast(res.message)
    if (res.warning) borrowWarning.value = res.warning
    borrow.value = { station_id: 0, bike_code: '' }
    stationDetail.value = null
    await loadAll()
  } catch (e) {
    toast(e.message, true)
  } finally { busy.value = false }
}

async function doReturn() {
  busy.value = true
  fullErr.value = ''
  try {
    const res = await post(`/rides/${ongoing.value.id}/return`, ret.value)
    toast(`${res.message}｜桩位 ${res.dock_no} 号｜费用 ¥${res.fee.toFixed(2)}`)
    ret.value = { station_id: 0, location: '', fault_type: '', fault_feedback: '' }
    await loadAll()
  } catch (e) {
    if (e.status === 409 && e.data?.error === 'station_full') {
      fullErr.value = e.data.message
    } else {
      toast(e.message, true)
    }
  } finally { busy.value = false }
}

async function createCannotReturn() {
  try {
    const res = await post('/events', {
      type: 'cannot_return',
      station_id: ret.value.station_id,
      ride_id: ongoing.value.id,
      title: '用户到站无法还车（满桩）',
    })
    toast('已创建协同事件，客服/调度/站管已加入')
    router.push('/events/' + res.event_id)
  } catch (e) { toast(e.message, true) }
}

async function lockStuck() {
  try {
    const res = await post(`/rides/${ongoing.value.id}/lock-stuck`)
    toast(res.message)
    router.push('/events/' + res.event_id)
  } catch (e) { toast(e.message, true) }
}

function openAppeal(r) { appealRide.value = r; appealReason.value = '' }

async function submitAppeal() {
  try {
    await post('/appeals', { ride_id: appealRide.value.id, reason: appealReason.value })
    toast('申诉已提交')
    appealRide.value = null
    await loadAll()
  } catch (e) { toast(e.message, true) }
}

onMounted(() => {
  loadAll().catch(e => toast(e.message, true))
  tickTimer = setInterval(() => { nowTick.value = Date.now() }, 30000)
})
onUnmounted(() => clearInterval(tickTimer))
</script>
