<template>
  <div class="page">
    <div class="page-header">
      <h3>CI 类型管理</h3>
      <el-button v-if="canAccessRoles(['cmdb_admin'])" type="primary" @click="openCreate"><el-icon><Plus /></el-icon>新建类型</el-button>
    </div>
    <el-card shadow="never">
      <el-table :data="treeData" row-key="id" default-expand-all :indent="24" v-loading="loading">
        <el-table-column prop="display_name" label="类型名称" min-width="200">
          <template #default="{ row }">
            <el-icon style="margin-right:6px"><component :is="row.icon || 'Server'" /></el-icon>
            {{ row.display_name }}
            <el-tag v-if="row.is_abstract" size="small" type="info" style="margin-left:8px">抽象</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="标识" width="160" />
        <el-table-column prop="description" label="描述" min-width="200" show-overflow-tooltip />
        <el-table-column v-if="canAccessRoles(['cmdb_admin'])" label="操作" width="240" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
            <el-button link type="primary" size="small" @click="openAttributes(row)">属性</el-button>
            <el-button link type="danger" size="small" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="dialogTitle" width="520px" destroy-on-close>
      <el-form :model="form" label-width="100px">
        <el-form-item label="类型标识"><el-input v-model="form.name" placeholder="如 MySQL" /></el-form-item>
        <el-form-item label="显示名称"><el-input v-model="form.display_name" placeholder="如 MySQL数据库" /></el-form-item>
        <el-form-item label="图标"><el-input v-model="form.icon" placeholder="Element Plus 图标名" /></el-form-item>
        <el-form-item label="父类型">
          <el-tree-select v-model="form.parent_id" :data="parentOptions" :props="{ label: 'display_name', value: 'id' }" check-strictly clearable placeholder="无(根类型)" style="width:100%" />
        </el-form-item>
        <el-form-item label="抽象类型"><el-switch v-model="form.is_abstract" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="form.description" type="textarea" :rows="2" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="attrDialogVisible" title="属性定义" width="700px" destroy-on-close>
      <el-table :data="attributes" v-loading="attrLoading" max-height="400">
        <el-table-column prop="display_name" label="属性名" width="140" />
        <el-table-column prop="name" label="字段标识" width="140" />
        <el-table-column prop="value_type" label="类型" width="90" />
        <el-table-column prop="is_required" label="必填" width="70"><template #default="{row}"><el-tag :type="row.is_required ? 'danger' : 'info'" size="small">{{ row.is_required ? '是' : '否' }}</el-tag></template></el-table-column>
        <el-table-column prop="is_unique" label="唯一" width="70"><template #default="{row}"><el-tag :type="row.is_unique ? 'warning' : 'info'" size="small">{{ row.is_unique ? '是' : '否' }}</el-tag></template></el-table-column>
      </el-table>
      <template v-if="canAccessRoles(['cmdb_admin'])">
      <el-divider />
      <el-form :model="attrForm" inline>
        <el-form-item label="属性标识"><el-input v-model="attrForm.name" placeholder="如 ip_address" size="small" /></el-form-item>
        <el-form-item label="显示名"><el-input v-model="attrForm.display_name" placeholder="如 IP地址" size="small" /></el-form-item>
        <el-form-item label="类型">
          <el-select v-model="attrForm.value_type" size="small" style="width:110px">
            <el-option v-for="t in ['string','int','float','bool','date','datetime','json','enum']" :key="t" :label="t" :value="t" />
          </el-select>
        </el-form-item>
        <el-form-item><el-checkbox v-model="attrForm.is_required">必填</el-checkbox></el-form-item>
        <el-form-item><el-checkbox v-model="attrForm.is_unique">唯一</el-checkbox></el-form-item>
        <el-form-item><el-button type="primary" size="small" @click="handleAddAttr">添加属性</el-button></el-form-item>
      </el-form>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { getCITypeTree, createCIType, updateCIType, deleteCIType, getAttributes, createAttribute } from '@/api/ci-type'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const canAccessRoles = auth.canAccessRoles

const loading = ref(false)
const treeData = ref<any[]>([])
const dialogVisible = ref(false)
const dialogTitle = ref('新建 CI 类型')
const isEdit = ref(false)
const editId = ref<number>(0)
const form = reactive({ name: '', display_name: '', icon: 'server', parent_id: null as number | null, is_abstract: false, description: '' })
const parentOptions = computed(() => treeData.value)

const attrDialogVisible = ref(false)
const attrLoading = ref(false)
const attributes = ref<any[]>([])
const currentTypeId = ref(0)
const attrForm = reactive({ name: '', display_name: '', value_type: 'string', is_required: false, is_unique: false })

async function fetchData() {
  loading.value = true
  const res = await getCITypeTree()
  treeData.value = res.data || []
  loading.value = false
}

function resetForm() {
  form.name = ''; form.display_name = ''; form.icon = 'server'; form.parent_id = null; form.is_abstract = false; form.description = ''
}
function openCreate() { resetForm(); isEdit.value = false; dialogTitle.value = '新建 CI 类型'; dialogVisible.value = true }
function openEdit(row: any) { Object.assign(form, { name: row.name, display_name: row.display_name, icon: row.icon, parent_id: row.parent_id, is_abstract: row.is_abstract, description: row.description }); isEdit.value = true; editId.value = row.id; dialogTitle.value = '编辑 CI 类型'; dialogVisible.value = true }
async function handleSave() {
  try {
    if (isEdit.value) { await updateCIType(editId.value, form) }
    else { await createCIType(form) }
    ElMessage.success(isEdit.value ? '更新成功' : '创建成功')
    dialogVisible.value = false
    fetchData()
  } catch { /* handled */ }
}
async function handleDelete(row: any) {
  await ElMessageBox.confirm('确定删除该 CI 类型吗？', '提示', { type: 'warning' })
  await deleteCIType(row.id)
  ElMessage.success('删除成功')
  fetchData()
}

async function openAttributes(row: any) {
  currentTypeId.value = row.id
  attrDialogVisible.value = true
  attrLoading.value = true
  const res = await getAttributes(row.id)
  attributes.value = res.data || []
  attrLoading.value = false
}
async function handleAddAttr() {
  await createAttribute(currentTypeId.value, attrForm)
  ElMessage.success('属性添加成功')
  const res = await getAttributes(currentTypeId.value)
  attributes.value = res.data || []
}
onMounted(fetchData)
</script>
