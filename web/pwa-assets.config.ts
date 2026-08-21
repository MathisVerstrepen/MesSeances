import { defineConfig, minimal2023Preset } from '@vite-pwa/assets-generator/config'

export default defineConfig({
  preset: {
    ...minimal2023Preset,
    maskable: {
      ...minimal2023Preset.maskable,
      resizeOptions: { background: '#ffcf3f' }
    },
    apple: {
      ...minimal2023Preset.apple,
      resizeOptions: { background: '#ffcf3f' }
    }
  },
  images: ['public/favicon.svg']
})
