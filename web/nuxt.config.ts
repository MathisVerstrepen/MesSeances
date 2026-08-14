import tailwindcss from '@tailwindcss/vite'

export default defineNuxtConfig({
  compatibilityDate: '2024-11-01',
  devtools: { enabled: false },
  css: ['~/assets/css/main.css'],
  vite: {
    plugins: [tailwindcss()]
  },
  app: {
    head: {
      htmlAttrs: { lang: 'fr' },
      title: 'MovieFlow — Vos séances, au bon moment',
      meta: [
        {
          name: 'description',
          content: 'Explorez les séances de cinéma de Lille sur une frise horaire et trouvez celles qui tiennent dans votre créneau.'
        },
        { name: 'theme-color', content: '#f7f6f2' }
      ]
    }
  },
  runtimeConfig: {
    public: {
      apiBase: 'http://localhost:8080'
    }
  },
  typescript: {
    strict: true,
    typeCheck: true
  }
})
