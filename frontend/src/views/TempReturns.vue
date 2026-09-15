<template>
  <div>
    <div class="page-title">临时还车处理单</div>
    <div class="page-sub">附近无空桩时用户临时锁车并提交含站点编号的车辆照片与位置；客服审核后关闭计费，费用调整理由对用户可见</div>

    <div class="flex mb" style="gap:8px">
      <button class="btn sm" :class="filter!=='pending' && 'ghost'" @click="setFilter('pending')">待审核 <span v-if="pendingCount" class="badge danger">{{ pendingCount }}</span></button>
      <button class="btn sm" :class="filter!=='approved' && 'ghost'" @click="setFilter('approved')">已通过</button>
      <button class="btn sm" :class="filter!=='rejected' && 'ghost'" @click="setFilter('rejected')">已驳回</button>
      <button class="btn sm ghost" @click="setFilter('')">全部</button>
      <button class="btn sm" style="margin-left:auto" @click="openCreate">＋ 客服生成临时还车单</button>
    </div>

    <div class="card">
      <table class="tbl" v-if="orders.length">
        <thead><tr><th>#</th><th>用户</th><th>车辆</th><th>满桩站点(编号)</th><th>照片含站点编号</th><th>位置/超时</th><th>暂停时费用</th><th>状态</th><th>审核</th></tr></thead>
        <tbody>
          <tr v-for="o in orders" :key="o.id">
            <td>{{ o.id }}</td>
            <td>{{ o.user_name }}<div class="small muted">{{ o.phone || '无电话' }}</div></td>
            <td class="mono">{{ o.bike_code }}</td>
            <td>{{ o.station }}<div class="small muted">编号 {{ o.station_code }} · 提交 {{ fmt(o.created_at) }}</div></td>
            <td>
              <img v-if="o.photo_data" :src="o.photo_data" class="thumb" @click="photo=o.photo_data" alt="车辆照片" />
              <span v-else class="muted small">无照片</span>
            </td>
            <td class="small">{{ o.user_location || '—' }}<div v-if="o.overtime"><span class="badge warn">提交时已超时</span></div></td>
            <td>¥{{ o.fee_before_pause.toFixed(2) }}<div v-if="o.status==='approved'" class="small muted">最终 ¥{{ o.final_fee.toFixed(2) }} · 减 ¥{{ o.waiver_amount.toFixed(2) }}</div></td>
            <td><span class="badge" :class="badgeOf(o.status)">{{ o.status_name }}</span>
              <div v-if="o.adjust_reason" class="small muted mt">{{ o.adjust_reason }}</div>
            </td>
            <td>
              <template v-if="o.status==='pending'">
                <button class="btn sm ok" @click="openReview(o,'approve')">通过并关闭计费</button>
                <button class="btn sm danger" @click="openReview(o,'reject')">驳回</button>
              </template>
              <span v-else class="small muted">{{ o.handled_at ? fmt(o.handled_at) : '' }}</span>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">暂无临时还车处理单</div>
    </div>

    <!-- 照片放大 -->
    <div v-if="photo" class="modal-mask" @click.self="photo=''">
      <div class="modal" style="max-width:560px">
        <img :src="photo" style="width:100%;border-radius:8px" />
        <div class="flex mt"><button class="btn ghost" @click="photo=''">关闭</button></div>
      </div>
    </div>

    <!-- 审核 -->
    <div v-if="reviewItem" class="modal-mask" @click.self="reviewItem=null">
      <div class="modal">
        <h3>{{ action==='approve' ? '审核通过 · 关闭计费' : '驳回临时还车' }}（工单 #{{ reviewItem.id }}）</h3>
        <dl class="kv mb">
          <dt>用户/车辆</dt><dd>{{ reviewItem.user_name }} · <span class="mono">{{ reviewItem.bike_code }}</span></dd>
          <dt>满桩站点</dt><dd>{{ reviewItem.station }}（编号 {{ reviewItem.station_code }}，与照片站点编号须一致）</dd>
          <dt>暂停时已产生费用</dt><dd>¥{{ reviewItem.fee_before_pause.toFixed(2) }}{{ reviewItem.overtime ? '（提交时已超时）' : '' }}</dd>
        </dl>
        <img v-if="reviewItem.photo_data" :src="reviewItem.photo_data" style="width:100%;max-height:260px;object-fit:contain;border:1px solid #e3e8f0;border-radius:8px;margin-bottom:10px" />
        <div v-if="action==='approve'" class="form-item mb">
          <label>免除金额（¥，0=按暂停时刻费用全额收取）</label>
          <input class="input" type="number" min="0" :max="reviewItem.fee_before_pause" step="0.5" v-model.number="form.waiver_amount" />
          <div class="small muted">最终费用 = ¥{{ reviewItem.fee_before_pause.toFixed(2) }} − ¥{{ Number(form.waiver_amount||0).toFixed(2) }} = ¥{{ finalFee.toFixed(2) }}</div>
        </div>
        <div class="form-item mb">
          <label>费用调整理由（用户端可见）{{ action==='approve' ? '' : '（驳回原因）' }}</label>
          <textarea class="input" rows="3" v-model="form.adjust_reason"
            :placeholder="action==='approve' ? '如：满桩引导临时还车，审核照片与位置属实，免除找桩等待期间费用' : '如：照片未包含站点编号，请重新拍摄后提交'"></textarea>
        </div>
        <div class="flex">
          <button class="btn" :class="action==='approve' && 'ok'" @click="submit">确认{{ action==='approve' ? '通过' : '驳回' }}</button>
          <button class="btn ghost" @click="reviewItem=null">取消</button>
        </div>
      </div>
    </div>
    <!-- 客服生成临时还车单 -->
    <div v-if="createOpen" class="modal-mask" @click.self="createOpen=false">
      <div class="modal" style="max-width:620px">
        <h3>客服生成临时还车处理单</h3>
        <div class="form-item mb">
          <label>选择用户进行中行程（满桩无法还车）</label>
          <select class="input" v-model="cf.ride_id" @change="onPickRide">
            <option :value="0" disabled>请选择进行中行程</option>
            <option v-for="r in ongoing" :key="r.ride_id" :value="r.ride_id">
              {{ r.user_name }}（{{ r.username }}）· {{ r.bike_code }} · 借自 {{ r.borrow_station }} · 已 {{ r.elapsed_min }} 分钟{{ r.overtime ? '（超时）' : '' }}
            </option>
          </select>
          <div v-if="!ongoing.length" class="small muted mt">当前无进行中行程</div>
        </div>
        <div class="form-item mb">
          <label>满桩站点（必须无空桩）</label>
          <select class="input" v-model.number="cf.full_station_id" @change="cf.station_code = codeOf(cf.full_station_id)">
            <option :value="0" disabled>请选择</option>
            <option v-for="s in stations" :key="s.id" :value="s.id">
              {{ s.name }}（{{ s.code }}）· 空桩 {{ s.free_docks }}{{ s.free_docks === 0 ? ' · 满桩' : '' }}
            </option>
          </select>
        </div>
        <div class="form-item mb">
          <label>车辆照片（画面需含站点编号）</label>
          <input class="input" type="file" accept="image/*" @change="onPhoto" />
          <div v-if="photoPreview" class="photo-preview">
            <img :src="photoPreview" alt="车辆照片" />
            <div class="photo-tag">站点编号 {{ cf.station_code }} · {{ stamp }}</div>
          </div>
        </div>
        <div class="form-item mb">
          <label>核对站点编号（须与照片、满桩站点一致）</label>
          <input class="input mono" v-model.trim="cf.station_code" placeholder="如 ST04" />
        </div>
        <div class="form-item mb">
          <label>用户位置</label>
          <input class="input" v-model="cf.user_location" placeholder="如：万象城西门非机动车围栏停放区" />
        </div>
        <div class="small muted mb">提交后行程转为审核中、计费暂停（审核前保持暂停），车辆临时锁定不可借出。</div>
        <div class="flex">
          <button class="btn ok" @click="submitCreate"
            :disabled="!cf.ride_id || !cf.full_station_id || !cf.station_code || !cf.photo_data || !cf.user_location">生成临时处理单</button>
          <button class="btn ghost" @click="createOpen=false">取消</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { get, post } from '../api'
