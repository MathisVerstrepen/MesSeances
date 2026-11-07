<script setup lang="ts">
const systems = [
  {
    id: 'laser',
    label: 'Laser 4K',
    short: '1 projecteur',
    resolution: 'Numérique 4K',
    ratio: 'Jusqu’à 1,90:1',
    aspectRatio: 1.9,
    name: 'IMAX Laser · simple laser',
    description:
      'La projection numérique courante des multiplexes IMAX, avec une image lumineuse et détaillée.',
  },
  {
    id: 'gt',
    label: 'Double laser',
    short: '2 projecteurs',
    resolution: 'Numérique 4K',
    ratio: 'Jusqu’à 1,43:1',
    aspectRatio: 1.43,
    name: 'IMAX · double laser',
    description:
      'Deux projecteurs laser et une optique adaptée permettent d’afficher les versions 1,43 sur un écran compatible.',
  },
  {
    id: 'film',
    label: 'Pellicule 70 mm',
    short: '1 projecteur à film',
    resolution: 'Pellicule 15/70',
    ratio: 'Jusqu’à 1,43:1',
    aspectRatio: 1.43,
    name: 'IMAX 70 mm · pellicule',
    description:
      'Une grande image photographique sur pellicule, pour une expérience rare. La séance doit porter la mention « IMAX 70 mm ».',
  },
  {
    id: 'xenon',
    label: 'Xénon 2K',
    short: '2 projecteurs',
    resolution: 'Numérique 2K',
    ratio: 'Jusqu’à 1,90:1',
    aspectRatio: 1.9,
    name: 'IMAX numérique · xénon',
    description:
      'L’ancienne génération numérique, avec deux projecteurs à lampes. Sa définition et son contraste sont inférieurs à ceux du laser.',
  },
] as const
const technicalDetails = {
  laser: {
    image: {
      width: 653,
      height: 1000,
      alt: 'Projecteur IMAX Laser 4K, avec son bloc optique et son châssis.',
    },
    caption: 'Un projecteur IMAX Laser 4K.',
    sections: [
      {
        title: 'Une lumière laser, une image numérique',
        text: 'Le laser est la source de lumière. L’image est formée par des puces DLP : des millions de micromiroirs orientent la lumière pour composer les pixels. Le film est lu depuis un fichier numérique, sans pellicule dans le projecteur.',
      },
      {
        title: 'Ce que le 4K apporte',
        text: 'Le 4K désigne une image d’environ 4 000 pixels de large. À proportions identiques, une matrice 4K possède quatre fois plus de pixels qu’une matrice 2K. Les détails visibles dépendent aussi du master du film, de la mise au point et de la distance à l’écran.',
      },
      {
        title: 'Pourquoi le cadre s’arrête à 1,90:1',
        text: 'La configuration simple laser présentée ici, souvent appelée CoLa pour Commercial Laser, utilise un seul projecteur. Elle peut montrer une version IMAX étendue en 1,90:1. Même installée devant un écran plus haut, elle ne remplit pas le cadre 1,43:1.',
      },
      {
        title: 'Au-delà du nombre de pixels',
        text: 'Le moteur optique et l’éclairage laser améliorent aussi le contraste, la luminosité et la gamme de couleurs par rapport à l’ancienne génération à lampes. Une projection 4K reste toutefois tributaire des informations présentes dans la version du film.',
      },
    ],
    sources: [
      {
        label: 'IMAX · Projection laser',
        url: 'https://www.imax.com/news/imax-laser-here',
      },
      {
        label: 'Texas Instruments · Les micromiroirs DLP',
        url: 'https://www.ti.com/about-ti/behind-chip/articles/the-inception-of-digital-cinema-and-the-journey-ahead.html',
      },
    ],
  },
  gt: {
    image: {
      width: 1200,
      height: 464,
      alt: 'Deux projecteurs IMAX laser côte à côte dans une cabine éclairée en bleu.',
    },
    caption: 'Une installation IMAX à double projection laser.',
    sections: [
      {
        title: 'Deux projecteurs 4K travaillent ensemble',
        text: 'Le système GT Laser, pour Grand Theatre, est conçu pour les très grands écrans. En 2D, les deux projecteurs superposent leurs images. Leur alignement est essentiel : une mauvaise superposition ferait perdre de la netteté au lieu d’en ajouter.',
      },
      {
        title: 'Deux fois 4K ne veut pas dire 8K',
        text: 'Les images occupent la même surface de l’écran. Les pixels ne sont pas assemblés côte à côte pour doubler la définition. Cette double projection permet notamment d’apporter la lumière nécessaire à une très grande image.',
      },
      {
        title: 'Le 1,43:1 demande toute une chaîne compatible',
        text: 'L’optique du système GT permet de restituer le grand cadre 1,43:1. Il faut aussi un écran adapté et une version du film qui contienne cette image. Deux projecteurs ne suffisent donc pas, à eux seuls, à garantir une séance en 1,43:1.',
      },
      {
        title: 'Et pour la 3D ?',
        text: 'Il faut envoyer une vue différente à chaque œil. Le système de projection et les lunettes assurent leur séparation. La luminosité disponible est particulièrement utile en 3D, car les lunettes absorbent une partie de la lumière.',
      },
    ],
    sources: [
      {
        label: 'IMAX Victoria · Double laser et format 1,43',
        url: 'https://imaxvictoria.com/faq/',
      },
      {
        label: 'IMAX · Système double laser 4K',
        url: 'https://static.imax.com/images/institutional/Feb2016/IMAX%20Laser%20Press%20Release%20Feb%204%5B3%5D.pdf',
      },
    ],
  },
  film: {
    image: {
      width: 1200,
      height: 900,
      alt: 'Projecteur argentique IMAX 70 mm dans sa cabine, avec ses galets de transport et son pupitre de commande.',
    },
    caption: 'Un projecteur argentique IMAX 70 mm en cabine.',
    sections: [
      {
        title: 'Un photogramme de 15 perforations',
        text: 'La copie de projection mesure 70 mm de large. Elle défile horizontalement, avec 15 perforations par image, contre 5 pour le 70 mm classique à défilement vertical. Chaque photogramme dispose ainsi d’une surface beaucoup plus grande.',
      },
      {
        title: 'Comment faire défiler une si grande image ?',
        text: 'Le mécanisme appelé rolling loop forme une boucle mobile dans la pellicule. Il permet de présenter les photogrammes successifs devant l’objectif. Des picots de repérage immobilisent précisément chaque image pendant sa projection, tandis que le reste du film continue son parcours.',
      },
      {
        title: 'Une image photographique, un son séparé',
        text: 'Une lampe puissante éclaire la pellicule. L’image provient directement du photogramme, avec son grain et les détails de la copie. Le son est lu séparément et synchronisé avec le film, il n’est pas porté par une piste sonore sur cette bande IMAX.',
      },
      {
        title: 'Pourquoi il n’y a pas de chiffre 4K ou 8K',
        text: 'La pellicule n’a pas de grille fixe de pixels. Le rendu dépend du tournage, des éventuelles étapes numériques, du tirage et de l’état de la copie. Le support 15/70 permet le cadre 1,43:1, mais toutes les scènes d’un film ne remplissent pas nécessairement ce cadre.',
      },
    ],
    sources: [
      {
        label: 'IMAX · Rapport technique et systèmes de projection',
        url: 'https://imaxcorporation.gcs-web.com/static-files/f8bc67ed-9d0f-4729-a2e5-921ef767b0e1',
      },
      {
        label: 'Documentation IMAX · Le mécanisme rolling loop',
        url: 'https://www.in70mm.com/presents/1970_imax/library/projector/index.htm',
      },
    ],
  },
  xenon: {
    image: {
      width: 740,
      height: 370,
      alt: 'Deux projecteurs numériques IMAX Digital à lampes xénon vus de face.',
    },
    caption: 'Deux projecteurs IMAX Digital à lampes xénon.',
    sections: [
      {
        title: 'Le xénon désigne la lampe',
        text: 'Un arc électrique dans une lampe au xénon produit la lumière. Comme avec le laser, l’image est numérique et formée par des puces DLP. Le mot xénon décrit donc l’éclairage du projecteur, pas le support du film.',
      },
      {
        title: 'Pourquoi deux projecteurs 2K ?',
        text: 'En 2D, les deux images 2K sont superposées et doivent être précisément alignées. Leur lumière s’additionne sur l’écran. Cela ne constitue pas une projection 4K native : les deux images ne forment pas une nouvelle matrice 4K.',
      },
      {
        title: 'Un cadre maximal de 1,90:1',
        text: 'Cette génération peut afficher une image IMAX étendue en 1,90:1. Elle ne restitue pas le grand cadre 1,43:1, même dans une salle équipée d’un écran à ces proportions. Le format d’une séance dépend également de la version du film.',
      },
      {
        title: 'Ce qui change avec le laser',
        text: 'La transition vers le laser apporte une matrice 4K et des progrès de contraste et de couleur. La définition n’explique cependant pas tout : le réglage des deux projecteurs, la qualité du master et l’entretien de l’installation influencent aussi ce que l’on voit.',
      },
    ],
    sources: [
      {
        label: 'IMAX · Générations laser et xénon',
        url: 'https://investors.imax.com/static-files/433f6acf-7f22-4fe3-a27c-78fd40000bc8',
      },
      {
        label: 'Texas Instruments · La projection numérique DLP',
        url: 'https://www.ti.com/about-ti/behind-chip/articles/the-inception-of-digital-cinema-and-the-journey-ahead.html',
      },
    ],
  },
} as const
const selectedId = ref('laser')
const selected = computed(
  () => systems.find((system) => system.id === selectedId.value) ?? systems[0],
)
const showTechnical = ref(false)
const technical = computed(() => technicalDetails[selected.value.id])
</script>

