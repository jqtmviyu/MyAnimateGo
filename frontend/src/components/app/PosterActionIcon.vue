<script setup>
defineProps({action: {type: String, default: 'add'}})
</script>

<template>
  <span class="poster-open-indicator inline-flex size-14 items-center justify-center rounded-full border border-media-foreground/25 bg-media-overlay text-media-foreground shadow-lg backdrop-blur-sm" aria-hidden="true">
    <svg class="pointer-events-none size-7" viewBox="0 0 24 24" fill="currentColor" stroke="none" focusable="false">
      <path v-if="action === 'edit'" d="M5 14.8 14.8 5 19 9.2 9.2 19H5v-4.2ZM16 3.8l1-1a1.4 1.4 0 0 1 2 0l2.2 2.2a1.4 1.4 0 0 1 0 2l-1 1L16 3.8Z"/>
      <path v-else-if="action === 'cover'" fill-rule="evenodd" d="M4 3h16a1 1 0 0 1 1 1v16a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1Zm1 2v14h14V5H5Zm2 12 3.5-5 2.5 3 2-2 2 4H7Zm2-9a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3Z"/>
      <path v-else d="M10.5 3h3v7.5H21v3h-7.5V21h-3v-7.5H3v-3h7.5V3Z"/>
    </svg>
  </span>
</template>