import { toast } from '../components/Toast.vue'

const orders = ref([])
const filter = ref('pending')
const photo = ref('')
const reviewItem = ref(null)
const action = ref('approve')
const form = ref({ adjust_reason: '', waiver_amount: 0 })
const ongoing = ref([])
const stations = ref([])
const createOpen = ref(false)
const photoPreview = ref('')
const stamp = new Date().toLocaleString('zh-CN', { hour12: false })
const cf = ref({ ride_id: 0, full_station_id: 0, station_code: '', photo_data: '', user_location: '' })

const pendingCount = computed(() => orders.value.filter(o => o.status === 'pending').length)
const finalFee = computed(() => Math.max(0, (reviewItem.value?.fee_before_pause || 0) - Number(form.value.waiver_amount || 0)))

function badgeOf(s) { return { pending: 'warn', approved: 'ok', rejected: 'danger' }[s] || 'gray' }
function fmt(t) { return t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : '—' }
function codeOf(id) { const s = stations.value.find(x => x.id === id); return s ? s.code : '' }

async function load() {
  const path = filter.value ? '/temp-returns?status=' + filter.value : '/temp-returns'
  orders.value = await get(path)
}
function setFilter(f) { filter.value = f; load().catch(e => toast(e.message, true)) }

async function openCreate() {
  cf.value = { ride_id: 0, full_station_id: 0, station_code: '', photo_data: '', user_location: '' }
  photoPreview.value = ''
  createOpen.value = true
  try {
    const [r, s] = await Promise.all([get('/temp-returns/ongoing'), get('/stations')])
    ongoing.value = r; stations.value = s
  } catch (e) { toast(e.message, true) }
}
function onPickRide() {
  const r = ongoing.value.find(x => x.ride_id === cf.value.ride_id)
  if (r) cf.value.user_location = cf.value.user_location || ''
}
function onPhoto(ev) {
  const file = ev.target.files[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => {
    const img = new Image()
    img.onload = () => {
      const w = Math.min(640, img.width), scale = w / img.width
      const h = Math.round(img.height * scale)
      const canvas = document.createElement('canvas')
      canvas.width = w; canvas.height = h
      const ctx = canvas.getContext('2d')
      ctx.drawImage(img, 0, 0, w, h)
      const code = cf.value.station_code || '----'
      ctx.fillStyle = 'rgba(0,0,0,0.62)'; ctx.fillRect(0, h - 46, w, 46)
      ctx.fillStyle = '#fff'; ctx.font = 'bold 20px sans-serif'
      ctx.fillText('站点编号 ' + code, 12, h - 18)
      ctx.font = '13px sans-serif'
      ctx.fillText(new Date().toLocaleString('zh-CN', { hour12: false }), w - 190, h - 20)
      const url = canvas.toDataURL('image/jpeg', 0.75)
      cf.value.photo_data = url; photoPreview.value = url
    }
    img.src = reader.result
  }
  reader.readAsDataURL(file)
}
async function submitCreate() {
  try {
    const res = await post('/temp-returns', {
      ride_id: cf.value.ride_id, full_station_id: cf.value.full_station_id,
      station_code: cf.value.station_code, photo_data: cf.value.photo_data,
      user_location: cf.value.user_location,
    })
    toast(res.message); createOpen.value = false; filter.value = 'pending'; await load()
  } catch (e) { toast(e.message, true) }
}

function openReview(o, act) {
  reviewItem.value = o; action.value = act
  form.value = { adjust_reason: '', waiver_amount: act === 'approve' ? o.fee_before_pause : 0 }
}
async function submit() {
  try {
    const res = await post(`/temp-returns/${reviewItem.value.id}/review`, {
      action: action.value, adjust_reason: form.value.adjust_reason, waiver_amount: Number(form.value.waiver_amount || 0),
    })
    toast(res.message); reviewItem.value = null; await load()
  } catch (e) { toast(e.message, true) }
}

onMounted(() => load().catch(e => toast(e.message, true)))
</script>

<style scoped>
.thumb { width: 78px; height: 58px; object-fit: cover; border-radius: 6px; border: 1px solid #e3e8f0; cursor: zoom-in; }
.photo-preview { margin-top: 8px; max-width: 320px; border: 1px solid #e3e8f0; border-radius: 8px; overflow: hidden; }
.photo-preview img { width: 100%; display: block; }
.photo-tag { background: rgba(0,0,0,.62); color: #fff; font-size: 12px; padding: 4px 8px; }
</style>
