<script setup>
import {computed, ref, watch} from 'vue'
import {AlertTriangle} from '@lucide/vue'
import {Button} from '@/components/ui/button'
import {Input} from '@/components/ui/input'
import {Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle} from '@/components/ui/dialog'
import {AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle} from '@/components/ui/alert-dialog'
import {nextDialogLayer} from '@/components/app/layers'
import {activeDialog, settleDialog} from '@/lib/dialogs'

const input = ref('')
const layer=ref(1000)
const prompt = computed(() => activeDialog.value?.kind === 'prompt')
const danger = computed(() => activeDialog.value?.options.type === 'error' || activeDialog.value?.options.confirmButtonClass?.includes('danger'))
const cancel = () => settleDialog(false, '', 'cancel')
const dismiss = open => { if (!open && activeDialog.value) settleDialog(false, '', 'close') }
watch(() => activeDialog.value?.id, () => { if(activeDialog.value)layer.value=nextDialogLayer(); input.value = activeDialog.value?.options.inputValue || '' })
</script>

<template>
  <Dialog :open="Boolean(activeDialog && prompt)" @update:open="dismiss">
    <DialogContent :layer-index="layer" class="sm:max-w-md">
      <DialogHeader><DialogTitle>{{ activeDialog?.title }}</DialogTitle><DialogDescription>{{ activeDialog?.message }}</DialogDescription></DialogHeader>
      <form class="space-y-5" @submit.prevent="settleDialog(true, input)">
        <Input v-model="input" :aria-label="activeDialog?.message" autofocus/>
        <DialogFooter>
          <Button type="button" variant="outline" @click.capture="cancel">{{ activeDialog?.options.cancelButtonText || '取消' }}</Button>
          <Button type="submit">{{ activeDialog?.options.confirmButtonText || '确认' }}</Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
  <AlertDialog :open="Boolean(activeDialog && !prompt)" @update:open="dismiss">
    <AlertDialogContent :layer-index="layer" class="sm:max-w-md">
      <AlertDialogHeader>
        <div v-if="danger || activeDialog?.options.type === 'warning'" class="mb-2 flex size-10 items-center justify-center rounded-xl bg-amber-500/10 text-amber-600"><AlertTriangle class="size-5"/></div>
        <AlertDialogTitle>{{ activeDialog?.title }}</AlertDialogTitle>
        <AlertDialogDescription class="whitespace-pre-wrap break-words leading-relaxed">{{ activeDialog?.message }}</AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter class="mt-3">
        <AlertDialogCancel v-if="activeDialog?.options.showCancelButton !== false" @click.capture="cancel">{{ activeDialog?.options.cancelButtonText || '取消' }}</AlertDialogCancel>
        <AlertDialogAction as-child><Button :variant="danger ? 'destructive' : 'default'" @click.capture="settleDialog(true)">{{ activeDialog?.options.confirmButtonText || '确认' }}</Button></AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
