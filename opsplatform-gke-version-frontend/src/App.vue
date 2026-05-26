<template>
  <el-container>
    <el-header style="display:flex;align-items:center;gap:16px;background:#1f2d3d;color:#fff;">
      <strong>GKE 版本监控</strong>
      <el-menu mode="horizontal" router :default-active="$route.path" background-color="#1f2d3d" text-color="#fff" active-text-color="#409EFF">
        <el-menu-item index="/">集群列表</el-menu-item>
        <el-menu-item index="/settings">设置</el-menu-item>
      </el-menu>
    </el-header>
    <el-main>
      <router-view />
    </el-main>
  </el-container>
</template>
