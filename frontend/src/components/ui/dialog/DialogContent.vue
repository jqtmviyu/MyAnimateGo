<script setup>
import { X } from "@lucide/vue";
import { reactiveOmit } from "@vueuse/core";
import { cn } from "cn";
import {
  DialogClose,
  DialogContent,
  DialogPortal,
  useForwardPropsEmits,
} from "reka-ui";
import DialogOverlay from "./DialogOverlay.vue";

defineOptions({
  inheritAttrs: false,
});

const props = defineProps({
  layerIndex: {type: Number, default: 50},
  dialogDepth: {type: Number, default: 1},
  forceMount: { type: Boolean, required: false },
  disableOutsidePointerEvents: { type: Boolean, required: false },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false },
  class: { type: null, required: false },
  showCloseButton: { type: Boolean, required: false, default: true },
});
const emits = defineEmits([
  "escapeKeyDown",
  "pointerDownOutside",
  "focusOutside",
  "interactOutside",
  "openAutoFocus",
  "closeAutoFocus",
]);

const delegatedProps = reactiveOmit(props, "class", "layerIndex", "dialogDepth", "showCloseButton");

const forwarded = useForwardPropsEmits(delegatedProps, emits);
</script>

<template>
  <DialogPortal>
    <DialogOverlay :style="{zIndex: layerIndex, background: dialogDepth > 1 ? 'var(--dialog-nested-overlay)' : 'var(--dialog-overlay)'}" />
    <DialogContent
      data-slot="dialog-content"
      :data-dialog-depth="dialogDepth"
      :style="{...$attrs.style, zIndex: layerIndex + 1}"
      v-bind="{ ...$attrs, ...forwarded }"
      :class="
        cn(
          'dialog-surface fixed top-[50%] left-[50%] z-50 grid w-full max-w-[calc(100%-2rem)] translate-x-[-50%] translate-y-[-50%] gap-4 rounded-lg border p-6 shadow-lg duration-200 sm:max-w-lg',
          props.class,
        )
      "
    >
      <slot />

      <DialogClose
        v-if="showCloseButton"
        data-slot="dialog-close"
        class="dialog-close-button"
      >
        <X />
        <span class="sr-only">关闭此对话框</span>
      </DialogClose>
    </DialogContent>
  </DialogPortal>
</template>
