<script setup>
import { reactiveOmit } from "@vueuse/core";
import { cn } from "cn";
import { ScrollAreaScrollbar, ScrollAreaThumb } from "reka-ui";

const props = defineProps({
  orientation: { type: String, required: false, default: "vertical" },
  forceMount: { type: Boolean, required: false },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false },
  class: { type: null, required: false },
});

const delegatedProps = reactiveOmit(props, "class");
</script>

<template>
  <ScrollAreaScrollbar
    data-slot="scroll-area-scrollbar"
    v-bind="delegatedProps"
    :class="
      cn(
        'group/scrollbar flex touch-none p-px transition-colors select-none bg-transparent',
        orientation === 'vertical' &&
          'h-full w-[7px]',
        orientation === 'horizontal' &&
          'h-[7px] flex-col',
        props.class,
      )
    "
  >
    <ScrollAreaThumb
      data-slot="scroll-area-thumb"
      class="bg-foreground/15 group-hover/scrollbar:bg-foreground/30 relative flex-1 rounded-full transition-colors"
    />
  </ScrollAreaScrollbar>
</template>
