<script setup lang="ts">
import { Film } from '@lucide/vue'
import { posterImageSources } from '~/utils/safeImageUrl'

defineOptions({ inheritAttrs: false })

type ResetKey = string | number | boolean | null | undefined

const props = withDefaults(defineProps<{
  src: string | null | undefined
  alt: string
  sizes: string
  resetKey?: ResetKey
  fallbackText?: string | null
  fallbackIconSize?: number
  fallbackVariant?: 'labelled' | 'compact' | 'icon-only'
  imageClass?: string
  fallbackClass?: string
  fallbackMarker?: string
}>(), {
  resetKey: undefined,
  fallbackText: 'Affiche indisponible',
  fallbackIconSize: 30,
  fallbackVariant: 'labelled',
  imageClass: '',
  fallbackClass: '',
  fallbackMarker: undefined
})

const attrs = useAttrs()
const image = ref<HTMLImageElement | null>(null)
const failedSource = ref<string | null>(null)
const imageSources = computed(() => posterImageSources(props.src))
const normalizedSource = computed(() => imageSources.value.src)
const normalizedSizes = computed(() => {
  const sizes = props.sizes.trim()
  if (!sizes) throw new TypeError('PosterImage sizes must be non-empty')
  return sizes
})
const imageVisible = computed(() => Boolean(normalizedSource.value) && failedSource.value !== normalizedSource.value)
const imageKey = computed(() => `${normalizedSource.value ?? ''}:${String(props.resetKey ?? '')}`)

const protectedImageAttrs = new Set([
  'src',
  'srcset',
  'sizes',
  'width',
  'height',
  'loading',
  'decoding',
  'fetchpriority',
  'fetch-priority'
])

function forwardedImageAttrs() {
  return Object.fromEntries(Object.entries(attrs).filter(([key]) => key !== 'class' && key !== 'style' && !protectedImageAttrs.has(key.toLowerCase())))
}

function detectCachedFailure() {
  const currentImage = image.value
  const source = normalizedSource.value
  if (source && currentImage?.complete && currentImage.naturalWidth === 0) failedSource.value = source
}

function handleError(event: Event) {
  const currentImage = event.currentTarget
  const source = normalizedSource.value
  if (!(currentImage instanceof HTMLImageElement) || currentImage !== image.value || !source) return
  if (currentImage.src === source) failedSource.value = source
}

watch([normalizedSource, () => props.resetKey], async () => {
  failedSource.value = null
  await nextTick()
  detectCachedFailure()
})

onMounted(() => nextTick(detectCachedFailure))
</script>

<template>
  <span class="poster-image" :class="$attrs.class" :style="$attrs.style">
    <img
      v-if="imageVisible"
      :key="imageKey"
      ref="image"
      v-bind="forwardedImageAttrs()"
      :src="normalizedSource!"
      :srcset="imageSources.srcset ?? undefined"
      :sizes="normalizedSizes"
      :alt="alt"
      width="500"
      height="750"
      loading="lazy"
      decoding="async"
      :class="imageClass"
      @error="handleError"
    />
    <span
      v-else
      :data-poster-fallback="fallbackMarker"
      class="poster-fallback"
      :class="[`poster-fallback--${fallbackVariant}`, fallbackClass]"
    >
      <Film :size="fallbackIconSize" aria-hidden="true" />
      <span v-if="fallbackVariant !== 'icon-only' && fallbackText !== null">{{ fallbackText }}</span>
    </span>
  </span>
</template>

<style scoped>
.poster-image {
  display: block;
}

.poster-fallback {
  display: flex;
  height: 100%;
  width: 100%;
  align-items: center;
  justify-content: center;
}

.poster-fallback--labelled,
.poster-fallback--compact {
  flex-direction: column;
}
</style>
