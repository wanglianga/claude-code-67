<template>
  <div>
    <div class="between mb">
      <div>
        <div class="page-title">调拨中心</div>
        <div class="page-sub">策略因子：地铁早高峰 · 学校放学 · 商圈活动 · 维修车比例 · 调拨车容量；每个调度动作均可解释</div>
      </div>
      <div class="flex" style="gap:8px">
        <button class="btn ghost" @click="$router.push('/peak-routes')">🗺️ 高峰调拨路线</button>
        <button v-if="canPlan" class="btn" @click="showPlan = true">＋ 生成调拨计划</button>
      </div>
    </div>

    <!-- 调拨车状态 -->
    <div class="grid grid-3 mb">
      <div v-for="t in trucks" :key="t.id" class="stat">
        <span class="label">🚚 {{ t.plate }} · 司机 {{ t.driver || '未排班' }}</span>
        <span class="value" style="font-size:18px">
          <span class="badge" :class="truckBadge(t.status)">{{ truckName(t.status) }}</span>
        </span>
        <span class="extra">容量 {{ t.capacity }} 辆{{ t.task_id ? ' · 任务 #' + t.task_id : '' }}</span>
      </div>
    </div>

    <!-- 任务列表 -->
    <div class="card">
      <h3><span class="dot"></span>调拨任务</h3>
      <table class="tbl" v-if="tasks.length">
        <thead><tr><th>#</th><th>调出 → 调入</th><th>数量</th><th>调拨车</th><th>原因</th><th>成本</th><th>状态</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="t in tasks" :key="t.id">
            <td>{{ t.id }}</td>
            <td>{{ t.from_station }} → {{ t.to_station }}</td>
            <td>{{ t.bike_count }} 辆</td>
            <td>{{ t.truck || '未派车' }}</td>
            <td class="small">{{ t.reason }}</td>
            <td>¥{{ t.cost.toFixed(2) }}</td>
            <td><span class="badge" :class="taskBadge(t.status)">{{ taskName(t.status) }}</span></td>
            <td>
              <div class="flex" style="gap:6px">
                <button class="btn sm ghost" @click="showExplain(t)">解释</button>
                <template v-if="canPlan">
                  <button v-if="t.status === 'pending'" class="btn sm" @click="openAssign(t)">派车</button>
                  <button v-if="t.status === 'assigned'" class="btn sm" @click="setStatus(t, 'in_progress')">出车</button>
                  <button v-if="t.status === 'in_progress'" class="btn sm ok" @click="setStatus(t, 'complete')">完成</button>
                  <button v-if="t.status === 'in_progress'" class="btn sm danger" @click="setStatus(t, 'stuck')">堵车</button>
                  <button v-if="t.status === 'stuck'" class="btn sm ok" @click="setStatus(t, 'complete')">完成</button>
                </template>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">暂无调拨任务</div>
    </div>

    <!-- 生成计划 -->
    <div v-if="showPlan" class="modal-mask" @click.self="showPlan = false">
      <div class="modal">
        <h3>生成调拨计划（策略因子）</h3>
        <div class="form-item mb">
          <label><input type="checkbox" v-model="plan.subway_peak" /> 地铁早高峰（6-10 点地铁口借车上调）</label>
        </div>
        <div class="form-item mb">
          <label><input type="checkbox" v-model="plan.school_dismissal" /> 学校放学（14-18 点学校站借车上调）</label>
        </div>
        <div class="form-item mb">
          <label><input type="checkbox" v-model="plan.business_event" /> 商圈活动（17-22 点商圈还车集中）</label>
        </div>
        <div class="form-row">
          <div class="form-item">
            <label>维修车比例阈值（{{ (plan.repair_ratio_limit * 100).toFixed(0) }}%）</label>
            <input class="input" type="range" min="0.05" max="0.5" step="0.05" v-model.number="plan.repair_ratio_limit" />
          </div>
          <div class="form-item">
            <label>调拨车容量（辆/趟，0=按空闲车最小容量）</label>
            <input class="input" type="number" min="0" max="40" v-model.number="plan.truck_capacity" />
          </div>
        </div>
        <div class="flex">
          <button class="btn" @click="generate" :disabled="generating">{{ generating ? '计算中…' : '生成任务' }}</button>
          <button class="btn ghost" @click="showPlan = false">关闭</button>
        </div>
        <div v-if="planResult" class="mt">
          <div class="alert ok">{{ planResult.message }}</div>
          <div v-for="c in planResult.created" :key="c.id" class="alert info" style="display:block">
            <b>#{{ c.id }} {{ c.from }} → {{ c.to }}（{{ c.count }} 辆，¥{{ c.cost.toFixed(2) }}）</b>
            <div class="small mt">{{ c.explanation }}</div>
          </div>
        </div>
      </div>
    </div>

    <!-- 派车 -->
    <div v-if="assignTask" class="modal-mask" @click.self="assignTask = null">
      <div class="modal">
        <h3>任务 #{{ assignTask.id }} 派车</h3>
        <p class="mb">{{ assignTask.from_station }} → {{ assignTask.to_station }}（{{ assignTask.bike_count }} 辆）</p>
        <div class="form-item mb">
          <label>选择调拨车（空闲）</label>
          <select class="input" v-model.number="assignTruckId">
            <option :value="0" disabled>请选择</option>
            <option v-for="t in idleTrucks" :key="t.id" :value="t.id">
              {{ t.plate }} · 容量 {{ t.capacity }} · 司机 {{ t.driver || '未排班' }}
            </option>
          </select>
        </div>
        <div class="flex">
          <button class="btn" @click="assign" :disabled="!assignTruckId">确认派车</button>
          <button class="btn ghost" @click="assignTask = null">取消</button>
        </div>
      </div>
    </div>

    <!-- 解释 -->
    <div v-if="explainTask" class="modal-mask" @click.self="explainTask = null">
      <div class="modal">
        <h3>调度动作解释（任务 #{{ explainTask.id }}）</h3>
        <dl class="kv mb">
          <dt>调出 → 调入</dt><dd>{{ explainTask.from_station }} → {{ explainTask.to_station }}</dd>
          <dt>数量 / 成本</dt><dd>{{ explainTask.bike_count }} 辆 · ¥{{ explainTask.cost.toFixed(2) }}</dd>
          <dt>状态</dt><dd>{{ taskName(explainTask.status) }}</dd>
        </dl>
        <div class="alert info" style="display:block">{{ explainTask.explanation || explainTask.reason }}</div>
        <div v-if="explainTask.factors" class="tag-row mt">
          <span v-if="explainTask.factors.subway_peak" class="badge info">地铁早高峰</span>
          <span v-if="explainTask.factors.school_dismissal" class="badge info">学校放学</span>
          <span v-if="explainTask.factors.business_event" class="badge info">商圈活动</span>
          <span v-if="explainTask.factors.truck_capacity" class="badge gray">车容量 {{ explainTask.factors.truck_capacity }}</span>
        </div>
        <p class="small muted mt">该说明同步提供给客服与城市管理方，用于解释调度动作对用户通勤的影响。</p>
        <div class="flex mt"><button class="btn ghost" @click="explainTask = null">关闭</button></div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { get, post, getUser } from '../api'
