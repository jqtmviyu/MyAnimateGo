<script setup>
import ThemeToggle from '@/components/app/ThemeToggle.vue'
import {computed, onMounted, reactive, ref} from 'vue'
import {ArrowRight, Eye, EyeOff, LoaderCircle, LockKeyhole, Rss} from '@lucide/vue'
import {Button} from '@/components/ui/button'
import {Input} from '@/components/ui/input'
import {Label} from '@/components/ui/label'
import {Checkbox} from '@/components/ui/checkbox'
import {Card, CardContent} from '@/components/ui/card'
import * as http from '@/js/http'
import {authorization, rememberThePassword} from '@/js/global'

const loading = ref(false)
const showPassword = ref(false)
const error = ref('')
const user = reactive({username: '', password: ''})
const ready = computed(() => user.username.trim() && user.password.trim())

async function login() {
  if (loading.value) return
  error.value = ''
  if (!ready.value) {
    error.value = '请输入用户名和密码'
    return
  }
  loading.value = true
  try {
    const result = await http.login({username: user.username.trim(), password: user.password.trim()}, {silent: true})
    const remember = rememberThePassword.value
    remember.username = remember.remember ? user.username.trim() : ''
    remember.password = remember.remember ? user.password.trim() : ''
    authorization.value = result.data
  } catch (cause) {
    error.value = cause?.message || '登录失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  const remembered = rememberThePassword.value
  if (remembered.remember) Object.assign(user, {username: remembered.username, password: remembered.password})
  try {
    const result = await http.testIpWhitelist()
    if (result.code === 200) authorization.value = String(Date.now())
  } catch {
    // 白名单不可用时继续保留账号登录入口。
  }
})
</script>

<template>
  <div class="relative flex min-h-dvh flex-col bg-background">
    <header class="flex items-center justify-between px-6 py-5 sm:px-10">
      <a href="#" class="flex items-center gap-2.5 font-semibold tracking-tight" @click.prevent>
        <span class="flex size-9 items-center justify-center text-primary"><Rss class="size-6"/></span>
        <span>MyAnimeGo</span>
      </a>
      <ThemeToggle/>
    </header>
    <main class="flex flex-1 items-center justify-center px-5 pb-12">
      <div class="w-full max-w-[420px]">
        <div class="mb-8 space-y-2 text-center">
          <div class="mx-auto mb-5 flex size-14 items-center justify-center rounded-2xl border border-primary/10 bg-primary/5 text-primary"><LockKeyhole class="size-6"/></div>
          <h1 class="text-3xl font-semibold tracking-tight">欢迎回来</h1>
          <p class="text-sm text-muted-foreground">登录媒体库，继续关注喜欢的作品。</p>
        </div>
        <Card class="rounded-md border-border/80 shadow-none">
          <CardContent class="p-6 sm:p-8">
            <form class="space-y-5" @submit.prevent="login">
              <div class="space-y-2">
                <Label for="login-username">用户名</Label>
                <Input id="login-username" v-model="user.username" name="username" autocomplete="username" placeholder="输入用户名" required class="h-11" :disabled="loading"/>
              </div>
              <div class="space-y-2">
                <Label for="login-password">密码</Label>
                <div class="relative">
                  <Input id="login-password" v-model="user.password" name="password" :type="showPassword ? 'text' : 'password'" autocomplete="current-password" placeholder="输入密码" required class="h-11 pr-11" :disabled="loading" :aria-invalid="Boolean(error)" :aria-describedby="error ? 'login-error' : undefined"/>
                  <Button type="button" variant="ghost" size="icon" class="absolute right-1 top-1 size-9" :aria-label="showPassword ? '隐藏密码' : '显示密码'" :aria-pressed="showPassword" @click="showPassword = !showPassword"><EyeOff v-if="showPassword" class="size-4"/><Eye v-else class="size-4"/></Button>
                </div>
              </div>
              <div class="flex items-center gap-2">
                <Checkbox id="remember-login" v-model="rememberThePassword.remember"/>
                <Label for="remember-login" class="cursor-pointer text-sm font-normal text-muted-foreground">记住密码</Label>
              </div>
              <p v-if="error" id="login-error" role="alert" class="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">{{ error }}</p>
              <Button type="submit" class="h-11 w-full gap-2" :disabled="loading" :aria-busy="loading">
                <LoaderCircle v-if="loading" class="size-4 animate-spin"/>
                <span>{{ loading ? '正在登录' : '登录' }}</span><ArrowRight v-if="!loading" class="size-4"/>
              </Button>
            </form>
          </CardContent>
        </Card>
        <p class="mt-5 text-center text-xs text-muted-foreground">订阅、下载与整理，让追番井井有条。</p>
      </div>
    </main>
  </div>
</template>
