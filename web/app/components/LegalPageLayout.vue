<script setup lang="ts">
defineProps<{
  eyebrow: string
  title: string
}>()
</script>

<template>
  <StaticPageLayout :eyebrow="eyebrow" :title="title">
    <article class="legal-document mx-auto grid max-w-5xl gap-10 px-4 py-10 sm:px-6 sm:py-14 lg:px-10 lg:py-16">
      <slot />
    </article>
  </StaticPageLayout>
</template>

<style scoped>
.legal-document :deep(.legal-draft) {
  border: 2px solid #27272a;
  background: #fff3a3;
  box-shadow: 6px 6px 0 #27272a;
  padding: 1.25rem;
}

.legal-document :deep(.legal-draft h2) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.75rem;
  font-weight: 900;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

.legal-document :deep(.legal-draft p) {
  margin-top: 0.75rem;
  font-weight: 700;
  line-height: 1.65;
}

.legal-document :deep(.legal-section) {
  border-top: 2px solid #27272a;
  padding-top: 1.5rem;
}

.legal-document :deep(.legal-section h2) {
  max-width: 44rem;
  font-size: clamp(1.65rem, 4vw, 2.35rem);
  font-weight: 900;
  letter-spacing: -0.035em;
  line-height: 1;
  text-transform: uppercase;
}

.legal-document :deep(.legal-section h3) {
  margin-top: 1.75rem;
  font-size: 1.05rem;
  font-weight: 900;
}

.legal-document :deep(.legal-section p),
.legal-document :deep(.legal-section li),
.legal-document :deep(.legal-list dd) {
  line-height: 1.75;
}

.legal-document :deep(.legal-section p),
.legal-document :deep(.legal-section ul),
.legal-document :deep(.legal-section ol),
.legal-document :deep(.legal-list) {
  margin-top: 1rem;
}

.legal-document :deep(.legal-section ul),
.legal-document :deep(.legal-section ol) {
  padding-left: 1.35rem;
}

.legal-document :deep(.legal-section ul) {
  list-style: square;
}

.legal-document :deep(.legal-section ol) {
  list-style: decimal;
}

.legal-document :deep(.legal-section li + li) {
  margin-top: 0.65rem;
}

.legal-document :deep(.legal-list) {
  display: grid;
  gap: 1rem;
}

.legal-document :deep(.legal-list > div) {
  border-left: 4px solid var(--color-highlight);
  background: #fcfaf8;
  padding: 0.9rem 1rem;
}

.legal-document :deep(.legal-list dt) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.7rem;
  font-weight: 900;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.legal-document :deep(.legal-list dd) {
  margin-top: 0.35rem;
}

.legal-document :deep(.legal-placeholder) {
  display: inline;
  background: #ffcf3f;
  box-decoration-break: clone;
  padding: 0.1rem 0.3rem;
  font-weight: 900;
  -webkit-box-decoration-break: clone;
}

.legal-document :deep(a) {
  font-weight: 800;
  text-decoration: underline;
  text-decoration-thickness: 2px;
  text-underline-offset: 3px;
}

.legal-document :deep(a:hover) {
  color: #991b1b;
}

.legal-document :deep(code) {
  overflow-wrap: anywhere;
  border: 1px solid #27272a;
  background: #fcfaf8;
  padding: 0.1rem 0.3rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.85em;
}

</style>
