<template>
  <div>
    <div class="between mb">
      <div>
        <div class="page-title">高峰调拨路线</div>
        <div class="page-sub">早高峰前按地铁口取车需求 · 住宅区还车积压 · 调拨车容量生成多站路线；执行时站点库存 / 车辆状态 / 预计到达实时更新</div>
      </div>
      <button v-if="canPlan" class="btn" @click="showPlan = true">＋ 生成高峰路线</button>
    </div>

    <div class="flex mb" style="gap:8px">
      <button class="btn sm" :class="tab!=='routes' && 'ghost'" @click="switchTab('routes')">路线</button>
      <button class="btn sm" :class="tab!=='deviations' && 'ghost'" @click="switchTab('deviations')">偏离台账</button>
      <button class="btn sm" :class="tab!=='reviews' && 'ghost'" @click="switchTab('reviews')">调拨复盘</button>
    </div>

    <!-- 路线列表 -->
    <div v-if="tab==='routes'" class="card">
      <h3><span class="dot"></span>高峰路线</h3>
      <table class="tbl" v-if="routes.length">
        <thead><tr><th>路线编号</th><th>名称 / 日期</th><th>站点</th><th>计划/在车</th><th>调拨车 / 司机</th><th>状态</th><th>偏离</th><th>复盘</th><th></th></tr></thead>
        <tbody>
          <tr v-for="r in routes" :key="r.id">
            <td><b>{{ r.code }}</b><div class="small muted">{{ r.peak_type==='morning'?'早高峰':'晚高峰' }}</div></td>
            <td>{{ r.name }}<div class="small muted">{{ r.plan_date }}</div></td>
            <td>{{ r.stop_count }} 站</td>
            <td>{{ r.total_load }} / {{ r.onboard }} 辆</td>
            <td class="small">{{ r.truck || '未派车' }}<br>{{ r.driver || '' }}</td>
            <td><span class="badge" :class="routeBadge(r.status)">{{ r.status_name }}</span></td>
            <td><span v-if="r.deviation_count" class="badge danger">{{ r.deviation_count }} 次</span><span v-else class="muted small">—</span></td>
            <td><span class="badge" :class="r.reviewed?'ok':'gray'">{{ r.reviewed?'已复盘':'未复盘' }}</span></td>
            <td><button class="btn sm ghost" @click="$router.push('/peak-routes/'+r.id)">查看路线</button></td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">暂无高峰路线，早高峰前点击右上角生成</div>
    </div>

    <!-- 偏离台账 -->
    <div v-if="tab==='deviations'" class="card">
      <h3><span class="dot"></span>偏离台账（原因进入复盘，用于优化下一次路线）</h3>
      <table class="tbl" v-if="deviations.length">
        <thead><tr><th>路线</th><th>站点</th><th>类型</th><th>补充原因</th><th>豁免</th><th>对后续补车影响</th><th>高峰天气</th></tr></thead>
        <tbody>
          <tr v-for="(d,i) in deviations" :key="i">
            <td class="small">{{ d.route }}<br><span class="muted">{{ d.peak_type==='morning'?'早高峰':'晚高峰' }}</span></td>
            <td>{{ d.station }}<div class="small muted">{{ d.kind==='pickup'?'装车点':'卸车点' }}</div></td>
            <td><span class="badge warn">{{ d.deviation_type_name }}</span></td>
            <td class="small">{{ d.reason || '（待调度员补充）' }}</td>
            <td><span v-if="d.exemption" class="badge ok">客观原因·免考核</span><span v-else class="badge gray">计司机考核</span></td>
            <td class="small">{{ d.impact || '—' }}</td>
            <td class="small">{{ d.weather || '—' }}<span v-if="d.weather_alert && d.weather_alert!=='无'" class="badge warn">{{ d.weather_alert }}</span></td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">暂无偏离记录</div>
    </div>

    <!-- 复盘列表 -->
    <div v-if="tab==='reviews'" class="card">
      <h3><span class="dot"></span>调拨复盘（关联高峰天气）</h3>
      <table class="tbl" v-if="reviews.length">
        <thead><tr><th>路线 / 日期</th><th>高峰天气</th><th>准点率</th><th>偏离/豁免</th><th>计划/实际补车</th><th>司机考核</th><th>改进建议（用于下次路线）</th></tr></thead>
        <tbody>
          <tr v-for="rv in reviews" :key="rv.id">
            <td class="small"><b>{{ rv.code }}</b><br><span class="muted">{{ rv.plan_date }} {{ rv.peak_type==='morning'?'早高峰':'晚高峰' }}</span></td>
            <td class="small">{{ rv.weather_condition || '—' }}<span v-if="rv.weather_alert && rv.weather_alert!=='无'" class="badge warn">{{ rv.weather_alert }}</span><div class="muted">{{ rv.wind }}</div></td>
            <td><b :class="rv.on_time_rate>=0.8?'ok-green':'txt-danger'">{{ Math.round(rv.on_time_rate*100) }}%</b></td>
            <td>{{ rv.deviation_count }} 次 / 豁免 {{ rv.exempt_count }} 次</td>
            <td>{{ rv.planned_total }} / {{ rv.actual_total }} 辆<span v-if="rv.shortage" class="small muted">（少补 {{ rv.shortage }}）</span></td>
            <td><span class="badge" :class="rv.driver_assessment==='exempt'?'ok':(rv.driver_assessment==='accountable'?'danger':'gray')">{{ rv.driver_assessment_name }}</span></td>
            <td class="small">{{ rv.improvement }}</td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">暂无复盘记录</div>
    </div>

    <!-- 生成路线 -->
    <div v-if="showPlan" class="modal-mask" @click.self="showPlan=false">
      <div class="modal">
        <h3>生成高峰调拨路线</h3>
        <div class="form-item mb">
          <label>高峰类型</label>
          <select class="input" v-model="plan.peak_type">
            <option value="morning">早高峰（住宅区还车积压 → 地铁口取车补车）</option>
            <option value="evening">晚高峰（商圈/地铁口 → 社区补车）</option>
          </select>
        </div>
        <div class="form-row">
          <div class="form-item">
            <label>调拨车容量（0=按空闲车最小容量）</label>
            <input class="input" type="number" min="0" max="40" v-model.number="plan.capacity" />
          </div>
          <div class="form-item">
            <label>最多装车点</label>
            <input class="input" type="number" min="1" max="5" v-model.number="plan.max_pickups" />
          </div>
          <div class="form-item">
            <label>最多卸车点</label>
            <input class="input" type="number" min="1" max="5" v-model.number="plan.max_dropoffs" />
          </div>
        </div>
        <p class="small muted mt">生成依据：7 点（晚高峰 18 点）分时段需求预测、住宅区还车积压、地铁口取车缺口、调拨车容量；并依据历史复盘的拥堵次数与今日高峰天气自动预留行车缓冲。</p>
        <div class="flex">
          <button class="btn" @click="generate" :disabled="generating">{{ generating?'计算中…':'生成路线' }}</button>
          <button class="btn ghost" @click="showPlan=false">关闭</button>
        </div>
        <div v-if="planResult" class="alert" :class="planResult.id?'ok':'info'" style="display:block">{{ planResult.message }}</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { get, post, getUser } from '../api'