<template>
  <div class="border-2 border-ink bg-surface">
    <fieldset class="grid grid-cols-2 border-b-2 border-ink lg:grid-cols-4">
      <legend class="sr-only">Comparer les systèmes de projection IMAX</legend>
      <label
        v-for="system in systems"
        :key="system.id"
        class="flex min-h-12 cursor-pointer items-center justify-center border border-ink/20 p-3 text-center font-mono text-xs font-bold focus-within:ring-2 focus-within:ring-inset focus-within:ring-accent"
        :class="selectedId === system.id ? 'bg-highlight' : 'hover:bg-subtle'"
      >
        <input
          v-model="selectedId"
          type="radio"
          name="imax-projection"
          :value="system.id"
          class="sr-only"
          aria-controls="imax-projection-result"
        >{{ system.label }}
      </label>
    </fieldset>
    <div
      id="imax-projection-result"
      class="grid sm:grid-cols-[0.8fr_1.2fr]"
      aria-live="polite"
      aria-atomic="true"
    >
      <div
        class="flex items-center justify-center gap-3 bg-[#f1f0e9] px-5 py-6"
        aria-hidden="true"
      >
        <div class="grid gap-2">
          <span
            v-for="number in selected.id === 'gt' || selected.id === 'xenon' ? 2 : 1"
            :key="number"
            class="grid h-8 w-12 place-items-center border-2 border-ink bg-ink font-mono text-[10px] text-white"
            >{{
              selected.id === 'film' ? '15/70' : selected.id === 'xenon' ? '2K' : '4K'
            }}</span
          >
        </div>
        <svg viewBox="0 0 100 90" class="w-12 text-accent">
          <path d="m0 40 100-35v80L0 50Z" fill="currentColor" opacity=".16" />
          <path
            d="m0 40 100-35M0 50l100 35"
            stroke="currentColor"
            stroke-width="2"
            stroke-dasharray="4 4"
          />
        </svg>
        <div class="grid h-24 w-24 place-items-center">
          <span
            class="grid w-full place-items-center border-2 border-ink bg-accent font-mono text-sm font-bold text-white transition-[aspect-ratio] motion-reduce:transition-none"
            :style="{ aspectRatio: selected.aspectRatio }"
            >{{
              selected.aspectRatio.toLocaleString('fr-FR', { minimumFractionDigits: 2 })
            }}:1</span
          >
        </div>
      </div>
      <div class="border-t-2 border-ink p-5 sm:border-t-0 sm:border-l-2 sm:p-6">
        <p class="text-lg font-black text-accent">
          {{ selected.resolution }} · {{ selected.ratio }}
        </p>
        <h3 class="mt-3 text-sm font-bold">{{ selected.name }}</h3>
        <p class="mt-2 text-sm leading-relaxed">{{ selected.description }}</p>
      </div>
    </div>
    <div class="border-t border-line">
      <button
        type="button"
        class="flex min-h-11 w-full items-center justify-between gap-3 px-5 py-3 text-left text-xs font-bold hover:bg-subtle"
        :aria-expanded="showTechnical"
        aria-controls="imax-projection-technical"
        @click="showTechnical = !showTechnical"
      >
        Détails techniques
        <span aria-hidden="true">{{ showTechnical ? '−' : '+' }}</span>
      </button>
      <div
        v-if="showTechnical"
        id="imax-projection-technical"
        class="border-t border-line p-5 sm:p-6"
      >
        <div
          class="grid items-start gap-6 lg:grid-cols-[0.85fr_1.15fr] lg:gap-8"
        >
          <figure :key="selected.id">
            <img
              :src="`/images/guides/projecteur-imax-${selected.id}.webp`"
              :alt="technical.image.alt"
              :width="technical.image.width"
              :height="technical.image.height"
              class="max-h-[420px] w-full bg-black object-contain"
              loading="lazy"
              decoding="async"
            >
            <figcaption class="mt-2 text-xs leading-relaxed text-muted">
              {{ technical.caption }}
            </figcaption>
          </figure>
          <div class="grid gap-5">
            <div v-for="section in technical.sections" :key="section.title">
              <h4 class="text-sm font-bold">{{ section.title }}</h4>
              <p class="mt-2 text-sm leading-relaxed text-muted">
                {{ section.text }}
              </p>
            </div>
            <div class="border-t border-line pt-4">
              <p class="text-xs font-bold">Pour aller plus loin</p>
              <ul class="mt-2 grid gap-2 text-xs leading-relaxed text-muted">
                <li v-for="source in technical.sources" :key="source.url">
                  <a
                    :href="source.url"
                    class="underline decoration-ink/30 underline-offset-4 hover:text-accent"
                    >{{ source.label }}
                    ↗</a
                  >
                </li>
              </ul>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
