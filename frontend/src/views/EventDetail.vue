<template>
  <div v-if="ev">
    <div class="between mb">
      <div>
        <div class="page-title">
          <span class="badge" :class="typeBadge(ev.type)" style="margin-right:8px">{{ ev.type_name }}</span>
          {{ ev.title }}
        </div>
        <div class="page-sub">事件 #{{ ev.id }} · {{ ev.station }} · 创建于 {{ fmt(ev.created_at) }}</div>
      </div>
      <span class="badge" :class="statusBadge(ev.status)" style="font-size:13px">{{ statusName(ev.status) }}</span>
    </div>

    <div class="grid" style="grid-template-columns: 280px 1fr">
      <!-- 参与者 -->
      <div>
        <div class="card">
          <h3><span class="dot"></span>协同人员（{{ ev.participants.length }}）</h3>
          <div v-for="p in ev.participants" :key="p.user_id" class="flex between" style="padding:6px 0;border-bottom:1px solid var(--line)">
            <span>{{ p.name }}</span>
            <span class="badge" :class="roleBadge(p.role)">{{ p.role_name }}</span>
          </div>
          <button v-if="isStaff && !joined && ev.status !== 'resolved'" class="btn ghost sm mt" @click="join">＋ 加入协同</button>
        </div>

        <!-- 处置动作 -->
        <div class="card" v-if="isStaff && ev.status !== 'resolved'">
          <h3><span class="dot"></span>处置动作</h3>
          <div class="section-gap">
            <div v-if="ev.type === 'cannot_return' || ev.type === 'no_bike' || ev.type === 'tidal_imbalance'">
              <div class="form-item mb">
                <label>调往站点</label>
                <select class="input" v-model.number="actionForm.to_station_id">
                  <option :value="0" disabled>选择站点</option>
                  <option v-for="s in stations" :key="s.id" :value="s.id">{{ s.name }}</option>
                </select>
              </div>
              <div class="form-item mb">
                <label>调拨数量</label>
                <input class="input" type="number" min="1" max="30" v-model.number="actionForm.bike_count" />
              </div>
              <button class="btn sm" @click="doAction('create_rebalance')">🚚 生成调拨任务</button>
            </div>
            <button v-if="ev.bike_id" class="btn sm ok" @click="doAction('dispatch_repair')">🔧 派发维修工单</button>
            <button v-if="ev.ride_id" class="btn sm ghost" @click="doAction('waive_fee')">💰 减免本程费用</button>
            <hr style="border:none;border-top:1px solid var(--line)" />
            <div class="form-item mb">
              <label>处理结论</label>
              <textarea class="input" rows="2" v-model="resolution" placeholder="填写结论后关闭事件"></textarea>
            </div>
            <button class="btn ok sm" @click="resolve" :disabled="!resolution">✔ 解决并关闭事件</button>
          </div>
        </div>
        <div v-if="ev.status === 'resolved'" class="card">
          <h3><span class="dot"></span>处理结论</h3>
          <p style="font-size:13px">{{ ev.resolution }}</p>
          <p class="small muted mt">解决于 {{ fmt(ev.resolved_at) }}</p>
        </div>
      </div>

      <!-- 时间线 -->
      <div class="card">
        <h3><span class="dot"></span>协同时间线</h3>
        <div class="timeline mb">
          <div v-for="m in ev.messages" :key="m.id" class="tl-item" :class="m.msg_type">
            <div class="tl-head">
              <b>{{ m.sender }}</b>
              <span v-if="m.role_name"> · {{ m.role_name }}</span>
              <span v-if="m.msg_type === 'action'" class="badge ok" style="margin-left:6px">处置</span>
              · {{ fmt(m.created_at) }}
            </div>
            <div class="tl-body">{{ m.content }}</div>
          </div>
        </div>
        <div class="flex" v-if="ev.status !== 'resolved'">
          <input class="input" v-model="msg" placeholder="发言（事件相关人员可见）…" @keyup.enter="send" />
          <button class="btn" @click="send" :disabled="!msg">发送</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { get, post, getUser } from '../api'
import { toast } from '../components/Toast.vue'

const route = useRoute()
const ev = ref(null)
const stations = ref([])
const msg = ref('')
const resolution = ref('')
const actionForm = ref({ to_station_id: 0, bike_count: 5 })
const me = computed(() => getUser())
const isStaff = computed(() => me.value && me.value.role !== 'user')
const joined = computed(() => ev.value && me.value && ev.value.participants.some(p => p.user_id === me.value.id))

function typeBadge(t) {
  return { cannot_return: 'danger', no_bike: 'warn', brake_fault: 'danger', lock_stuck: 'purple', truck_stuck: 'warn', tidal_imbalance: 'info' }[t] || 'gray'
}
function statusBadge(s) { return { open: 'danger', processing: 'warn', resolved: 'ok' }[s] || 'gray' }
function statusName(s) { return { open: '待处理', processing: '处理中', resolved: '已解决' }[s] || s }
function roleBadge(r) {
  return { user: 'info', cs: 'purple', dispatcher: 'warn', repair: 'ok', station_admin: 'gray', operator: 'info', city: 'gray', creator: 'info' }[r] || 'gray'
}
function fmt(t) { return t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : '—' }

async function load() {
  ev.value = await get('/events/' + route.params.id)
}

async function join() {
  try { await post(`/events/${route.params.id}/join`); await load() } catch (e) { toast(e.message, true) }
}

async function send() {
  try {
    await post(`/events/${route.params.id}/messages`, { content: msg.value })
    msg.value = ''
    await load()
  } catch (e) { toast(e.message, true) }
}

async function doAction(action) {
  try {
    const res = await post(`/events/${route.params.id}/action`, { action, payload: actionForm.value })
    toast(res.message)
    await load()
  } catch (e) { toast(e.message, true) }
}

async function resolve() {
  try {
    await post(`/events/${route.params.id}/resolve`, { resolution: resolution.value })
    toast('事件已关闭')
    await load()
  } catch (e) { toast(e.message, true) }
}

onMounted(async () => {
  try {
    await load()
    if (isStaff.value) stations.value = await get('/stations')
  } catch (e) { toast(e.message, true) }
})
</script>
