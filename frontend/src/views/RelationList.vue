<template>
  <div class="page">
    <div class="page-header"><h3>关系管理</h3></div>
    <el-tabs v-model="activeTab">
      <el-tab-pane label="关系规则" name="rules">
        <div style="margin-bottom:12px;text-align:right">
          <el-button type="primary" size="small" @click="openRuleCreate"><el-icon><Plus /></el-icon>新建规则</el-button>
        </div>
        <el-table :data="rules" v-loading="rulesLoading" stripe>
          <el-table-column prop="display_name" label="关系名" width="150" />
          <el-table-column prop="source_type.display_name" label="源类型" width="150" />
          <el-table-column prop="target_type.display_name" label="目标类型" width="150" />
          <el-table-column prop="cardinality" label="基数" width="80" />
          <el-table-column prop="reverse_name" label="反向关系" width="120" />
          <el-table-column prop="is_hard_dependency" label="强依赖" width="90">
            <template #default="{row}"><el-tag :type="row.is_hard_dependency ? 'danger' : 'info'" size="small">{{ row.is_hard_dependency ? '是' : '否' }}</el-tag></template>
          </el-table-column>
          <el-table-column label="操作" width="140">
            <template #default="{row}"><el-button link type="primary" size="small" @click="openRuleEdit(row)">编辑</el-button><el-button link type="danger" size="small" @click="handleRuleDelete(row)">删除</el-button></template>
          </el-table-column>
        </el-table>
      </el-tab-pane>
      <el-tab-pane label="关系实例" name="instances">
        <div style="margin-bottom:12px;text-align:right">
          <el-button type="primary" size="small" @click="openInstanceCreate"><el-icon><Plus /></el-icon>新建关系</el-button>
        </div>
        <el-table :data="instances" v-loading="instLoading" stripe>
          <el-table-column prop="rule.display_name" label="关系类型" width="140" />
          <el-table-column prop="source_ci.name" label="源 CI" min-width="180" />
          <el-table-column prop="target_ci.name" label="目标 CI" min-width="180" />
          <el-table-column label="操作" width="80">
            <template #default="{row}"><el-button link type="danger" size="small" @click="handleInstanceDelete(row)">删除</el-button></template>
          </el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="ruleDialogVisible" title="关系规则" width="520px" destroy-on-close>
      <el-form :model="ruleForm" label-width="100px">
        <el-form-item label="关系标识"><el-input v-model="ruleForm.name" placeholder="如 runs_on" /></el-form-item>
        <el-form-item label="显示名称"><el-input v-model="ruleForm.display_name" placeholder="如 运行在" /></el-form-item>
        <el-form-item label="反向名称"><el-input v-model="ruleForm.reverse_name" placeholder="如 运行着" /></el-form-item>
        <el-form-item label="源类型"><el-input v-model.number="ruleForm.source_type_id" placeholder="源CI类型ID" /></el-form-item>
        <el-form-item label="目标类型"><el-input v-model.number="ruleForm.target_type_id" placeholder="目标CI类型ID" /></el-form-item>
        <el-form-item label="能否强依赖"><el-switch v-model="ruleForm.is_hard_dependency" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="ruleDialogVisible = false">取消</el-button><el-button type="primary" @click="handleRuleSave">保存</el-button></template>
    </el-dialog>

    <el-dialog v-model="instDialogVisible" title="新建关系实例" width="400px" destroy-on-close>
      <el-form :model="instForm" label-width="100px">
        <el-form-item label="关系规则ID"><el-input v-model.number="instForm.rule_id" /></el-form-item>
        <el-form-item label="源CI ID"><el-input v-model.number="instForm.source_ci_id" /></el-form-item>
        <el-form-item label="目标CI ID"><el-input v-model.number="instForm.target_ci_id" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="instDialogVisible = false">取消</el-button><el-button type="primary" @click="handleInstSave">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getRelationRules, createRelationRule, updateRelationRule, deleteRelationRule, getRelationInstances, createRelationInstance, deleteRelationInstance } from '@/api/relation'

const activeTab = ref('rules')
const rulesLoading = ref(false)
const instLoading = ref(false)
const rules = ref<any[]>([])
const instances = ref<any[]>([])

async function fetchRules() { rulesLoading.value = true; const res = await getRelationRules(); rules.value = res.data || []; rulesLoading.value = false }
async function fetchInstances() { instLoading.value = true; const res = await getRelationInstances(); instances.value = res.data || []; instLoading.value = false }

const ruleDialogVisible = ref(false)
const isRuleEdit = ref(false)
const ruleEditId = ref(0)
const ruleForm = reactive({ name: '', display_name: '', reverse_name: '', source_type_id: 0, target_type_id: 0, is_hard_dependency: false })
function openRuleCreate() { Object.assign(ruleForm, { name: '', display_name: '', reverse_name: '', source_type_id: 0, target_type_id: 0, is_hard_dependency: false }); isRuleEdit.value = false; ruleDialogVisible.value = true }
function openRuleEdit(row: any) { Object.assign(ruleForm, row); isRuleEdit.value = true; ruleEditId.value = row.id; ruleDialogVisible.value = true }
async function handleRuleSave() { if (isRuleEdit.value) { await updateRelationRule(ruleEditId.value, ruleForm) } else { await createRelationRule(ruleForm) }; ElMessage.success('保存成功'); ruleDialogVisible.value = false; fetchRules() }
async function handleRuleDelete(row: any) { await ElMessageBox.confirm('确定删除?', '提示', { type: 'warning' }); await deleteRelationRule(row.id); ElMessage.success('删除成功'); fetchRules() }

const instDialogVisible = ref(false)
const instForm = reactive({ rule_id: 0, source_ci_id: 0, target_ci_id: 0 })
function openInstanceCreate() { instForm.rule_id = 0; instForm.source_ci_id = 0; instForm.target_ci_id = 0; instDialogVisible.value = true }
async function handleInstSave() { await createRelationInstance(instForm); ElMessage.success('创建成功'); instDialogVisible.value = false; fetchInstances() }
async function handleInstanceDelete(row: any) { await ElMessageBox.confirm('确定删除?', '提示', { type: 'warning' }); await deleteRelationInstance(row.id); ElMessage.success('删除成功'); fetchInstances() }

onMounted(() => { fetchRules(); fetchInstances() })
</script>