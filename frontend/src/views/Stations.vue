<template>
  <div>
    <div class="page-title">站点与调整分析</div>
    <div class="page-sub">站点调整前评估：用户流失 · 满桩投诉 · 调拨成本 · 调整历史</div>

    <div class="card">
      <table class="tbl">
        <thead><tr><th>站点</th><th>类型</th><th>在桩/容量</th><th>可借</th><th>故障</th><th>电源</th><th>状态</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="s in stations" :key="s.id">
            <td><b>{{ s.name }}</b><div class="small muted">{{ s.address }}</div></td>
            <td><span class="badge gray">{{ typeName(s.type) }}</span></td>
            <td>
              <div class="flex" style="gap:8px">
                <div class="progress-outer" style="width:80px">
                  <div class="progress-inner" :style="{ width: (s.fill_ratio*100)+'%', background: barColor(s) }"></div>
                </div>
                <span class="small">{{ s.used_docks }}/{{ s.capacity }}</span>
              </div>
            </td>
            <td>{{ s.available_bikes }}</td>
            <td>{{ s.fault_bikes }}</td>
            <td><span class="badge" :class="s.power_status === 'normal' ? 'ok' : 'danger'">{{ s.power_status === 'normal' ? '正常' : '断电' }}</span></td>
            <td><span class="badge" :class="stateBadge(s.state)">{{ stateName(s.state) }}</span></td>
            <td>
              <div class="flex" style="gap:6px">
                <button class="btn sm ghost" @click="analyze(s)">调整分析</button>
                <button v-if="isOperator" class="btn sm" @click="openAdjust(s)">调整</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 调整分析 -->
    <div v-if="analysis" class="modal-mask" @click.self="analysis = null">
      <div class="modal" style="width:640px">
        <h3>{{ analysis.station_name }} · 调整影响分析</h3>
        <div class="grid grid-3 mb">
          <div class="stat"><span class="label">用户流失（30天）</span><span class="value" style="color:#d03050">{{ analysis.churned_users_30d }}</span><span class="extra">最后一骑在本站后未再骑行</span></div>
          <div class="stat"><span class="label">满桩投诉（30天）</span><span class="value" style="color:#e6a23c">{{ analysis.full_complaints_30d }}</span><span class="extra">空桩投诉 {{ analysis.empty_complaints_30d }} 起</span></div>
          <div class="stat"><span class="label">调拨成本（30天）</span><span class="value" style="color:#1668dc">¥{{ analysis.rebalance_cost_30d.toFixed(0) }}</span><span class="extra">{{ analysis.rebalance_tasks_30d }} 次调拨</span></div>
        </div>
        <dl class="kv mb">
          <dt>当前容量</dt><dd>{{ analysis.capacity }} 桩</dd>
          <dt>30 天骑行量</dt><dd>{{ analysis.rides_30d }} 次</dd>
        </dl>
        <h4 class="mb">调整历史</h4>
        <div v-if="analysis.adjustments.length" class="timeline">
          <div v-for="(a, i) in analysis.adjustments" :key="i" class="tl-item">
            <div class="tl-head">{{ fmt(a.created_at) }} · {{ a.operator }}</div>
            <div class="tl-body">{{ a.detail || a.action }}</div>
          </div>
        </div>
        <div v-else class="empty">暂无调整记录</div>
        <div class="flex mt"><button class="btn ghost" @click="analysis = null">关闭</button></div>
      </div>
    </div>

    <!-- 站点调整 -->
    <div v-if="adjustStation" class="modal-mask" @click.self="adjustStation = null">
      <div class="modal">
        <h3>站点调整 · {{ adjustStation.name }}</h3>
        <div class="form-row">
          <div class="form-item">
            <label>调整类型</label>
            <select class="input" v-model="adjustForm.action">
              <option value="capacity_expand">扩容桩位</option>
              <option value="capacity_shrink">缩减桩位</option>
              <option value="relocate">迁移站点</option>
              <option value="other">其他</option>
            </select>
          </div>
          <div class="form-item">
            <label>新容量（桩，0=不变）</label>
            <input class="input" type="number" min="0" max="60" v-model.number="adjustForm.new_capacity" />
          </div>
        </div>
        <div class="form-item mb">
          <label>调整说明（同步客服与城市管理方）</label>
          <textarea class="input" rows="3" v-model="adjustForm.detail" placeholder="调整原因、依据数据、对用户通勤的影响"></textarea>
        </div>
        <div class="flex">
          <button class="btn" @click="submitAdjust">提交调整</button>
          <button class="btn ghost" @click="adjustStation = null">取消</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { get, post, getUser } from '../api'
import { toast } from '../components/Toast.vue'

const stations = ref([])
const analysis = ref(null)
const adjustStation = ref(null)
const adjustForm = ref({ action: 'capacity_expand', new_capacity: 0, detail: '' })
const isOperator = computed(() => getUser()?.role === 'operator')

function typeName(t) { return { subway: '地铁口', school: '学校', business: '商圈', residential: '社区' }[t] || t }
function stateName(st) { return { normal: '正常', full: '满桩', empty: '空桩', nearly_full: '将满', nearly_empty: '将空', power_outage: '电源故障', weather_suspended: '天气停运' }[st] || st }
function stateBadge(st) { return { normal: 'ok', full: 'danger', empty: 'danger', nearly_full: 'warn', nearly_empty: 'warn', power_outage: 'purple', weather_suspended: 'gray' }[st] || 'gray' }
function barColor(s) {
  if (s.fill_ratio >= 0.85 || s.fill_ratio <= 0.15) return '#d03050'
  if (s.fill_ratio >= 0.7 || s.fill_ratio <= 0.3) return '#e6a23c'
  return '#18a058'
}
function fmt(t) { return new Date(t).toLocaleString('zh-CN', { hour12: false }) }

async function load() { stations.value = await get('/stations') }

async function analyze(s) {
  try { analysis.value = await get(`/stations/${s.id}/adjust-analysis`) } catch (e) { toast(e.message, true) }
}

function openAdjust(s) {
  adjustStation.value = s
  adjustForm.value = { action: 'capacity_expand', new_capacity: s.capacity, detail: '' }
}

async function submitAdjust() {
  try {
    await post(`/stations/${adjustStation.value.id}/adjust`, adjustForm.value)
    toast('站点调整已记录')
    adjustStation.value = null
    await load()
  } catch (e) { toast(e.message, true) }
}

onMounted(() => load().catch(e => toast(e.message, true)))
</script>
