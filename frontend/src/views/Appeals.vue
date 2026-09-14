<template>
  <div>
    <div class="page-title">申诉处理</div>
    <div class="page-sub">从一次骑行回溯：借车桩位 → 还车状态 → 客服处理 → 调拨影响；判定费用 / 车辆 / 站点责任</div>

    <div class="card">
      <table class="tbl" v-if="appeals.length">
        <thead><tr><th>#</th><th>行程</th><th>用户</th><th>申诉原因</th><th>状态</th><th>处理人</th><th>退费</th><th>提交时间</th><th></th></tr></thead>
        <tbody>
          <tr v-for="a in appeals" :key="a.id">
            <td>{{ a.id }}</td>
            <td>#{{ a.ride_id }}</td>
            <td>{{ a.user }}</td>
            <td class="small" style="max-width:300px">{{ a.reason }}</td>
            <td><span class="badge" :class="statusBadge(a.status)">{{ statusName(a.status) }}</span></td>
            <td>{{ a.handler || '—' }}</td>
            <td>{{ a.refund > 0 ? '¥' + a.refund.toFixed(2) : '—' }}</td>
            <td class="small">{{ fmt(a.created_at) }}</td>
            <td><router-link class="btn sm ghost" :to="'/appeals/' + a.id">溯源</router-link></td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">暂无申诉记录</div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { get } from '../api'
import { toast } from '../components/Toast.vue'

const appeals = ref([])
function statusBadge(s) { return { pending: 'danger', processing: 'warn', resolved: 'ok', rejected: 'gray' }[s] || 'gray' }
function statusName(s) { return { pending: '待处理', processing: '处理中', resolved: '已解决', rejected: '已驳回' }[s] || s }
function fmt(t) { return new Date(t).toLocaleString('zh-CN', { hour12: false }) }

onMounted(async () => {
  try { appeals.value = await get('/appeals') } catch (e) { toast(e.message, true) }
})
</script>
