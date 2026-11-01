<script setup>
import {computed, ref} from 'vue'
import {Bell, BookOpen, ChevronDown, Database, Download, FileText, Filter, Home, Info, Monitor, Network, Plus, Rss, Search, Shield, SlidersHorizontal, Timer} from '@lucide/vue'
const props = defineProps({categories: Array, active: String})
const emit = defineEmits(['select', 'navigate'])
const query = ref('')
const collapsed = ref(new Set())
const sectionIcons = {download: Download, peerFilter: Shield, migration: Timer, proxy: Network, security: Shield, add: Plus, rename: FileText, rss: Rss, exclude: Filter, bangumi: BookOpen, page: Monitor, notification: Bell, backup: Database, other: SlidersHorizontal, about: Info}
const filteredCategories = computed(() => props.categories.map(category => ({...category, items: category.items.filter(item => (item.label + item.description).toLowerCase().includes(query.value.trim().toLowerCase()))})).filter(category => category.items.length))
const toggle = label => { if (collapsed.value.has(label)) collapsed.value.delete(label); else collapsed.value.add(label) }
</script>
<template>
  <aside class="settings-navigation" aria-label="设置分类">
    <RouterLink to="/catalog" class="settings-brand" @click="emit('navigate')"><Rss :size="27"/>MyAnimeGo</RouterLink>
    <label class="settings-search" data-focus-container><Search :size="18"/><input v-model="query" aria-label="搜索设置分类" placeholder="搜索" type="search"></label>
    <RouterLink to="/catalog" class="settings-home" @click="emit('navigate')"><Home :size="19"/>媒体库</RouterLink>
    <nav>
      <section v-for="category in filteredCategories" :key="category.label" class="settings-category">
        <h2><button :aria-expanded="!!query || !collapsed.has(category.label)" @click="toggle(category.label)">{{ category.label }}<ChevronDown :size="17" :class="{'category-collapsed': !query && collapsed.has(category.label)}"/></button></h2>
        <div v-show="query || !collapsed.has(category.label)"><button v-for="item in category.items" :key="item.name" class="settings-link" :aria-current="active === item.name ? 'page' : undefined" @click="emit('select', item.name)"><component :is="sectionIcons[item.name]" :size="19"/><span>{{ item.label }}</span></button></div>
      </section>
      <p v-if="!filteredCategories.length" class="settings-no-results">没有匹配的设置</p>
    </nav>
  </aside>
</template>
<style scoped>
.settings-navigation{height:100%;background:var(--sidebar);overflow:auto;padding:22px 14px 32px;scrollbar-width:thin}.settings-brand{display:flex;align-items:center;gap:8px;padding:0 10px;margin-bottom:26px;font-size:22px;font-weight:600;letter-spacing:-.035em;color:var(--foreground)}.settings-brand svg{color:var(--primary)}.settings-search{display:flex;align-items:center;gap:9px;background:var(--surface-hover);border:0;border-radius:11px;padding:9px 10px;margin:0 0 20px;color:var(--text-secondary)}.settings-search input{width:100%;min-width:0;background:none;border:0;font-size:14px;color:var(--foreground);outline:none}.settings-search:focus-within{outline:2px solid var(--ring);outline-offset:-2px}.settings-home{display:flex;align-items:center;gap:12px;padding:8px 12px;font-size:14px;color:var(--foreground);margin-bottom:20px;border-radius:8px}.settings-home:hover{background:var(--surface-hover)}.settings-category{margin-bottom:22px}.settings-category h2{margin-bottom:5px}.settings-category h2 button{display:flex;align-items:center;justify-content:space-between;width:100%;padding:7px 3px;color:var(--muted-foreground);font-size:13px;font-weight:600}.settings-category h2 svg{color:var(--success);transition:transform .15s}.category-collapsed{transform:rotate(-90deg)}.settings-link{width:100%;display:flex;align-items:center;gap:12px;text-align:left;padding:8px 12px;font-size:14px;color:var(--text-secondary);border-radius:8px;line-height:1.5}.settings-link:hover{background:var(--surface-hover);color:var(--foreground)}.settings-link[aria-current]{background:var(--primary);color:var(--primary-foreground)}.settings-link svg{flex-shrink:0}.settings-no-results{padding:12px;color:var(--text-secondary);font-size:14px}
</style>
