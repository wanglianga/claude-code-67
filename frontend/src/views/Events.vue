<template>
  <div>
    <div class="between mb">
      <div>
        <div class="page-title">协同事件</div>
        <div class="page-sub">无法还车 / 站点无车 / 刹车异常 / 锁具打不开 / 调拨车拥堵 / 潮汐失衡 —— 多角色同一事件协同处理</div>
      </div>
      <div class="flex">
        <select class="input" style="width:130px" v-model="filter" @change="load">
          <option value="all">全部状态</option>
          <option value="open">待处理</option>
          <option value="processing">处理中</option>
          <option value="resolved">已解决</option>
        </select>
        <button v-if="isStaff" class="btn" @click="showCreate = true">＋ 发起事件</button>
      </div>
    </div>

    <div class="card">
      <table class="tbl" v-if="events.length">
        <thead><tr><th>#</th><th>类型</th><th>标题</th><th>站点</th><th>优先级</th><th>状态</th><th>参与</th><th>创建时间</th><th></th></tr></thead>
        <tbody>
          <tr v-for="e in events" :key="e.id">
            <td>{{ e.id }}</td>
            <td><span class="badge" :class="typeBadge(e.type)">{{ e.type_name }}</span></td>
            <td>{{ e.title }}</td>
            <td>{{ e.station }}</td>
            <td><span class="badge" :class="e.priority === 'high' ? 'danger' : 'gray'">{{ e.priority === 'high' ? '高' : '中' }}</span></td>
            <td><span class="badge" :class="statusBadge(e.status)">{{ statusName(e.status) }}</span></td>
            <td class="small">{{ e.participant_count }} 人 · {{ e.message_count }} 条</td>
            <td class="small">{{ fmt(e.created_at) }}</td>
            <td><router-link class="btn sm ghost" :to="'/events/' + e.id">进入</router-link></td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">暂无事件</div>
    </div>

    <div v-if="showCreate" class="modal-mask" @click.self="showCreate = false">
      <div class="modal">
        <h3>发起协同事件</h3>
        <div class="form-item mb">
          <label>事件类型</label>
          <select class="input" v-model="form.type">
            <option value="cannot_return">无法还车（满桩）</option>
            <option value="no_bike">站点无车</option>
            <option value="brake_fault">刹车异常</option>
            <option value="lock_stuck">锁具打不开</option>
            <option value="truck_stuck">调拨车堵在路上</option>
            <option value="tidal_imbalance">潮汐失衡</option>
          </select>
        </div>
        <div class="form-item mb">
          <label>关联站点</label>
          <select class="input" v-model.number="form.station_id">
            <option :value="0" disabled>请选择</option>
            <option v-for="s in stations" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
        </div>
        <div class="form-item mb">
          <label>标题（可留空自动生成）</label>
          <input class="input" v-model="form.title" placeholder="事件标题" />
        </div>
        <div class="flex">
          <button class="btn" @click="create" :disabled="!form.station_id">创建并拉入相关角色</button>
          <button class="btn ghost" @click="showCreate = false">取消</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { get, post, getUser } from '../api'
import { toast } from '../components/Toast.vue'

const router = useRouter()
const events = ref([])
const stations = ref([])
const filter = ref('all')
const showCreate = ref(false)
const form = ref({ type: 'cannot_return', station_id: 0, title: '' })
const isStaff = computed(() => getUser()?.role !== 'user')

function typeBadge(t) {
  return { cannot_return: 'danger', no_bike: 'warn', brake_fault: 'danger', lock_stuck: 'purple', truck_stuck: 'warn', tidal_imbalance: 'info' }[t] || 'gray'
}
function statusBadge(s) { return { open: 'danger', processing: 'warn', resolved: 'ok' }[s] || 'gray' }
function statusName(s) { return { open: '待处理', processing: '处理中', resolved: '已解决' }[s] || s }
function fmt(t) { return new Date(t).toLocaleString('zh-CN', { hour12: false }) }

async function load() {
  events.value = await get('/events?status=' + filter.value)
}

async function create() {
  try {
    const res = await post('/events', form.value)
    toast('事件已创建')
    showCreate.value = false
    router.push('/events/' + res.event_id)
  } catch (e) { toast(e.message, true) }
}

onMounted(async () => {
  try {
    await load()
    if (isStaff.value) stations.value = await get('/stations')
  } catch (e) { toast(e.message, true) }
})
</script>
