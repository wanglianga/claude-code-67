<template>
  <div>
    <div class="page-title">借车 / 还车</div>
    <div class="page-sub">借车核验：账户 · 押金 · 车辆编号 · 站点状态 · 骑行限制；满桩还车：智能推荐附近空桩（费用暂停）或申请临时还车（客服审核）</div>

    <div v-if="weather.active" class="alert danger">⛈ {{ weather.content || '恶劣天气停运中，暂停借车（还车正常）' }}</div>

    <!-- 临时还车审核中 -->
    <div v-if="tempOngoing" class="card">
      <h3><span class="dot"></span>临时还车审核中</h3>
      <div class="alert info" style="display:block">
        车辆 <b class="mono">{{ tempOngoing.bike_code }}</b> 的临时还车申请已提交，<b>计费已停止</b>，请等待客服审核（需核对含站点编号的车辆照片）。
      </div>
      <dl class="kv mb">
        <dt>满桩站点</dt><dd>{{ tempOngoing.from_station }}（行程起点满桩还车）</dd>
        <dt>状态</dt><dd><span class="badge purple">客服审核中</span></dd>
      </dl>
      <div v-for="t in myTempOrders.filter(o => o.status==='pending')" :key="t.id" class="small muted">
        工单 #{{ t.id }}：{{ t.station }}（编号 {{ t.station_code }}）· 暂停时已产生 ¥{{ t.fee_before_pause.toFixed(2) }}
      </div>
    </div>

    <!-- 进行中行程 → 还车 -->
    <div v-else-if="ongoing" class="card">
      <h3><span class="dot"></span>进行中行程</h3>
      <dl class="kv mb">
        <dt>车辆编号</dt><dd class="mono">{{ ongoing.bike_code }}</dd>
        <dt>借车站点</dt><dd>{{ ongoing.from_station }}（{{ ongoing.borrow_time && fmtTime(ongoing.borrow_time) }} 取车）</dd>
        <dt>已骑行</dt><dd>{{ elapsed }}</dd>
      </dl>

      <!-- 费用已暂停横幅 -->
      <div v-if="feePausedStation" class="alert ok mb" style="display:block">
        ✅ 您已接受满桩还车引导，<b>费用已暂停计算（封顶 ¥{{ lockedFee }}）</b>，步行前往「{{ feePausedStation }}」还车不再计费。请选择该站点并点「还车并锁止」。
      </div>

      <div class="form-row">
        <div class="form-item">
          <label>还车站点</label>
          <select class="input" v-model.number="ret.station_id">
            <option :value="0" disabled>请选择还车站点</option>
            <option v-for="s in stations" :key="s.id" :value="s.id">
              {{ s.name }}（空桩 {{ s.free_docks }}）{{ s.free_docks === 0 ? '·已满桩' : '' }}
            </option>
          </select>
          <div class="small muted" style="margin-top:4px">所选站点满桩时，可使用「智能推荐还车点」或申请临时还车</div>
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
        <button class="btn" @click="openGuidance(ret.station_id)" :disabled="busy || !ret.station_id">🧭 满桩智能推荐还车点</button>
        <button class="btn ghost" @click="lockStuck" :disabled="busy">🔧 锁具打不开？</button>
      </div>
      <div v-if="fullErr" class="alert warn mt">
        ⚠ {{ fullErr }}
        <div class="flex" style="gap:8px;margin-top:8px">
          <button class="btn sm" @click="openGuidance(fullStationId)">🧭 按该满桩站周边推荐空桩（费用暂停）</button>
          <button class="btn sm ghost" @click="requestCsTemp">📞 附近都没位？联系客服生成临时还车单</button>
        </div>
      </div>
    </div>

    <!-- 借车 -->
    <div v-else-if="!tempOngoing" class="card">
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
            <td>
              {{ r.status === 'ongoing' ? '—' : '¥' + r.fee.toFixed(2) }}
              <span v-if="r.fee_adjust_amount" class="badge ok" style="margin-left:4px">减 ¥{{ r.fee_adjust_amount.toFixed(2) }}</span>
            </td>
            <td>
              <span class="badge" :class="r.status === 'completed' ? 'ok' : (r.status === 'temp_pending' ? 'purple' : 'info')">{{ r.status_name || r.status }}</span>
              <span v-if="r.fault_type" class="badge warn" style="margin-left:4px">已报障</span>
            </td>
            <td>
              <button v-if="r.status === 'completed' && !r.has_appeal" class="btn sm ghost" @click="openAppeal(r)">申诉</button>
              <span v-if="r.has_appeal" class="badge purple">已申诉</span>
            </td>
          </tr>
          <tr v-for="r in rides.filter(x => x.fee_adjust_reason && x.status==='completed')" :key="'r'+r.id" class="adjust-row">
            <td colspan="7" class="small">
              <span class="badge info">费用调整理由</span> {{ r.fee_adjust_reason }}
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">暂无行程记录</div>
    </div>

    <!-- 智能推荐还车点 -->
    <div v-if="guide" class="modal-mask" @click.self="guide=null">
      <div class="modal" style="max-width:640px">
        <h3>满桩还车引导</h3>
        <div class="alert" :class="guide.overtime ? 'warn' : 'info'" style="display:block">
          {{ guide.full_station }} 已无空桩。{{ guide.tip }}
          <div class="small mt">已骑行 {{ guide.elapsed_min }} 分钟{{ guide.overtime ? '（已超时）' : '' }} · 当前费用约 ¥{{ guide.fee_now.toFixed(2) }} · 信用分 {{ guide.credit_score }}</div>
        </div>
        <table class="tbl" v-if="guide.has_option">
          <thead><tr><th>推荐还车点</th><th>步行</th><th>空桩</th><th></th></tr></thead>
          <tbody>
            <tr v-for="(s,i) in guide.nearby" :key="s.station_id" :class="{ 'rec-first': i===0 }">
              <td><b>{{ s.name }}</b><div class="small muted">{{ s.code }} · {{ s.reason }}</div></td>
              <td>{{ s.walk_min }} 分钟</td>
              <td><span class="badge ok">{{ s.free_docks }}</span></td>
              <td><button class="btn sm ok" @click="acceptGuide(s)">接受引导·费用暂停</button></td>
            </tr>
          </tbody>
        </table>
        <div v-else class="alert danger" style="display:block">该站周边均无空桩，请联系客服生成临时还车处理单。</div>
        <div class="flex mt">
          <button class="btn" @click="requestCsTemp">📞 附近都没空桩？联系客服生成临时还车单</button>
          <button class="btn ghost" @click="guide=null">取消</button>
        </div>
      </div>
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
import { get, post, getUser } from '../api'
import { toast } from '../components/Toast.vue'

