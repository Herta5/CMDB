<template>
  <div class="page" v-loading="loading">
    <el-page-header @back="$router.push('/ci-instances')" :content="instance?.name || '实例详情'" />
    <el-card shadow="never" style="margin-top:16px" v-if="instance">
      <el-descriptions title="基本信息" :column="2" border>
        <el-descriptions-item label="CI编码">{{ instance.ci_code }}</el-descriptions-item>
        <el-descriptions-item label="名称">{{ instance.name }}</el-descriptions-item>
        <el-descriptions-item label="CI类型">{{ instance.ci_type?.display_name }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="statusTag(instance.status)" size="small">{{ statusLabel(instance.status) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="IP地址">{{ instance.ip_address || '-' }}</el-descriptions-item>
        <el-descriptions-item label="MAC地址">{{ instance.mac_address || '-' }}</el-descriptions-item>
        <el-descriptions-item label="序列号">{{ instance.sn || '-' }}</el-descriptions-item>
        <el-descriptions-item label="资产标签">{{ instance.asset_tag || '-' }}</el-descriptions-item>
        <el-descriptions-item label="负责人">{{ instance.owner || '-' }}</el-descriptions-item>
        <el-descriptions-item label="数据来源">{{ instance.source }}</el-descriptions-item>
        <el-descriptions-item label="创建时间">{{ instance.created_at }}</el-descriptions-item>
        <el-descriptions-item label="更新时间">{{ instance.updated_at }}</el-descriptions-item>
      </el-descriptions>

      <el-descriptions title="动态属性" :column="2" border style="margin-top:16px" v-if="Object.keys(instance.attributes || {}).length">
        <el-descriptions-item v-for="(v, k) in instance.attributes" :key="k" :label="k">{{ v }}</el-descriptions-item>
      </el-descriptions>

      <h4 style="margin-top:24px;margin-bottom:12px">关联关系</h4>
      <el-table :data="relations" v-loading="relLoading" size="small">
        <el-table-column label="关系类型" width="140"><template #default="{row}">{{ row.rule?.display_name }}</template></el-table-column>
        <el-table-column label="源 CI" min-width="180"><template #default="{row}">{{ row.source_ci?.name }} <span style="color:#8c8c8c">({{ row.source_ci?.ci_type?.display_name }})</span></template></el-table-column>
        <el-table-column label="目标 CI" min-width="180"><template #default="{row}">{{ row.target_ci?.name }} <span style="color:#8c8c8c">({{ row.target_ci?.ci_type?.display_name }})</span></template></el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { getCIInstance } from '@/api/ci-instance'
import { getRelationInstances } from '@/api/relation'

const route = useRoute()
const loading = ref(false)
const relLoading = ref(false)
const instance = ref<any>(null)
const relations = ref<any[]>([])

onMounted(async () => {
  loading.value = true
  const id = Number(route.params.id)
  const [ciRes, relRes] = await Promise.all([getCIInstance(id), getRelationInstances({})])
  instance.value = ciRes.data
  relations.value = (relRes.data || []).filter((r: any) => r.source_ci_id === id || r.target_ci_id === id)
  loading.value = false
})

function statusTag(s: string) { const m: Record<string,string> = { active:'success', inactive:'info', maintenance:'warning', retired:'danger' }; return m[s]||'info' }
function statusLabel(s: string) { const m: Record<string,string> = { active:'运行中', inactive:'已停用', maintenance:'维护中', retired:'已退役' }; return m[s]||s }
</script>