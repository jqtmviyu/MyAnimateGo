<template>
  <AppPopconfirm :title="props.title" @confirm="emit('confirm')" :width="160">
    <template #reference>
      <slot name="reference"/>
    </template>
    <template #actions="{ confirm, cancel }">
      <div class="flex action">
        <div>
          <AppButton size="small" @click="cancel" bg text icon="Close">取消</AppButton>
        </div>
        <div>
          <AppButton
              type="danger"
              size="small"
              @click="confirm"
              bg text
              icon="Check"
          >
            确定
          </AppButton>
        </div>
      </div>
    </template>
  </AppPopconfirm>
</template>

<script setup>
let props = defineProps({
  title: String
})
const emit = defineEmits(['confirm'])
</script>

<style scoped>
.action {
  justify-content:space-between;
}
</style>