const router = useRouter()
const user = getUser()
const stations = ref([])
const stationDetail = ref(null)
const rides = ref([])
const myTempOrders = ref([])
const weather = ref({})
const busy = ref(false)
const fullErr = ref('')
const borrowWarning = ref('')
const borrow = ref({ station_id: 0, bike_code: '' })
const ret = ref({ station_id: 0, location: '', fault_type: '', fault_feedback: '' })
const appealRide = ref(null)
const appealReason = ref('')
const guide = ref(null)
const lockedFee = ref(0)
const nowTick = ref(Date.now())
let tickTimer = null

const ongoing = computed(() => rides.value.find(r => r.status === 'ongoing'))
const tempOngoing = computed(() => rides.value.find(r => r.status === 'temp_pending'))
const feePausedStation = computed(() => {
  const og = ongoing.value
  if (!og) return ''
  if (og.guidance_station_name) return og.guidance_station_name
  const s = stations.value.find(x => x.id === guidanceTarget.value)
  return s ? s.name : '推荐站点'
})
const guidanceTarget = ref(0)

const elapsed = computed(() => {
  if (!ongoing.value) return ''
  const mins = Math.floor((nowTick.value - new Date(ongoing.value.borrow_time)) / 60000)
  if (ongoing.value.fee_paused) return mins + ' 分钟（费用已暂停封顶 ¥' + lockedFee.value.toFixed(2) + '）'
  return mins + ' 分钟（预估费用 ¥' + (Math.max(1, Math.ceil(mins / 30)) * 1.5).toFixed(2) + '）'
})

