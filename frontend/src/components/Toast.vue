<template>
  <teleport to="body">
    <div v-if="msg" class="toast" :class="kind">{{ msg }}</div>
  </teleport>
</template>

<script>
import { ref } from 'vue'

const msg = ref('')
const kind = ref('ok')
let timer = null

export function toast(text, isErr = false) {
  msg.value = text
  kind.value = isErr ? 'err' : 'ok'
  clearTimeout(timer)
  timer = setTimeout(() => { msg.value = '' }, 3200)
}

export default {
  setup() {
    return { msg, kind }
  }
}
</script>
