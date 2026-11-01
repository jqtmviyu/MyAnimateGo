<script setup>
import {Moon, Sun} from '@lucide/vue'
import {resolvedTheme, toggleTheme} from '@/lib/theme'
</script>

<template>
  <button type="button" class="inline-flex size-10 shrink-0 items-center justify-center rounded-full text-muted-foreground hover:bg-secondary hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring" :aria-label="resolvedTheme === 'dark' ? '切换到白天模式' : '切换到暗黑模式'" :title="resolvedTheme === 'dark' ? '切换到白天模式' : '切换到暗黑模式'" @click="toggleTheme">
    <Sun v-if="resolvedTheme === 'dark'" class="size-5"/><Moon v-else class="size-5"/>
  </button>
</template>
