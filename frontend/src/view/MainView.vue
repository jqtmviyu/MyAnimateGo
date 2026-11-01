<script setup>
import {Toaster} from '@/components/ui/sonner'
import {TooltipProvider} from '@/components/ui/tooltip'
import ConfirmDialogHost from '@/components/app/ConfirmDialogHost.vue'
import LoginView from '@/view/LoginView.vue'
import MainLayoutView from '@/view/home/MainLayoutView.vue'
import {authorization, init, store} from '@/js/global'

init()
</script>

<template>
  <TooltipProvider :delay-duration="200">
    <Toaster :theme="store === 'auto' ? 'system' : store" position="top-center" container-aria-label="通知" rich-colors close-button/>
    <ConfirmDialogHost/>
    <LoginView v-if="!authorization"/>
    <MainLayoutView v-else/>
  </TooltipProvider>
</template>
