<script setup>
import {computed,ref} from 'vue'
import TableHeader from './TableHeader.vue'
import {useTableLayout} from './table-layout'
import {selectRows} from './selection'
const props=defineProps({name:String,columns:{type:Array,required:true},rows:{type:Array,default:()=>[]},rowKey:{type:Function,required:true}})
const table=useTableLayout(props.name,()=>props.columns),columns=table.columns
const rows=computed(()=>table.sorted(props.rows)),selected=ref([]),anchor=ref(null),focused=ref(null)
const ids=computed(()=>rows.value.map(props.rowKey)),chosen=computed(()=>new Set(selected.value))
function select(event,row,checkbox=false){const id=props.rowKey(row),next=selectRows({ids:ids.value,selected:selected.value,anchor:anchor.value,id,toggle:checkbox||event.ctrlKey||event.metaKey,range:event.shiftKey});selected.value=next.selected;anchor.value=next.anchor;focused.value=id}
function move(event,row){if(event.target!==event.currentTarget||!['ArrowUp','ArrowDown',' '].includes(event.key))return;event.preventDefault();const index=ids.value.indexOf(props.rowKey(row)),next=rows.value[Math.max(0,Math.min(rows.value.length-1,index+(event.key==='ArrowUp'?-1:event.key==='ArrowDown'?1:0)))];select(event,next,event.key===' ');event.currentTarget.parentElement.querySelectorAll('tr')[ids.value.indexOf(props.rowKey(next))]?.focus()}
</script>
<template>
 <div class="detail-table-scroll"><table class="detail-table configurable-detail-table" :style="{width:`${table.totalWidth.value+36}px`}"><TableHeader :table="table"><template #before><th class="check-column"><input type="checkbox" aria-label="选择全部记录" :checked="rows.length>0&&rows.every(row=>chosen.has(rowKey(row)))" @change="selected=$event.target.checked?ids:[]"></th></template></TableHeader><tbody><tr v-for="row in rows" :key="rowKey(row)" :tabindex="focused===rowKey(row)||(!focused&&rows[0]===row)?0:-1" :aria-selected="chosen.has(rowKey(row))" :class="{'file-is-selected':chosen.has(rowKey(row))}" @click="select($event,row)" @keydown="move($event,row)"><td><input type="checkbox" :aria-label="`选择记录 ${rowKey(row)}`" :checked="chosen.has(rowKey(row))" @click.stop="select($event,row,true)"></td><td v-for="column in columns" :key="column.key" :title="String(column.format?column.format(row[column.key],row):row[column.key]??'')">{{ column.format?column.format(row[column.key],row):row[column.key]??'—' }}</td></tr><tr v-if="!rows.length"><td :colspan="columns.length+1" class="detail-empty">暂无记录</td></tr></tbody></table></div>
</template>