import { toast } from '../components/Toast.vue'

const tasks = ref([])
const trucks = ref([])
const showPlan = ref(false)
const generating = ref(false)
const planResult = ref(null)
const plan = ref({ subway_peak: true, school_dismissal: true, business_event: false, repair_ratio_limit: 0.15, truck_capacity: 0 })
const assignTask = ref(null)
const assignTruckId = ref(0)
const explainTask = ref(null)

const canPlan = computed(() => ['dispatcher', 'operator'].includes(getUser()?.role))
const idleTrucks = computed(() => trucks.value.filter(t => t.status === 'idle'))

function truckBadge(s) { return { idle: 'ok', loading: 'info', en_route: 'info', stuck: 'danger' }[s] || 'gray' }
function truckName(s) { return { idle: '空闲', loading: '装车中', en_route: '在途', stuck: '拥堵' }[s] || s }
function taskBadge(s) { return { pending: 'warn', assigned: 'info', in_progress: 'purple', stuck: 'danger', completed: 'ok', cancelled: 'gray' }[s] || 'gray' }
function taskName(s) { return { pending: '待派车', assigned: '已派车', in_progress: '在途', stuck: '拥堵', completed: '已完成', cancelled: '已取消' }[s] || s }

async function load() {
  const [t, k] = await Promise.all([get('/rebalance/tasks'), get('/trucks')])
  tasks.value = t
  trucks.value = k
}

async function generate() {
  generating.value = true
  try {
    planResult.value = await post('/rebalance/plan', plan.value)
    await load()
  } catch (e) { toast(e.message, true) } finally { generating.value = false }
}

function openAssign(t) { assignTask.value = t; assignTruckId.value = 0 }

async function assign() {
  try {
    await post(`/rebalance/tasks/${assignTask.value.id}/assign`, { truck_id: assignTruckId.value })
    toast('已派车')
    assignTask.value = null
    await load()
  } catch (e) { toast(e.message, true) }
}

async function setStatus(t, status) {
  try {
    const res = await post(`/rebalance/tasks/${t.id}/status`, { status })
    toast(res.message)
    await load()
  } catch (e) { toast(e.message, true) }
}

async function showExplain(t) {
  try {
    explainTask.value = await get('/rebalance/tasks/' + t.id)
  } catch (e) { toast(e.message, true) }
}

onMounted(() => load().catch(e => toast(e.message, true)))
</script>
