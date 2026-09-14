<template>
  <div class="login-wrap">
    <div class="login-card">
      <h1>🚲 城市公共自行车运营平台</h1>
      <div class="sub">站点满桩调拨 · 维修档案 · 协同事件 · 申诉溯源</div>
      <form @submit.prevent="doLogin">
        <div class="form-item mb">
          <label>用户名</label>
          <input class="input" v-model.trim="username" placeholder="请输入用户名" autocomplete="username" />
        </div>
        <div class="form-item mb">
          <label>密码</label>
          <input class="input" type="password" v-model="password" placeholder="请输入密码" autocomplete="current-password" />
        </div>
        <div v-if="error" class="alert danger mb">{{ error }}</div>
        <button class="btn" style="width:100%;justify-content:center" :disabled="loading">
          {{ loading ? '登录中…' : '登 录' }}
        </button>
      </form>
      <div class="demo-accounts">
        <div class="small muted mb">演示账号（密码均为 123456，点击填入）：</div>
        <div v-for="a in accounts" :key="a.u" class="acc" @click="fill(a.u)">
          <span>{{ a.label }}</span><span class="mono">{{ a.u }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { post, setAuth } from '../api'

const router = useRouter()
const username = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)

const accounts = [
  { u: 'rider1', label: '用户（正常）' },
  { u: 'rider2', label: '用户（押金不足）' },
  { u: 'rider3', label: '用户（骑行受限）' },
  { u: 'cs1', label: '客服' },
  { u: 'disp1', label: '调度员' },
  { u: 'repair1', label: '维修员' },
  { u: 'admin1', label: '站点管理员' },
  { u: 'ops1', label: '运营' },
  { u: 'city1', label: '城市管理方' },
]

function fill(u) { username.value = u; password.value = '123456' }

async function doLogin() {
  error.value = ''
  loading.value = true
  try {
    const res = await post('/login', { username: username.value, password: password.value })
    setAuth(res.token, res.user)
    router.push('/')
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}
</script>
