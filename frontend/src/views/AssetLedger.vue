<template>
  <div>
    <div class="page-title">重复故障报废评估 · 车辆资产台账</div>
    <div class="page-sub">同一车辆多次出现刹车 / 车锁 / 轮胎问题时，汇总维修记录、骑行里程与配件成本；维修主管裁决继续维修 / 限制投放 / 报废，并联动资产台账、采购计划与站点可用车预测</div>

    <div class="flex mb" style="gap:8px; flex-wrap:wrap">
      <button class="btn sm" :class="tab!=='assess' && 'ghost'" @click="switchTab('assess')">重复故障评估</button>
      <button class="btn sm" :class="tab!=='forecast' && 'ghost'" @click="switchTab('forecast')">站点可用车预测</button>
      <button class="btn sm" :class="tab!=='reviews' && 'ghost'" @click="switchTab('reviews')">
        资产复核（维修仓）<span v-if="openReviews" class="badge danger">{{ openReviews }}</span>
      </button>
      <button class="btn sm" :class="tab!=='ledger' && 'ghost'" @click="switchTab('ledger')">资产台账</button>
      <button class="btn sm" :class="tab!=='procure' && 'ghost'" @click="switchTab('procure')">采购计划</button>
    </div>

    <!-- 重复故障评估 -->
    <div v-if="tab==='assess'">
      <div class="card mb">
        <h3><span class="dot"></span>重复故障候选车辆（刹车 / 车锁 / 轮胎）</h3>
        <table class="tbl" v-if="candidates.length">
          <thead><tr><th>车辆</th><th>刹车</th><th>车锁</th><th>轮胎</th><th>维修/重复</th><th>里程 km</th><th>配件成本</th><th>工时成本</th><th>系统建议</th><th>状态</th><th v-if="isLead">操作</th></tr></thead>
          <tbody>
            <tr v-for="c in candidates" :key="c.bike_id">
              <td class="mono">{{ c.bike_code }}<div class="small muted">{{ bikeStatusName(c.bike_status) }}</div></td>
              <td :class="c.brake_count>=2?'txt-danger':''">{{ c.brake_count }}</td>
              <td :class="c.lock_count>=2?'txt-danger':''">{{ c.lock_count }}</td>
              <td :class="c.tire_count>=2?'txt-danger':''">{{ c.tire_count }}</td>
              <td>{{ c.repair_count }} / <span :class="c.repeat_count?'txt-danger':''">{{ c.repeat_count }}</span></td>
              <td>{{ Math.round(c.mileage_km) }}</td>
              <td>¥{{ c.parts_cost.toFixed(0) }}</td>
              <td>¥{{ c.labor_cost.toFixed(0) }}<div class="small muted">合计 ¥{{ c.total_cost.toFixed(0) }}</div></td>
              <td><span class="badge" :class="recBadge(c.recommendation)">{{ c.recommendation_name }}</span></td>
              <td>
                <span v-if="c.open_assessment" class="badge purple">评估中</span>
                <span v-else-if="c.last_decision" class="badge" :class="recBadge(c.last_decision)">{{ decisionName(c.last_decision) }}</span>
                <span v-else class="muted small">未评估</span>
              </td>
              <td v-if="isLead">
                <button v-if="!c.open_assessment" class="btn sm" @click="createAssess(c)">生成评估单</button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-else class="empty">暂无重复故障候选车辆</div>
      </div>

      <div class="card">
        <h3><span class="dot"></span>报废评估单</h3>
        <table class="tbl" v-if="assessments.length">
          <thead><tr><th>#</th><th>车辆</th><th>关键故障(刹/锁/胎)</th><th>里程</th><th>累计成本</th><th>系统建议</th><th>裁决</th><th>主管</th><th>采购</th><th v-if="isLead">操作</th></tr></thead>
          <tbody>
            <tr v-for="a in assessments" :key="a.id">
              <td>{{ a.id }}</td>
              <td class="mono">{{ a.bike_code }}</td>
              <td>{{ a.brake_count }}/{{ a.lock_count }}/{{ a.tire_count }}<span v-if="a.repeat_count" class="badge danger" style="margin-left:4px">重复{{ a.repeat_count }}</span></td>
              <td>{{ Math.round(a.mileage_km) }} km</td>
              <td>¥{{ a.total_cost.toFixed(0) }}<div class="small muted">配件 ¥{{ a.parts_cost.toFixed(0) }} · 工时 ¥{{ a.labor_cost.toFixed(0) }}</div></td>
              <td><span class="badge" :class="recBadge(a.recommendation)">{{ a.recommendation_name }}</span></td>
              <td>
                <span v-if="a.decision==='pending'" class="badge warn">待裁决</span>
                <span v-else class="badge" :class="recBadge(a.decision)">{{ a.decision_name }}</span>
                <div v-if="a.decision_reason" class="small muted">{{ a.decision_reason }}</div>
              </td>
              <td class="small">{{ a.decided_by || '—' }}</td>
              <td><span v-if="a.procurement_id" class="badge info">采购 #{{ a.procurement_id }}</span><span v-else class="small muted">—</span></td>
              <td v-if="isLead">
                <button class="btn sm ghost" @click="openDetail(a)">详情</button>
                <button v-if="a.decision==='pending'" class="btn sm ok" @click="openDecide(a)">裁决</button>
              </td>
              <td v-else><button class="btn sm ghost" @click="openDetail(a)">详情</button></td>
            </tr>
          </tbody>
        </table>
        <div v-else class="empty">暂无评估单</div>
      </div>
    </div>

    <!-- 站点可用车预测 -->
    <div v-if="tab==='forecast'" class="card">
      <h3><span class="dot"></span>站点可用车预测（剔除报废 / 限投 / 故障 / 在修，防止账面有车现场无车）</h3>
      <table class="tbl" v-if="forecast.length">
        <thead><tr><th>站点</th><th>类型</th><th>实际可用</th><th>占用桩</th><th>故障待修/账实异常</th><th>未来3h 借/还</th><th>预测可用</th><th>缺口</th><th>账实核对</th></tr></thead>
        <tbody>
          <tr v-for="f in forecast" :key="f.station_id">
            <td>{{ f.station }}</td>
            <td class="small">{{ typeName(f.type) }}</td>
            <td><b :class="f.usable_bikes<=1?'txt-danger':''">{{ f.usable_bikes }}</b> / {{ f.capacity }}</td>
            <td>{{ f.occupied_docks }}</td>
            <td><span :class="f.fault_on_dock?'':''">{{ f.fault_on_dock }}</span> / <span :class="f.asset_phantom?'txt-danger':''">{{ f.asset_phantom }}</span></td>
            <td class="small">{{ f.borrow_need_3h }} / {{ f.return_need_3h }}</td>
            <td :class="f.projected_avail<0?'txt-danger':''">{{ f.projected_avail }}</td>
            <td><span v-if="f.shortage" class="badge danger">缺 {{ f.shortage }}</span><span v-else class="small muted">—</span></td>
            <td><span v-if="f.book_site_mismatch" class="badge danger">账面异常·待复核</span><span v-else class="badge ok">一致</span></td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 资产复核（维修仓） -->
    <div v-if="tab==='reviews'" class="card">
      <h3><span class="dot"></span>资产状态复核（报废车仍在站点时自动触发并通知维修仓）</h3>
      <table class="tbl" v-if="reviews.length">
        <thead><tr><th>#</th><th>车辆</th><th>站点</th><th>原因</th><th>维修仓通知</th><th>状态</th><th>处理记录</th><th v-if="canWarehouse">操作</th></tr></thead>
        <tbody>
          <tr v-for="rv in reviews" :key="rv.id">
            <td>{{ rv.id }}</td>
            <td class="mono">{{ rv.bike_code }}</td>
            <td>{{ rv.station || '—' }}</td>
            <td class="small">{{ rv.detail }}</td>
            <td><span class="badge" :class="rv.notified_warehouse?'ok':'gray'">{{ rv.notified_warehouse?'已通知维修仓':'未通知' }}</span></td>
            <td><span class="badge" :class="rv.status==='open'?'danger':(rv.status==='resolved'?'ok':'warn')">{{ rv.status_name }}</span></td>
            <td class="small">{{ rv.handler ? rv.handler+'：' : '' }}{{ rv.warehouse_note || '—' }}</td>
            <td v-if="canWarehouse">
              <div class="flex" style="gap:6px">
                <button v-if="rv.status==='open'" class="btn sm" @click="handleReview(rv,'acknowledge')">受理</button>
                <button v-if="rv.status!=='resolved'" class="btn sm ok" @click="handleReview(rv,'resolve')">现场回收销账</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">暂无资产复核记录</div>
    </div>

    <!-- 资产台账 -->
    <div v-if="tab==='ledger'" class="card">
      <h3><span class="dot"></span>车辆资产台账（裁决结果即时影响资产状态）</h3>
      <table class="tbl" v-if="ledger.length">
        <thead><tr><th>资产编号</th><th>车辆</th><th>资产状态</th><th>购置日期</th><th>购置价</th><th>累计配件</th><th>累计维修</th><th>里程</th><th>残值/账面净值</th><th>所在站</th></tr></thead>
        <tbody>
          <tr v-for="a in ledger" :key="a.id">
            <td class="small mono">{{ a.bike_code && ('ZC-'+a.bike_code) }}</td>
            <td class="mono">{{ a.bike_code }}</td>
            <td><span class="badge" :class="a.status==='scrapped'?'danger':(a.status==='restricted'?'warn':'ok')">{{ a.status_name }}</span></td>
            <td class="small">{{ a.purchase_date }}</td>
            <td>¥{{ a.purchase_price.toFixed(0) }}</td>
            <td>¥{{ a.accum_parts_cost.toFixed(0) }}</td>
            <td>¥{{ a.accum_repair_cost.toFixed(0) }}</td>
            <td>{{ Math.round(a.mileage_km) }}</td>
            <td class="small">残值 ¥{{ a.salvage_value.toFixed(0) }} / 净值 ¥{{ a.book_value.toFixed(0) }}</td>
            <td class="small">{{ a.station || '不在站' }}</td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">暂无资产台账记录（裁决后自动建档）</div>
    </div>

    <!-- 采购计划 -->
    <div v-if="tab==='procure'" class="card">
      <h3><span class="dot"></span>采购计划（报废决定自动同步补货）</h3>
      <table class="tbl" v-if="procure.length">
        <thead><tr><th>#</th><th>对应报废车</th><th>数量</th><th>原因</th><th>状态</th><th>生成时间</th></tr></thead>
        <tbody>
          <tr v-for="p in procure" :key="p.id">
            <td>{{ p.id }}</td><td class="mono">{{ p.bike_code || '—' }}</td><td>{{ p.qty }}</td>
            <td class="small">{{ p.reason }}</td>
            <td><span class="badge" :class="p.status==='planned'?'warn':'ok'">{{ p.status_name }}</span></td>
            <td class="small muted">{{ fmt(p.created_at) }}</td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">暂无采购计划</div>
    </div>

    <!-- 裁决弹窗 -->
    <div v-if="decideItem" class="modal-mask" @click.self="decideItem=null">
      <div class="modal">
        <h3>报废评估裁决 · {{ decideItem.bike_code }}</h3>
        <div class="alert" :class="recAlert(decideItem.recommendation)" style="display:block">
          系统建议：<b>{{ decideItem.recommendation_name }}</b>
          （刹车 {{ decideItem.brake_count }} / 车锁 {{ decideItem.lock_count }} / 轮胎 {{ decideItem.tire_count }}，
          重复 {{ decideItem.repeat_count }}，里程 {{ Math.round(decideItem.mileage_km) }}km，累计成本 ¥{{ decideItem.total_cost.toFixed(0) }}）
        </div>
        <div class="form-item mb">
          <label>裁决结果</label>
          <select class="input" v-model="decideForm.decision">
            <option value="continue">继续维修（保留在役）</option>
            <option value="restrict">限制投放（退出可用车、观察）</option>
            <option value="scrap">报废（退役资产 + 同步采购 + 移出预测）</option>
          </select>
        </div>
        <div class="form-item mb">
          <label>裁决理由（进入车辆资产台账）</label>
          <textarea class="input" rows="3" v-model="decideForm.reason" placeholder="如：刹车两年内三次维修、累计成本已接近新车价，予以报废"></textarea>
        </div>
        <div class="small muted mb">报废后若车辆仍在站点桩位，系统将自动触发资产状态复核并通知维修仓现场回收。</div>
        <div class="flex"><button class="btn ok" @click="submitDecide">确认裁决</button><button class="btn ghost" @click="decideItem=null">取消</button></div>
      </div>
    </div>

    <!-- 评估详情弹窗 -->
    <div v-if="detail" class="modal-mask" @click.self="detail=null">
      <div class="modal" style="max-width:680px">
        <h3>评估单 #{{ detail.id }} · {{ detail.bike_code }}</h3>
        <div class="tag-row mb">
          <span class="badge danger">刹车 {{ detail.brake_count }}</span>
          <span class="badge warn">车锁 {{ detail.lock_count }}</span>
          <span class="badge info">轮胎 {{ detail.tire_count }}</span>
          <span class="badge purple">里程 {{ Math.round(detail.mileage_km) }}km</span>
          <span class="badge gray">配件 ¥{{ detail.parts_cost.toFixed(0) }}</span>
          <span class="badge gray">工时 ¥{{ detail.labor_cost.toFixed(0) }}</span>
        </div>
        <div class="timeline" style="max-height:46vh;overflow:auto">
          <div v-for="(h,i) in detail.records" :key="i" class="tl-item" :class="{ action: h.result==='fixed' }">
            <div class="tl-head">{{ fmt(h.at) }} · {{ h.type_name }}故障
              <span v-if="h.is_repeat" class="badge warn">重复故障</span>
              <span v-if="h.result==='scrapped'" class="badge danger">报废</span>
            </div>
            <div class="tl-body">
              <div>{{ h.description }}</div>
              <div v-if="h.result" class="small muted mt">维修用时 {{ h.duration_min }} 分钟 · 配件 {{ h.parts || '无' }} · {{ h.result==='fixed'?'已修复':'已报废' }}</div>
            </div>
          </div>
        </div>
        <div class="flex mt"><button class="btn ghost" @click="detail=null">关闭</button></div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { get, post, getUser } from '../api'
