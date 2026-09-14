<template>
  <div>
    <div class="between mb">
      <div class="page-title">维修与车辆档案</div>
      <button v-if="isLead" class="btn ghost" @click="$router.push('/assets')">🏷️ 重复故障报废评估 / 资产台账</button>
    </div>
    <div class="page-sub">故障类型 · 维修用时 · 配件消耗 · 重复故障 · 报废 —— 全部进入车辆档案</div>

    <div class="grid grid-4 mb" v-if="stats">
      <div class="stat"><span class="label">故障待处理</span><span class="value" style="color:#d03050">{{ stats.fault_bikes }}</span><span class="extra">在修 {{ stats.in_repair }} 辆</span></div>
      <div class="stat"><span class="label">重复故障</span><span class="value" style="color:#e6a23c">{{ stats.repeat_faults }}</span><span class="extra">30 天内同车同类型</span></div>
      <div class="stat"><span class="label">平均维修用时</span><span class="value" style="color:#1668dc">{{ (stats.avg_repair_min || 0).toFixed(0) }}′</span><span class="extra">修复类工单</span></div>
      <div class="stat"><span class="label">已报废</span><span class="value" style="color:#5a6478">{{ stats.scrapped }}</span><span class="extra">退出运营</span></div>
    </div>

    <div class="grid" style="grid-template-columns: 1.2fr 1fr">
      <!-- 故障工单 -->
      <div class="card">
        <h3><span class="dot"></span>故障工单</h3>
        <table class="tbl" v-if="faults.length">
          <thead><tr><th>#</th><th>车辆</th><th>类型</th><th>描述</th><th>状态</th><th>操作</th></tr></thead>
          <tbody>
            <tr v-for="f in faults" :key="f.id">
              <td>{{ f.id }}</td>
              <td class="mono">{{ f.bike_code }}</td>
              <td><span class="badge" :class="f.type === 'brake' ? 'danger' : 'warn'">{{ f.type_name }}</span></td>
              <td class="small">{{ f.description }}</td>
              <td><span class="badge" :class="faultBadge(f.status)">{{ faultName(f.status) }}</span></td>
              <td>
                <button v-if="isRepair && ['pending','assigned'].includes(f.status)" class="btn sm" @click="take(f)">接收入库</button>
                <button v-if="isRepair && f.status === 'repairing'" class="btn sm ok" @click="openRepair(f)">登记维修</button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-else class="empty">暂无故障工单</div>
      </div>

      <!-- 车辆档案查询 -->
      <div class="card">
        <h3><span class="dot"></span>车辆档案</h3>
        <div class="flex mb">
          <input class="input mono" v-model.trim="archiveCode" placeholder="输入车辆编号，如 BK0012" @keyup.enter="loadArchive" />
          <button class="btn" @click="loadArchive" :disabled="!archiveCode">查询</button>
        </div>
        <div v-if="archive">
          <dl class="kv mb">
            <dt>车辆</dt><dd class="mono">{{ archive.code }} <span class="badge" :class="archive.status === 'scrapped' ? 'danger' : 'ok'">{{ bikeStatusName(archive.status) }}</span></dd>
            <dt>累计骑行</dt><dd>{{ archive.total_rides }} 次 · {{ Math.round(archive.mileage_km || 0) }} km
              <span v-if="archive.deploy_restricted" class="badge warn">限制投放</span>
            </dd>
            <dt>累计配件</dt><dd>¥{{ (archive.total_parts_cost || 0).toFixed(0) }}</dd>
            <dt>最近清洗</dt><dd>{{ archive.last_cleaned_at ? fmt(archive.last_cleaned_at) : '未记录' }}</dd>
          </dl>
          <div class="timeline">
            <div v-for="h in archive.history" :key="h.fault_id" class="tl-item" :class="{ action: h.result === 'fixed' }">
              <div class="tl-head">
                {{ fmt(h.reported_at) }} · {{ h.type_name }}故障
                <span v-if="h.is_repeat" class="badge warn">重复故障</span>
                <span v-if="h.result === 'scrapped'" class="badge danger">已报废</span>
              </div>
              <div class="tl-body">
                <div>{{ h.description }}</div>
                <div v-if="h.result" class="small muted mt">
                  维修：{{ h.repairer }} · 用时 {{ h.duration_min }} 分钟 · 配件 {{ h.parts || '无' }} · {{ h.result === 'fixed' ? '修复' : '报废' }}
                  <div v-if="h.notes">备注：{{ h.notes }}</div>
                </div>
              </div>
            </div>
            <div v-if="!archive.history.length" class="empty">该车暂无故障记录</div>
          </div>
        </div>
      </div>
    </div>

    <!-- 维修记录 -->
    <div class="card">
      <h3><span class="dot"></span>维修记录</h3>
      <table class="tbl" v-if="repairs.length">
        <thead><tr><th>#</th><th>车辆</th><th>故障类型</th><th>维修员</th><th>用时</th><th>配件消耗</th><th>结果</th><th>备注</th></tr></thead>
        <tbody>
          <tr v-for="r in repairs" :key="r.id">
            <td>{{ r.id }}</td>
            <td class="mono">{{ r.bike_code }}</td>
            <td>{{ r.fault_type_name }}</td>
            <td>{{ r.repairer }}</td>
            <td>{{ r.duration_min }} 分钟</td>
            <td class="small">{{ r.parts || '—' }}</td>
            <td>
              <span class="badge" :class="r.result === 'fixed' ? 'ok' : 'danger'">{{ r.result === 'fixed' ? '修复' : '报废' }}</span>
              <span v-if="r.is_repeat" class="badge warn" style="margin-left:4px">重复</span>
            </td>
            <td class="small">{{ r.notes }}</td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">暂无维修记录</div>
    </div>

    <!-- 登记维修弹窗 -->
    <div v-if="repairFault" class="modal-mask" @click.self="repairFault = null">
      <div class="modal">
        <h3>维修登记（{{ repairFault.bike_code }} · {{ repairFault.type_name }}）</h3>
        <div class="form-row">
          <div class="form-item">
            <label>维修用时（分钟）</label>
            <input class="input" type="number" min="1" v-model.number="repairForm.duration_min" />
          </div>
          <div class="form-item">
            <label>维修结果</label>
            <select class="input" v-model="repairForm.result">
              <option value="fixed">修复，恢复运营</option>
              <option value="scrapped">报废，退出运营</option>
            </select>
          </div>
        </div>
        <div class="form-item mb">
          <label>配件消耗</label>
          <div v-for="(p, i) in repairForm.parts" :key="i" class="flex mb">
            <select class="input" v-model.number="p.part_id">
              <option v-for="pt in parts" :key="pt.id" :value="pt.id">{{ pt.name }}（库存 {{ pt.stock }}）</option>
            </select>
            <input class="input" style="width:90px" type="number" min="1" v-model.number="p.qty" />
            <button class="btn sm ghost" @click="repairForm.parts.splice(i, 1)">✕</button>
          </div>
          <button class="btn sm ghost" @click="repairForm.parts.push({ part_id: parts[0]?.id || 0, qty: 1 })">＋ 添加配件</button>
        </div>
        <div class="form-item mb">
          <label>维修备注</label>
          <textarea class="input" rows="2" v-model="repairForm.notes" placeholder="故障原因、处理过程"></textarea>
        </div>
        <div class="flex">
          <button class="btn ok" @click="submitRepair">提交入库</button>
          <button class="btn ghost" @click="repairFault = null">取消</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { get, post, getUser } from '../api'
