<script setup>
import {computed,inject,onBeforeUnmount,ref} from 'vue'
import {ContextMenuRoot,ContextMenuTrigger,ContextMenuPortal,ContextMenuContent,ContextMenuItem,ContextMenuSeparator} from 'reka-ui'
import {layerContext} from '@/components/app/layers'
const props=defineProps({table:{type:Object,required:true}})
const layer=inject(layerContext,null),menuLayer=computed(()=>(layer?.value||60)+30),dragging=ref(''),current=ref('')
let cleanup=()=>{}
function resize(event,column){
 event.preventDefault();event.stopPropagation();cleanup()
 const target=event.currentTarget,start=event.clientX,width=target.parentElement.getBoundingClientRect().width
 target.setPointerCapture(event.pointerId)
 let frame,last=width
 const move=e=>{last=width+e.clientX-start;if(!frame)frame=requestAnimationFrame(()=>{frame=null;props.table.resize(column.key,last)})}
 const end=()=>{if(frame)cancelAnimationFrame(frame);props.table.resize(column.key,last);cleanup()}
 cleanup=()=>{target.removeEventListener('pointermove',move);target.removeEventListener('pointerup',end);target.removeEventListener('pointercancel',end);if(frame)cancelAnimationFrame(frame)}
 target.addEventListener('pointermove',move);target.addEventListener('pointerup',end);target.addEventListener('pointercancel',end)
}
function dragStart(event,key){dragging.value=key;event.dataTransfer.effectAllowed='move';event.dataTransfer.setData('text/plain',key)}
function moveColumn(direction){const order=props.table.layout.value.order,index=order.indexOf(current.value),target=order[index+direction];if(target){if(direction<0)props.table.reorder(current.value,target);else props.table.reorder(target,current.value)}}
onBeforeUnmount(()=>cleanup())
</script>
<template>
 <ContextMenuRoot><ContextMenuTrigger as-child><thead @contextmenu.stop><tr><slot name="before"/><th v-for="column in table.columns.value" :key="column.key" :class="`col-${column.key}`" :style="{width:`${table.width(column)}px`,minWidth:`${column.minWidth||72}px`}" :aria-sort="table.layout.value.sort.key===column.key?table.layout.value.sort.descending?'descending':'ascending':'none'" @contextmenu="current=column.key" @dragover.prevent @drop.prevent="dragging&&table.reorder(dragging,column.key);dragging=''">
 <button class="sortable-heading" draggable="true" @dragstart="dragStart($event,column.key)" @dragend="dragging=''" @click="table.sort(column.key)">{{ column.label }}<span v-if="table.layout.value.sort.key===column.key">{{ table.layout.value.sort.descending?'↓':'↑' }}</span></button>
 <span class="column-resizer" role="separator" aria-orientation="vertical" :aria-label="`调整${column.label}列宽`" tabindex="0" :aria-valuenow="Math.round(table.width(column))" :aria-valuemin="column.minWidth||72" aria-valuemax="1000" @pointerdown="resize($event,column)" @keydown.left.prevent="table.resize(column.key,table.width(column)-16)" @keydown.right.prevent="table.resize(column.key,table.width(column)+16)"/>
 </th><slot name="after"/></tr></thead></ContextMenuTrigger><ContextMenuPortal><ContextMenuContent class="download-context-menu" :style="{zIndex:menuLayer}"><div class="download-context-heading">显示列 · 拖动标题可调整顺序</div><ContextMenuItem v-for="column in table.definitions.value" :key="column.key" class="download-context-item" :disabled="column.required" @select.prevent="table.toggle(column.key)">{{ table.layout.value.visible.includes(column.key)?'✓':'　' }} {{ column.label }}</ContextMenuItem><ContextMenuSeparator class="download-context-separator"/><ContextMenuItem class="download-context-item" :disabled="!current" @select.prevent="moveColumn(-1)">当前列左移</ContextMenuItem><ContextMenuItem class="download-context-item" :disabled="!current" @select.prevent="moveColumn(1)">当前列右移</ContextMenuItem><ContextMenuItem class="download-context-item" @select="table.reset()">重置列布局</ContextMenuItem></ContextMenuContent></ContextMenuPortal></ContextMenuRoot>
</template>
