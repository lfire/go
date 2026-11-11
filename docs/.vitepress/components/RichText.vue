<script setup lang="ts">
import { computed } from 'vue'
import { parseRich } from '../richtext'

const props = defineProps<{ text?: string }>()
const parts = computed(() => parseRich(props.text || ''))
</script>

<template>
  <template v-for="(p, i) in parts" :key="i">
    <a v-if="p.t === 'link'" :href="p.href">{{ p.v }}</a>
    <strong v-else-if="p.t === 'strong'">{{ p.v }}</strong>
    <code v-else-if="p.t === 'code'">{{ p.v }}</code>
    <template v-else>{{ p.v }}</template>
  </template>
</template>
