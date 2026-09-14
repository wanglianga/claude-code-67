<template>
  <div v-if="route">
    <div class="between mb">
      <div>
        <div class="page-title">
          <a class="back-link" @click="$router.push('/peak-routes')">高峰调拨路线</a> / {{ route.code }}
        </div>
        <div class="page-sub">{{ route.name }} · {{ route.plan_date }} {{ route.peak_type==='morning'?'早高峰':'晚高峰' }}</div>
      </div>
      <span class="badge" :class="routeBadge(route.status)" style="font-size:13px">{{ route.status_name }}</span>
    </div>

    <!-- 汇总 -->
    <div class="grid grid-4 mb">
      <div class="stat"><span class="label">调拨车 / 司机</span><span class="value" style="font-size:17px">{{ route.truck || '未派车' }}</span><span class="extra">{{ route.driver || '待排班' }} · 容量 {{ route.capacity }}</span></div>
      <div class="stat"><span class="label">计划装载 / 车上实时</span><span class="value">{{ route.total_load }} / {{ route.onboard }} 辆</span><span class="extra">当前第 {{ route.current_seq }} 站</span></div>
      <div class="stat"><span class="label">生成依据</span><span class="value" style="font-size:15px">地铁取车 {{ route.subway_borrow_need }}</span><span class="extra">住宅积压 {{ route.residential_backlog }}</span></div>
      <div class="stat">
        <span class="label">高峰天气</span>
        <span class="value" style="font-size:15px">{{ route.weather ? route.weather.condition : '未关联' }}</span>
        <span class="extra">
          <template v-if="route.weather">{{ route.weather.temp_c }}℃ · {{ route.weather.wind_level }}
            <span v-if="route.weather.alert_level && route.weather.alert_level!=='无'" class="badge warn">{{ route.weather.alert_level }}预警</span></template>
          <a v-if="canExec" class="back-link" @click="openWeather">{{ route.weather ? '更新' : '录入' }}</a>
        </span>
      </div>
    </div>

    <div class="alert info mb" style="display:block">{{ route.explanation }}</div>

    <!-- 全局操作 -->
    <div class="flex mb" style="gap:8px">
      <button v-if="canExec && route.status==='planned'" class="btn" @click="openAssign">派车</button>
      <button v-if="canExec && route.status==='assigned'" class="btn" @click="start">出车开始执行</button>
      <button v-if="canExec && route.status==='completed' && !hasReview" class="btn ok" @click="openReview">发起调拨复盘</button>
      <span v-if="route.status==='executing'" class="small muted"><i class="pulse-dot"></i> 执行中：站点库存、车辆状态、预计到达每 5 秒实时刷新</span>
    </div>

    <!-- 路线流程图 -->
    <div class="card mb">
      <h3><span class="dot"></span>路线与实时执行</h3>
      <div class="route-flow">
        <template v-for="(s,i) in route.stops" :key="s.id">
          <div class="rf-stop" :class="{ active: route.status==='executing' && s.seq===route.current_seq, done: s.status==='done' }">
            <div class="rf-seq">{{ s.seq }}</div>
            <div class="rf-kind" :class="s.kind">{{ s.kind==='pickup'?'装车':'卸车' }}</div>
            <div class="rf-name">{{ s.station }}</div>
            <div class="rf-qty">{{ s.kind==='pickup'?'取':'补' }} {{ s.planned_load }}<span v-if="s.status==='done'"> / 实 {{ s.actual_load }}</span></div>
            <div class="rf-time">
              <span v-if="s.status==='pending'">预计 {{ fmt(s.eta) }}</span>
              <span v-else>到达 {{ fmt(s.actual_arrival) }}</span>
            </div>
          </div>
          <div v-if="i < route.stops.length-1" class="rf-arrow" :class="{ done: s.status==='done' }">→</div>
        </template>
      </div>
    </div>

    <!-- 站点明细 -->
    <div class="card mb">
      <h3><span class="dot"></span>站点库存与偏离</h3>
      <table class="tbl">
        <thead><tr><th>#</th><th>站点 / 类型</th><th>计划</th><th>实际</th><th>预计/实际到达</th><th>站点实时库存</th><th>状态 / 偏离</th><th v-if="canExec">操作</th></tr></thead>
        <tbody>
          <tr v-for="s in route.stops" :key="s.id">
            <td>{{ s.seq }}</td>
            <td><b>{{ s.station }}</b><div class="small muted">{{ s.kind==='pickup'?'住宅区装车点（还车积压）':'地铁口卸车点（取车需求）' }}</div></td>
            <td>{{ s.kind==='pickup'?'-':'+' }}{{ s.planned_load }} 辆</td>
            <td>{{ s.status==='done' ? (s.kind==='pickup'?'-':'+')+s.actual_load+' 辆' : '—' }}</td>
            <td class="small">
              计划 {{ fmt(s.eta) }}<br>
              <span :class="s.deviated?'txt-danger':''">{{ s.actual_arrival ? '实际 '+fmt(s.actual_arrival) : '未到达' }}</span>
            </td>
            <td class="small">
              在桩 <b>{{ s.available_bikes }}</b> · 占用 {{ s.used_docks }}/{{ s.capacity }}<span v-if="s.fault_bikes"> · 故障 {{ s.fault_bikes }}</span>
              <div class="inv-bar"><div class="inv-fill" :style="{ width: fillPct(s)+'%' }" :class="invClass(s)"></div></div>
            </td>
            <td>
              <span class="badge" :class="stopBadge(s.status)">{{ stopName(s.status) }}</span>
              <div v-if="s.deviated" class="mt">
                <span class="badge danger">{{ devName(s.deviation_type) }}</span>
                <span v-if="s.exemption" class="badge ok">客观原因·免考核</span>
              </div>
              <div v-if="s.deviation_reason" class="small mt">原因：{{ s.deviation_reason }}</div>
              <div v-else-if="s.deviated && canExec" class="small txt-danger mt">待补充原因</div>
              <div v-if="s.impact" class="small muted mt">影响：{{ s.impact }}</div>
            </td>
            <td v-if="canExec">
              <div class="flex" style="gap:6px">
                <button v-if="isCurrent(s) && s.status==='pending'" class="btn sm" @click="arrive(s)">到达本站</button>
                <button v-if="isCurrent(s) && s.status==='arrived'" class="btn sm ok" @click="execute(s)">
                  {{ s.kind==='pickup'?'确认装车':'确认卸车补车' }}
                </button>
                <button v-if="s.deviated" class="btn sm ghost" @click="openDeviation(s)">{{ s.deviation_reason?'修改原因':'补充原因' }}</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 复盘结果 -->
    <div v-if="hasReview" class="card mb">
      <h3><span class="dot"></span>调拨复盘（已关联高峰天气）</h3>
      <div class="grid grid-4 mb">
        <div class="stat"><span class="label">准点率</span><span class="value">{{ Math.round(review.on_time_rate*100) }}%</span></div>
        <div class="stat"><span class="label">偏离 / 拥堵豁免</span><span class="value">{{ review.deviation_count }} / {{ review.exempt_count }}</span></div>
        <div class="stat"><span class="label">计划/实际补车</span><span class="value">{{ review.planned_total }}/{{ review.actual_total }}<span v-if="review.shortage" class="txt-danger"> 少补{{review.shortage}}</span></span></div>
        <div class="stat"><span class="label">司机考核结论</span><span class="value" style="font-size:15px"><span class="badge" :class="review.driver_assessment==='exempt'?'ok':(review.driver_assessment==='accountable'?'danger':'gray')">{{ assessName(review.driver_assessment) }}</span></span></div>
      </div>
      <div class="tag-row mb">
        <span v-for="c in review.causes" :key="c" class="badge" :class="c==='congestion'?'ok':'warn'">{{ causeName(c) }}</span>
      </div>
      <div class="alert ok" style="display:block"><b>改进建议（用于优化下一次路线）：</b>{{ review.improvement }}</div>
    </div>

    <!-- 派车 -->
    <div v-if="assignOpen" class="modal-mask" @click.self="assignOpen=false">
      <div class="modal">
        <h3>路线 {{ route.code }} 派车</h3>
        <p class="small muted mb">本路线计划装载 {{ route.total_load }} 辆，调拨车容量须不小于该值。</p>
        <div class="form-item mb">
          <label>选择调拨车（空闲）</label>
          <select class="input" v-model.number="truckId">
            <option :value="0" disabled>请选择</option>
            <option v-for="t in idleTrucks" :key="t.id" :value="t.id">{{ t.plate }} · 容量 {{ t.capacity }} · 司机 {{ t.driver || '未排班' }}</option>
          </select>
        </div>
        <div class="flex"><button class="btn" :disabled="!truckId" @click="assign">确认派车</button><button class="btn ghost" @click="assignOpen=false">取消</button></div>
      </div>
    </div>

    <!-- 偏离补充 -->
    <div v-if="devStop" class="modal-mask" @click.self="devStop=null">
      <div class="modal">
        <h3>补充路线偏离原因 · {{ devStop.station }}</h3>
        <div class="alert warn mb" style="display:block">{{ devStop.impact || '该站到达/装卸偏离计划，可能影响后续站点补车。' }}</div>
        <div class="form-item mb">
          <label>偏离类型</label>
          <select class="input" v-model="devForm.deviation_type">
            <option value="late">晚点到达</option>
            <option value="early">过早到达</option>
            <option value="short">装卸数量不足</option>
            <option value="skipped">跳过站点</option>
          </select>
        </div>
        <div class="form-item mb">
          <label>偏离原因（进入调拨复盘）</label>
          <textarea class="input" rows="3" v-model="devForm.reason" placeholder="如：江东大道早高峰道路拥堵，信号灯排队 12 分钟"></textarea>
        </div>
        <div class="form-item mb">
          <label><input type="checkbox" v-model="devForm.exemption" /> 道路拥堵 / 交通管制 / 恶劣天气等客观原因，申请免予司机考核</label>
          <div class="small muted">勾选后平台校验原因关键词；非客观原因不能豁免，避免司机被错误考核的同时防止滥用。</div>
        </div>
        <div class="flex"><button class="btn" @click="submitDeviation">提交</button><button class="btn ghost" @click="devStop=null">取消</button></div>
      </div>
    </div>

    <!-- 复盘 -->
    <div v-if="reviewOpen" class="modal-mask" @click.self="reviewOpen=false">
      <div class="modal">
        <h3>调拨复盘 · {{ route.code }}</h3>
        <p class="small muted mb">系统自动汇总准点率、各站偏离与豁免、少补车辆，并自动关联当日高峰天气；改进建议留空则由平台按偏离原因生成。</p>
        <div v-if="route.weather" class="alert info mb" style="display:block">将关联高峰天气：{{ route.weather.condition }} {{ route.weather.alert_level!=='无' ? route.weather.alert_level+'预警' : '' }}（{{ route.plan_date }} {{ route.peak_type==='morning'?'早高峰':'晚高峰' }}）</div>
        <div v-else class="alert warn mb" style="display:block">当日高峰天气尚未录入，可先在右上角录入，复盘将自动关联。</div>
        <div class="form-item mb">
          <label>改进建议（可选）</label>
          <textarea class="input" rows="3" v-model="improvement" placeholder="留空由平台按偏离原因自动生成，用于优化下一次路线"></textarea>
        </div>
        <div class="flex"><button class="btn ok" @click="submitReview">提交复盘</button><button class="btn ghost" @click="reviewOpen=false">取消</button></div>
      </div>
    </div>

    <!-- 天气录入 -->
    <div v-if="weatherOpen" class="modal-mask" @click.self="weatherOpen=false">
      <div class="modal">
        <h3>录入高峰天气 · {{ route.plan_date }} {{ route.peak_type==='morning'?'早高峰':'晚高峰' }}</h3>
        <div class="form-row">
          <div class="form-item"><label>天气</label>
            <select class="input" v-model="wform.condition">
              <option>晴</option><option>多云</option><option>阴</option><option>小雨</option><option>中雨</option><option>大雨</option><option>雷阵雨</option><option>雾</option><option>小雪</option>
            </select>
          </div>
          <div class="form-item"><label>气温 ℃</label><input class="input" type="number" v-model.number="wform.temp_c" /></div>
        </div>
        <div class="form-row">
          <div class="form-item"><label>风力</label><input class="input" v-model="wform.wind_level" placeholder="如 东风2级" /></div>
          <div class="form-item"><label>预警等级</label>
            <select class="input" v-model="wform.alert_level"><option>无</option><option>蓝色</option><option>黄色</option><option>橙色</option><option>红色</option></select>
          </div>
        </div>
        <div class="form-item mb"><label>天气摘要</label><textarea class="input" rows="2" v-model="wform.summary"></textarea></div>
        <div class="flex"><button class="btn" @click="submitWeather">保存</button><button class="btn ghost" @click="weatherOpen=false">取消</button></div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { get, post, getUser } from '../api'
