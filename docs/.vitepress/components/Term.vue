<script setup lang="ts">
import { ref } from 'vue'
defineProps<{ word: string; text: string }>()
const open = ref(false)
</script>

<template>
  <span
    class="go-term" tabindex="0"
    @mouseenter="open = true" @mouseleave="open = false"
    @focus="open = true" @blur="open = false"
    @click="open = !open"
  >{{ word }}<Transition name="go-fade"><span v-if="open" class="go-term-tip">{{ text }}</span></Transition></span>
</template>
