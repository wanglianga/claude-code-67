<template>
  <div v-if="data">
    <div class="between mb">
      <div>
        <div class="page-title">申诉溯源 #{{ data.appeal.id }}</div>
        <div class="page-sub">{{ data.appeal.user }} · 提交于 {{ fmt(data.appeal.created_at) }}</div>
      </div>
      <span class="badge" :class="statusBadge(data.appeal.status)" style="font-size:13px">{{ statusName(data.appeal.status) }}</span>
    </div>

    <div class="alert info mb">📣 申诉内容：{{ data.appeal.reason }}</div>

    <!-- 骑行链路 -->
    <div class="card">
      <h3><span class="dot"></span>① 骑行链路（借车桩位 → 还车状态）</h3>
      <div class="timeline">
        <div class="tl-item">
          <div class="tl-head">借车 · {{ fmt(data.trace.borrow_time) }}</div>
          <div class="tl-body">
            站点 <b>{{ data.trace.borrow_station }}</b> · 桩位 <b>{{ data.trace.borrow_dock_no }} 号</b> · 车辆 <b class="mono">{{ data.trace.bike_code }}</b>
          </div>
        </div>
        <div class="tl-item" :class="{ action: data.trace.ride_status === 'completed' }">
          <div class="tl-head">还车 · {{ data.trace.return_time ? fmt(data.trace.return_time) : '未还车' }}</div>
          <div class="tl-body">
            <template v-if="data.trace.return_time">
              站点 <b>{{ data.trace.return_station }}</b> · 桩位 <b>{{ data.trace.return_dock_no }} 号</b> · 车辆已锁止
              · 骑行 {{ data.trace.duration_min }} 分钟 · 费用 <b>¥{{ data.trace.fee.toFixed(2) }}</b>
              <div class="small muted mt">用户位置：{{ data.trace.user_location || '未记录' }}</div>
              <div v-if="data.trace.fault_type" class="small" style="color:#d03050">故障反馈（{{ data.trace.fault_type }}）：{{ data.trace.fault_feedback }}</div>
            </template>
            <template v-else>行程进行中</template>
          </div>
        </div>
      </div>
    </div>

    <div class="grid grid-2">
      <!-- 客服处理 -->
      <div class="card">
        <h3><span class="dot"></span>② 客服处理（关联协同事件）</h3>
        <template v-if="data.cs_trace.length">
          <div v-for="c in data.cs_trace" :key="c.event_id" class="alert info" style="display:block">
            <div class="between">
              <b>#{{ c.event_id }} {{ c.title }}</b>
              <span class="badge" :class="statusBadge(c.status)">{{ statusName(c.status) }}</span>
            </div>
            <div class="small muted mt">{{ c.type_name }} · {{ fmt(c.created_at) }}</div>
            <div v-if="c.resolution" class="small mt">结论：{{ c.resolution }}</div>
          </div>
        </template>
        <div v-else class="empty">该行程无关联协同事件</div>
      </div>

      <!-- 调拨影响 -->
      <div class="card">
        <h3><span class="dot"></span>③ 调拨影响（骑行前后 6 小时）</h3>
        <table class="tbl" v-if="data.rebalance_impact.length">
          <thead><tr><th>#</th><th>调出 → 调入</th><th>数量</th><th>状态</th><th>原因</th></tr></thead>
          <tbody>
            <tr v-for="t in data.rebalance_impact" :key="t.task_id">
              <td>{{ t.task_id }}</td>
              <td class="small">{{ t.from }} → {{ t.to }}</td>
              <td>{{ t.count }}</td>
              <td><span class="badge gray">{{ t.status }}</span></td>
              <td class="small">{{ t.reason }}</td>
            </tr>
          </tbody>
        </table>
        <div v-else class="empty">时间窗口内无涉及本站点的调拨任务</div>
      </div>
    </div>

    <!-- 定责处理 -->
    <div class="card" v-if="canHandle && ['pending','processing'].includes(data.appeal.status)">
      <h3><span class="dot"></span>④ 责任判定与处理</h3>
      <div class="form-row">
        <div class="form-item">
          <label>费用责任</label>
          <select class="input" v-model="form.resp_fee">
            <option value="">请选择</option>
            <option>平台计费规则</option>
            <option>用户正常骑行费用</option>
            <option>站点桩位故障导致</option>
            <option>车辆故障导致</option>
          </select>
        </div>
        <div class="form-item">
          <label>车辆责任</label>
          <select class="input" v-model="form.resp_bike">
            <option value="">请选择</option>
            <option>无责任</option>
            <option>车辆故障（已停用维修）</option>
            <option>车辆损耗（正常磨损）</option>
          </select>
        </div>
        <div class="form-item">
          <label>站点责任</label>
          <select class="input" v-model="form.resp_station">
            <option value="">请选择</option>
            <option>无责任</option>
            <option>桩位故障未及时检修</option>
            <option>满桩调拨不及时</option>
            <option>站点电源/通信故障</option>
          </select>
        </div>
      </div>
      <div class="form-row">
        <div class="form-item">
          <label>退费金额（元）</label>
          <input class="input" type="number" min="0" step="0.5" v-model.number="form.refund" />
        </div>
        <div class="form-item">
          <label>处理结果</label>
          <select class="input" v-model="form.status">
            <option value="resolved">解决（退费/整改）</option>
            <option value="rejected">驳回（申诉不成立）</option>
          </select>
        </div>
      </div>
      <div class="form-item mb">
        <label>处理结论</label>
        <textarea class="input" rows="3" v-model="form.resolution" placeholder="说明核实过程与结论，将同步给用户"></textarea>
      </div>
      <button class="btn" @click="handle" :disabled="!form.resolution">提交处理</button>
    </div>

    <!-- 已处理结果 -->
    <div class="card" v-else-if="data.appeal.resolution">
      <h3><span class="dot"></span>④ 处理结果</h3>
      <dl class="kv">
        <dt>处理人</dt><dd>{{ data.appeal.handler }} · {{ fmt(data.appeal.resolved_at) }}</dd>
        <dt>费用责任</dt><dd>{{ data.appeal.resp_fee || '—' }}</dd>
        <dt>车辆责任</dt><dd>{{ data.appeal.resp_bike || '—' }}</dd>
        <dt>站点责任</dt><dd>{{ data.appeal.resp_station || '—' }}</dd>
        <dt>退费</dt><dd>{{ data.appeal.refund > 0 ? '¥' + data.appeal.refund.toFixed(2) : '无' }}</dd>
        <dt>结论</dt><dd>{{ data.appeal.resolution }}</dd>
      </dl>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { get, post, getUser } from '../api'
import { toast } from '../components/Toast.vue'

const route = useRoute()
const data = ref(null)
const form = ref({ resp_fee: '', resp_bike: '', resp_station: '', refund: 0, status: 'resolved', resolution: '' })
const canHandle = computed(() => ['cs', 'operator'].includes(getUser()?.role))

function statusBadge(s) { return { pending: 'danger', processing: 'warn', resolved: 'ok', rejected: 'gray' }[s] || 'gray' }
function statusName(s) { return { pending: '待处理', processing: '处理中', resolved: '已解决', rejected: '已驳回' }[s] || s }
function fmt(t) { return t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : '—' }

async function load() {
  data.value = await get('/appeals/' + route.params.id)
}

async function handle() {
  try {
    await post(`/appeals/${route.params.id}/handle`, form.value)
    toast('申诉已处理')
    await load()
  } catch (e) { toast(e.message, true) }
}

onMounted(() => load().catch(e => toast(e.message, true)))
</script>
