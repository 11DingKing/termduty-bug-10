<template>
  <div class="td-layout">
    <div class="td-aside">
      <div class="logo">终端监测告警</div>
      <el-menu :default-active="activeMenu" router background-color="#304156" text-color="#bfcbd9" active-text-color="#409eff">
        <el-menu-item index="/alerts"><el-icon><Bell /></el-icon><span>告警工单</span></el-menu-item>
        <el-menu-item index="/readings"><el-icon><DataLine /></el-icon><span>运行读数</span></el-menu-item>
        <el-menu-item index="/collectors"><el-icon><Monitor /></el-icon><span>采集点</span></el-menu-item>
        <el-menu-item index="/backlog"><el-icon><Warning /></el-icon><span>积压清单</span></el-menu-item>
      </el-menu>
    </div>
    <div class="td-main">
      <router-view />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { Bell, DataLine, Monitor, Warning } from '@element-plus/icons-vue'

const route = useRoute()
const activeMenu = computed(() => '/' + (route.path.split('/')[1] ?? ''))
</script>
