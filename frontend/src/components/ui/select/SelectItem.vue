<script setup>
import { Check } from "@lucide/vue";
import { reactiveOmit } from "@vueuse/core";
import { cn } from "cn";
import {
  SelectItem,
  SelectItemIndicator,
  SelectItemText,
  useForwardProps,
} from "reka-ui";

const props = defineProps({
  value: { type: null, required: true },
  multiple: {type: Boolean, default: false},
  disabled: { type: Boolean, required: false },
  textValue: { type: String, required: false },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false },
  class: { type: null, required: false },
});

const delegatedProps = reactiveOmit(props, "class", "multiple");

const forwardedProps = useForwardProps(delegatedProps);
</script>

<template>
  <SelectItem
    data-slot="select-item"
    v-bind="forwardedProps"
    :class="
      cn(
        `select-option focus:bg-accent focus:text-accent-foreground [&_svg:not([class*='text-'])]:text-muted-foreground relative flex w-full cursor-default items-center gap-2 rounded-sm py-1.5 pr-8 pl-2 text-sm outline-hidden select-none data-[disabled]:pointer-events-none data-[disabled]:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4 *:[span]:last:flex *:[span]:last:items-center *:[span]:last:gap-2`,
        props.multiple && 'pl-10 pr-3',
        props.class,
      )
    "
  >
    <span :class="props.multiple ? 'select-checkbox absolute left-3 flex h-[18px] w-[18px] items-center justify-center rounded-[3px] border' : 'absolute right-3 flex size-4 items-center justify-center'">
      <SelectItemIndicator>
        <slot name="indicator-icon">
          <Check class="size-4" />
        </slot>
      </SelectItemIndicator>
    </span>

    <SelectItemText>
      <slot />
    </SelectItemText>
  </SelectItem>
</template>

<style scoped>
.select-checkbox{border-color:var(--strong-border);color:var(--foreground)}
.select-checkbox :deep(svg){color:var(--foreground)}
.select-option[data-state=checked] .select-checkbox{border-color:var(--primary);background:var(--primary)}
.select-option[data-state=checked] .select-checkbox :deep(svg){color:var(--primary-foreground)}
</style>