import { toast } from '../components/Toast.vue'

const tab = ref('routes')
const routes = ref([])
const deviations = ref([])
const reviews = ref([])
const showPlan = ref(false)
const generating = ref(false)
const planResult = ref(null)
const plan = ref({ peak_type: 'morning', capacity: 0, max_pickups: 3, max_dropoffs: 3 })
const canPlan = ['dispatcher', 'operator'].includes(getUser()?.role)

function routeBadge(s) {
  return { planned: 'warn', assigned: 'info', executing: 'purple', completed: 'ok', cancelled: 'gray' }[s] || 'gray'
}

async function loadRoutes() { routes.value = await get('/peak-routes') }
async function loadDeviations() { deviations.value = await get('/peak-routes/deviations') }
async function loadReviews() { reviews.value = await get('/peak-routes/reviews') }

async function switchTab(t) {
  tab.value = t
  try {
    if (t === 'routes') await loadRoutes()
    if (t === 'deviations') await loadDeviations()
    if (t === 'reviews') await loadReviews()
  } catch (e) { toast(e.message, true) }
}

async function generate() {
  generating.value = true
  planResult.value = null
  try {
    planResult.value = await post('/peak-routes/plan', plan.value)
    await loadRoutes()
  } catch (e) { toast(e.message, true) } finally { generating.value = false }
}

onMounted(() => loadRoutes().catch(e => toast(e.message, true)))
</script>
