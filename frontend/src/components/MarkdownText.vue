<template>
  <div class="md-body" v-html="html"></div>
</template>

<script setup>
import { computed } from 'vue'
import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'

const props = defineProps({
  text: { type: String, default: '' },
})

const md = new MarkdownIt({
  html: false,
  linkify: true,
  breaks: true,
})

// 渲染出的链接一律新窗口打开，并阻断 opener 反向引用
DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node.tagName === 'A') {
    node.setAttribute('target', '_blank')
    node.setAttribute('rel', 'noopener')
  }
})

const html = computed(() => DOMPurify.sanitize(md.render(props.text || '')))
</script>