import { toast } from '../components/Toast.vue'

const tab = ref('assess')
const candidates = ref([])
const assessments = ref([])
const forecast = ref([])
const reviews = ref([])
const ledger = ref([])
const procure = ref([])
const decideItem = ref(null)
const detail = ref(null)
const decideForm = ref({ decision: 'scrap', reason: '' })

const role = getUser()?.role
const isLead = computed(() => ['repair_lead', 'operator'].includes(role))
const canWarehouse = computed(() => ['repair', 'repair_lead', 'operator'].includes(role))
const openReviews = computed(() => reviews.value.filter(r => r.status === 'open').length)

function bikeStatusName(s) { return { docked: '在桩', rented: '租用中', fault: '故障', in_repair: '维修中', in_transit: '调拨在途', cleaning: '清洗中', restricted: '限制投放', scrapped: '已报废' }[s] || s }
function decisionName(d) { return { pending: '待裁决', continue: '继续维修', restrict: '限制投放', scrap: '报废' }[d] || d }
function recBadge(d) { return { continue: 'ok', restrict: 'warn', scrap: 'danger' }[d] || 'gray' }
function recAlert(d) { return { continue: 'ok', restrict: 'warn', scrap: 'danger' }[d] || 'info' }
function typeName(t) { return { subway: '地铁口', school: '学校', business: '商圈', residential: '住宅区/社区' }[t] || t }
function fmt(t) { return t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : '—' }