import { toast } from '../components/Toast.vue'

const r = useRoute()
const route = ref(null)
const trucks = ref([])
const canExec = ['dispatcher', 'operator'].includes(getUser()?.role)

const assignOpen = ref(false)
const truckId = ref(0)
const devStop = ref(null)
const devForm = ref({ deviation_type: 'late', reason: '', exemption: false })
const reviewOpen = ref(false)
const improvement = ref('')
const weatherOpen = ref(false)
const wform = ref({ condition: '晴', temp_c: 24, wind_level: '', alert_level: '无', summary: '' })

let timer = null
const review = computed(() => route.value?.review || {})
const hasReview = computed(() => !!(route.value?.review && route.value.review.id))
const idleTrucks = computed(() => trucks.value.filter(t => t.status === 'idle'))

function routeBadge(s) { return { planned: 'warn', assigned: 'info', executing: 'purple', completed: 'ok', cancelled: 'gray' }[s] || 'gray' }
function stopBadge(s) { return { pending: 'gray', arrived: 'purple', done: 'ok', skipped: 'danger' }[s] || 'gray' }
function stopName(s) { return { pending: '待到达', arrived: '已到达待装卸', done: '已完成', skipped: '已跳过' }[s] || s }
function devName(t) { return { late: '晚点', early: '早到', short: '装卸不足', skipped: '跳过站点' }[t] || t }
function causeName(c) { return { congestion: '道路拥堵（客观）', late: '晚点', early: '早到', short: '装卸不足' }[c] || c }
function assessName(a) { return { normal: '准点无偏离', exempt: '客观原因豁免，不计考核', accountable: '存在非客观偏离，计入考核' }[a] || a }
function fmt(t) { return t ? new Date(t).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }) : '—' }
function isCurrent(s) { return route.value.status === 'executing' && s.seq === route.value.current_seq }
function fillPct(s) { return s.capacity ? Math.min(100, Math.round(s.used_docks / s.capacity * 100)) : 0 }
function invClass(s) { if (s.used_docks >= s.capacity) return 'bg-danger'; if (s.available_bikes <= 1) return 'bg-warn'; return 'bg-ok' }