function fmtTime(t) { return new Date(t).toLocaleString('zh-CN', { hour12: false }) }

async function loadAll() {
  const [s, r, d] = await Promise.all([get('/stations'), get('/rides/my'), get('/dashboard')])
  stations.value = s
  rides.value = r
  weather.value = d.weather_alert || {}
  try { myTempOrders.value = await get('/temp-returns/my') } catch (e) { /* */ }
  if (ongoing.value?.fee_paused && !lockedFee.value) {
    const mins = Math.floor((Date.now() - new Date(ongoing.value.borrow_time)) / 60000)
    lockedFee.value = Math.max(1.5, Math.ceil(mins / 30) * 1.5)
  }
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
  } catch (e) { toast(e.message, true) } finally { busy.value = false }
}

async function doReturn() {
  busy.value = true
  fullErr.value = ''
  try {
    const res = await post(`/rides/${ongoing.value.id}/return`, ret.value)
    toast(`${res.message}｜桩位 ${res.dock_no} 号｜费用 ¥${res.fee.toFixed(2)}${res.fee_paused ? '（引导暂停计费）' : ''}`)
    ret.value = { station_id: 0, location: '', fault_type: '', fault_feedback: '' }
    guidanceTarget.value = 0
    await loadAll()
  } catch (e) {
    if (e.status === 409 && e.data?.error === 'station_full') {
      fullErr.value = e.data.message
      fullStationId.value = e.data.station_id || ret.value.station_id
    } else { toast(e.message, true) }
  } finally { busy.value = false }
}

async function openGuidance(stationId) {
  if (!ongoing.value) return
  const sid = stationId || ret.value.station_id
  if (!sid) { toast('请先选择发现满桩的目标站点', true); return }
  try {
    guide.value = await post('/rides/return-guidance', { ride_id: ongoing.value.id, station_id: sid })
  } catch (e) {
    if (e.status === 409 && e.data?.error === 'station_has_free_dock') toast(e.data.message, true)
    else toast(e.message, true)
  }
}
async function acceptGuide(s) {
  try {
    const res = await post('/rides/guidance/accept', { ride_id: ongoing.value.id, station_id: s.station_id })
    toast(res.message)
    lockedFee.value = res.fee_locked
    guidanceTarget.value = s.station_id
    ret.value.station_id = s.station_id
    guide.value = null
  } catch (e) { toast(e.message, true) }
}

// 附近无空位：用户无权自建临时处理单，需联系客服（一键发起「无法还车」协同事件，客服据此创建临时还车单）
const fullStationId = ref(0)
async function requestCsTemp() {
  guide.value = null
  const stationId = fullStationId.value || ret.value.station_id
  if (!stationId) { toast('请先选择满桩站点', true); return }
  const stationName = (stations.value.find(s => s.id === stationId) || {}).name || ''
  try {
    const res = await post('/events', {
      type: 'cannot_return',
      station_id: stationId,
      ride_id: ongoing.value.id,
      title: `满桩无法还车，申请客服生成临时还车处理单（${stationName}）`,
    })
    await post(`/events/${res.event_id}/messages`, {
      content: `行程 #${ongoing.value.id}（车辆 ${ongoing.value.bike_code}，借自 ${ongoing.value.from_station}）；满桩站点：${stationName}；用户当前位置：${ret.value.location || '未填写'}。请客服核对含站点编号的车辆照片后创建临时还车处理单。`,
    })
    toast('已通知客服，客服将与您核对含站点编号的照片并生成临时还车单，审核期间计费暂停')
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
    toast('申诉已提交'); appealRide.value = null; await loadAll()
  } catch (e) { toast(e.message, true) }
}

onMounted(() => {
  loadAll().catch(e => toast(e.message, true))
  tickTimer = setInterval(() => { nowTick.value = Date.now() }, 30000)
})
onUnmounted(() => clearInterval(tickTimer))
</script>

<style scoped>
.rec-first { background: #f0fbf4; }
.adjust-row td { background: #f6faff; }
</style>
