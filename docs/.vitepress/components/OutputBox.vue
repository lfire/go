<script setup lang="ts">
import { ref } from 'vue'

/** 运行结果展示：可先隐藏输出让学生自己猜，再揭晓。 */
const props = withDefaults(defineProps<{
  title?: string
  output: string
  note?: string
  guess?: boolean | string
}>(), { guess: false })

const isGuess = () => props.guess === true || props.guess === 'true'
const shown = ref(!isGuess())
const text = String(props.output || '').replace(/\\n/g, '\n')
</script>

<template>
  <div class="ob-box">
    <div class="ob-head">
      <span class="ob-dots"><i></i><i></i><i></i></span>
      <b>{{ title || '运行结果' }}</b>
      <button v-if="!shown" class="ob-toggle" type="button" @click="shown = true">先自己猜，再点这里揭晓 👀</button>
      <button v-else-if="isGuess()" class="ob-toggle" type="button" @click="shown = false">再藏起来</button>
    </div>
    <pre v-if="shown" class="ob-body"><code>{{ text }}</code></pre>
    <div v-else class="ob-hidden">输出已隐藏 · 先在脑子里跑一遍这段代码</div>
    <div v-if="note" class="ob-note">💡 {{ note }}</div>
  </div>
</template>