async function load() {
  try {
    route.value = await get('/peak-routes/' + r.params.id)
  } catch (e) { toast(e.message, true) }
}
async function loadTrucks() { try { trucks.value = await get('/trucks') } catch (e) { /* */ } }

function openAssign() { truckId.value = 0; assignOpen.value = true; loadTrucks() }
async function assign() {
  try {
    toast((await post(`/peak-routes/${route.value.id}/assign`, { truck_id: truckId.value })).message)
    assignOpen.value = false; await load()
  } catch (e) { toast(e.message, true) }
}
async function start() {
  try { toast((await post(`/peak-routes/${route.value.id}/start`, {})).message); await load() }
  catch (e) { toast(e.message, true) }
}
async function arrive(s) {
  try {
    const res = await post(`/peak-routes/${route.value.id}/stops/${s.seq}/arrive`, {})
    toast(res.message)
    await load()
  } catch (e) { toast(e.message, true) }
}
async function execute(s) {
  try {
    const res = await post(`/peak-routes/${route.value.id}/stops/${s.seq}/execute`, {})
    toast(res.message)
    await load()
  } catch (e) { toast(e.message, true) }
}
function openDeviation(s) {
  devStop.value = s
  devForm.value = { deviation_type: s.deviation_type || 'late', reason: s.deviation_reason || '', exemption: !!s.exemption }
}
async function submitDeviation() {
  if (!devForm.value.reason.trim()) { toast('请填写偏离原因', true); return }
  try {
    toast((await post(`/peak-routes/${route.value.id}/stops/${devStop.value.seq}/deviation`, devForm.value)).message)
    devStop.value = null; await load()
  } catch (e) { toast(e.message, true) }
}
function openReview() { improvement.value = ''; reviewOpen.value = true }
async function submitReview() {
  try {
    toast((await post(`/peak-routes/${route.value.id}/review`, { improvement: improvement.value })).message)
    reviewOpen.value = false; await load()
  } catch (e) { toast(e.message, true) }
}
function openWeather() {
  const w = route.value.weather
  wform.value = w
    ? { condition: w.condition, temp_c: w.temp_c, wind_level: w.wind_level, alert_level: w.alert_level || '无', summary: w.summary || '' }
    : { condition: '晴', temp_c: 24, wind_level: '', alert_level: '无', summary: '' }
  weatherOpen.value = true
}
async function submitWeather() {
  try {
    await post('/peak-weather', { peak_date: route.value.plan_date, peak_type: route.value.peak_type, ...wform.value })
    toast('高峰天气已保存'); weatherOpen.value = false; await load()
  } catch (e) { toast(e.message, true) }
}