async function loadAssess() {
  const [c, a] = await Promise.all([get('/assets/candidates'), get('/assets/assessments')])
  candidates.value = c; assessments.value = a
}
async function switchTab(t) {
  tab.value = t
  try {
    if (t === 'assess') await loadAssess()
    if (t === 'forecast') forecast.value = await get('/assets/forecast')
    if (t === 'reviews') reviews.value = await get('/assets/reviews')
    if (t === 'ledger') ledger.value = await get('/assets/ledger')
    if (t === 'procure') procure.value = await get('/assets/procurement')
  } catch (e) { toast(e.message, true) }
}
async function createAssess(c) {
  try {
    toast((await post('/assets/assessments', { bike_id: c.bike_id })).message)
    await loadAssess()
  } catch (e) { toast(e.message, true) }
}
function openDecide(a) { decideItem.value = a; decideForm.value = { decision: a.recommendation === 'continue' ? 'continue' : a.recommendation, reason: '' } }
async function submitDecide() {
  try {
    const res = await post(`/assets/assessments/${decideItem.value.id}/decide`, decideForm.value)
    toast(res.message); decideItem.value = null
    await Promise.all([loadAssess(), refreshReviewsIfOpen()])
  } catch (e) { toast(e.message, true) }
}
async function refreshReviewsIfOpen() { if (tab.value === 'reviews') reviews.value = await get('/assets/reviews') }
async function openDetail(a) { try { detail.value = await get('/assets/assessments/' + a.id) } catch (e) { toast(e.message, true) } }
async function handleReview(rv, action) {
  const note = action === 'resolve' ? '维修仓已现场清运车辆、释放桩位并销账' : '维修仓已收到通知，安排现场回收'
  try {
    toast((await post(`/assets/reviews/${rv.id}/handle`, { action, note })).message)
    reviews.value = await get('/assets/reviews')
  } catch (e) { toast(e.message, true) }
}

onMounted(() => loadAssess().catch(e => toast(e.message, true)))
</script>
