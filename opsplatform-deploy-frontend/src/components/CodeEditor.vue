<template>
  <div ref="host" class="cm-host"></div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { EditorView, basicSetup } from 'codemirror'
import { EditorState } from '@codemirror/state'
import { yaml } from '@codemirror/lang-yaml'

const props = defineProps({ modelValue: { type: String, default: '' } })
const emit = defineEmits(['update:modelValue'])
const host = ref(null)
let view = null

onMounted(() => {
  view = new EditorView({
    parent: host.value,
    state: EditorState.create({
      doc: props.modelValue || '',
      extensions: [
        basicSetup,        // 行号、语法高亮、括号匹配、撤销、当前行高亮…（VSCode 式）
        yaml(),            // YAML 语言高亮
        EditorView.updateListener.of((u) => {
          if (u.docChanged) emit('update:modelValue', u.state.doc.toString())
        }),
      ],
    }),
  })
})

// 外部（切模式/换模板）改了内容时同步进编辑器
watch(() => props.modelValue, (v) => {
  if (view && v !== view.state.doc.toString()) {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: v || '' } })
  }
})

onBeforeUnmount(() => { if (view) view.destroy() })
</script>

<style scoped>
.cm-host { border: 1px solid var(--el-border-color); border-radius: 6px; overflow: hidden; }
.cm-host :deep(.cm-editor) { max-height: 440px; font-size: 13px; }
.cm-host :deep(.cm-scroller) { overflow: auto; font-family: 'Menlo', 'Consolas', monospace; }
.cm-host :deep(.cm-gutters) { background: #f6f8fa; border-right: 1px solid var(--el-border-color); }
.cm-host :deep(.cm-focused) { outline: none; }
</style>
