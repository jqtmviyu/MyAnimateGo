<script setup>
import { reactiveOmit } from "@vueuse/core";
import { cn } from "cn";
import { DialogOverlay } from "reka-ui";

const props = defineProps({
  forceMount: { type: Boolean, required: false },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false },
  class: { type: null, required: false },
});

const delegatedProps = reactiveOmit(props, "class");
</script>

<template>
  <DialogOverlay
    data-slot="dialog-overlay"
    v-bind="delegatedProps"
    :class="
      cn(
        'dialog-backdrop fixed inset-0 z-50',
        props.class,
      )
    "
  >
    <slot />
  </DialogOverlay>
</template>