import { toast } from '../components/Toast.vue'

const faults = ref([])
const repairs = ref([])
const parts = ref([])
const stats = ref(null)
const archive = ref(null)
const archiveCode = ref('')
const repairFault = ref(null)
const repairForm = ref({ duration_min: 30, result: 'fixed', notes: '', parts: [] })

const isRepair = computed(() => ['repair', 'operator'].includes(getUser()?.role))
const isLead = computed(() => ['repair_lead', 'operator'].includes(getUser()?.role))

function faultBadge(s) { return { pending: 'danger', assigned: 'warn', repairing: 'purple', fixed: 'ok', scrapped: 'gray' }[s] || 'gray' }
function faultName(s) { return { pending: '待接单', assigned: '已派单', repairing: '维修中', fixed: '已修复', scrapped: '已报废' }[s] || s }
function bikeStatusName(s) {
  return { docked: '在桩', rented: '租用中', fault: '故障', in_repair: '维修中', in_transit: '调拨在途', cleaning: '清洗中', scrapped: '已报废' }[s] || s
}
function fmt(t) { return new Date(t).toLocaleString('zh-CN', { hour12: false }) }

async function load() {
  const [f, r, p, s] = await Promise.all([
    get('/faults'), get('/repairs'), get('/parts'), get('/maintenance/stats'),
  ])
  faults.value = f.filter(x => ['pending', 'assigned', 'repairing'].includes(x.status))
  repairs.value = r
  parts.value = p
  stats.value = s
}

async function take(f) {
  try {
    await post(`/faults/${f.id}/assign`)
    toast('已接收入库')
    await load()
  } catch (e) { toast(e.message, true) }
}

function openRepair(f) {
  repairFault.value = f
  repairForm.value = { duration_min: 30, result: 'fixed', notes: '', parts: [] }
}

async function submitRepair() {
  try {
    const res = await post('/repairs', {
      fault_id: repairFault.value.id,
      ...repairForm.value,
    })
    toast(res.message)
    repairFault.value = null
    await load()
  } catch (e) { toast(e.message, true) }
}

async function loadArchive() {
  try {
    archive.value = await get('/bikes/' + archiveCode.value + '/archive')
  } catch (e) { toast(e.message, true) }
}

onMounted(() => load().catch(e => toast(e.message, true)))
</script>
