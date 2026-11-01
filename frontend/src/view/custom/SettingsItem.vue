<script setup>
import {provide, ref, useId} from 'vue'
import {Label} from '@/components/ui/label'
import {fieldContext} from '@/components/app/controls/context'

defineProps({label: {type: String, default: ''}, labelWidth: {type: Number, default: 150}, description: {type: String, default: ''}})
const firstId = ref('')
const labelId = useId()
provide(fieldContext, {firstId, labelId})
</script>

<template>
  <div class="settings-item grid min-w-0 gap-2.5 border-b border-border/60 py-3.5 last:border-b-0" :class="label ? 'sm:grid-cols-[var(--settings-label-width)_minmax(0,1fr)]' : 'grid-cols-1'" :style="{'--settings-label-width': `${labelWidth}px`}">
    <Label v-if="label" :id="labelId" :for="firstId || undefined" class="break-words text-sm font-medium leading-9">{{ label }}</Label>
    <div class="settings-value flex min-w-0 flex-wrap items-center gap-2"><p v-if="description" class="settings-help">{{ description }}</p><slot/></div>
  </div>
</template>