onMounted(async () => {
  await load()
  timer = setInterval(() => { if (route.value && route.value.status === 'executing') load() }, 5000)
})
onUnmounted(() => timer && clearInterval(timer))
</script>

<style scoped>
.back-link { color: #1668dc; cursor: pointer; }
.route-flow { display: flex; align-items: stretch; flex-wrap: wrap; gap: 6px; }
.rf-stop { flex: 1 1 150px; min-width: 150px; border: 1px solid #e3e8f0; border-radius: 10px; padding: 10px; background: #f8fafc; position: relative; }
.rf-stop.active { border-color: #1668dc; box-shadow: 0 0 0 2px rgba(22,104,220,.15); background: #f0f6ff; }
.rf-stop.done { background: #f0fbf4; border-color: #b7e4c7; }
.rf-seq { position: absolute; top: 6px; right: 8px; font-size: 12px; color: #94a3b8; }
.rf-kind { display: inline-block; font-size: 12px; padding: 1px 8px; border-radius: 10px; margin-bottom: 4px; }
.rf-kind.pickup { background: #fff3e0; color: #b25e09; }
.rf-kind.dropoff { background: #e7f1ff; color: #1668dc; }
.rf-name { font-weight: 600; font-size: 13px; }
.rf-qty { font-size: 12px; color: #475569; margin-top: 2px; }
.rf-time { font-size: 12px; color: #64748b; margin-top: 2px; }
.rf-arrow { align-self: center; color: #94a3b8; font-size: 18px; }
.rf-arrow.done { color: #18a058; }
.inv-bar { height: 5px; background: #eef2f7; border-radius: 3px; margin-top: 4px; overflow: hidden; }
.inv-fill { height: 100%; border-radius: 3px; }
.bg-ok { background: #18a058; } .bg-warn { background: #e6a23c; } .bg-danger { background: #d03050; }
.pulse-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #1668dc; margin-right: 5px; animation: pulse 1.4s infinite; }
@keyframes pulse { 0% { box-shadow: 0 0 0 0 rgba(22,104,220,.5);} 70% { box-shadow: 0 0 0 7px rgba(22,104,220,0);} 100% { box-shadow: 0 0 0 0 rgba(22,104,220,0);} }
.ok-green { color: #18a058; }
</style>
