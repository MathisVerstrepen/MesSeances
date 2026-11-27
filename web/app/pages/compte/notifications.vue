<script setup lang="ts">
import { BellPlus, LoaderCircle, Pencil, Trash2 } from '@lucide/vue'
import type {
  AccountNotification,
  NotificationRule,
  NotificationRuleDraft,
} from '~/types/notifications'
import {
  notificationKindLabel,
  notificationRuleTheaterCount,
  notificationScopeLabel,
} from '~/utils/notifications'

definePageMeta({ middleware: 'account-auth' })
useHead({ title: 'Notifications - MesSeances' })

type Tab = 'inbox' | 'alerts'
const route = useRoute()
const account = useAccountSession()
const center = useNotificationCenter()
const rules = useNotificationRules()
const push = useWebPush()
const digest = useNotificationDigest()
const emailAvailable = computed(() => !!digest.digest.value?.available)
const digestEnabled = computed(
  () => !!digest.digest.value && digest.digest.value.frequency !== 'off',
)
const follows = useCinemaFollows()
const preferences = useCinemaPreferences()

// Account pages strip URL fragments; the tab lives in the query instead.
const tab = computed<Tab>(() =>
  route.query.onglet === 'alertes' ? 'alerts' : 'inbox',
)
const now = ref(new Date())
const editing = ref<string | 'new' | null>(null)
const confirmingDelete = ref<string | null>(null)

const followedCount = computed(() => follows.ids.value.size)
const preferredCount = computed(
  () => preferences.favoriteTheaterIds.value.length,
)
const catalog = computed(() => preferences.theaters.value)
const theaterNames = computed(
  () => new Map(catalog.value.map((theater) => [theater.id, theater.name])),
)
const blankDraft: NotificationRuleDraft = {
  kind: 'watchlist_screening',
  theater_scope: 'followed',
  theater_ids: [],
  include_returns: false,
  enabled: true,
  push: false,
  email: false,
}
const formInitial = computed<NotificationRuleDraft>(() => {
  const rule = rules.rules.value.find((item) => item.id === editing.value)
  return rule
    ? {
        kind: rule.kind,
        theater_scope: rule.theater_scope,
        theater_ids: [...rule.theater_ids],
        include_returns: rule.include_returns,
        enabled: rule.enabled,
        push: rule.push,
        email: rule.email,
      }
    : blankDraft
})
const writesDisabled = computed(
  () => account.writesBlocked.value || rules.saving.value,
)

function selectTab(next: Tab) {
  if (next === tab.value) return
  void navigateTo(
    { query: next === 'alerts' ? { onglet: 'alertes' } : {} },
    { replace: true },
  )
}
function tabKeys(event: KeyboardEvent) {
  if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
  event.preventDefault()
  const next = tab.value === 'inbox' ? 'alerts' : 'inbox'
  selectTab(next)
  void nextTick(() =>
    document.getElementById(`notifications-tab-${next}`)?.focus(),
  )
}

function ruleScope(rule: NotificationRule) {
  const label = notificationScopeLabel(
    rule.theater_scope,
    notificationRuleTheaterCount(
      rule,
      followedCount.value,
      preferredCount.value,
    ),
  )
  if (rule.theater_scope !== 'custom') return label
  const names = rule.theater_ids
    .slice(0, 3)
    .map((id) => theaterNames.value.get(id) ?? id)
  const others = rule.theater_ids.length - names.length
  return `${label} : ${names.join(', ')}${others > 0 ? ` et ${others} autres` : ''}`
}

async function submitRule(draft: NotificationRuleDraft) {
  const id = editing.value === 'new' ? undefined : (editing.value ?? undefined)
  if (await rules.save(draft, id)) editing.value = null
}
async function toggleRule(rule: NotificationRule, enabled: boolean) {
  await rules.save(
    {
      kind: rule.kind,
      theater_scope: rule.theater_scope,
      theater_ids: [...rule.theater_ids],
      include_returns: rule.include_returns,
      enabled,
      push: rule.push,
      email: rule.email,
    },
    rule.id,
  )
}
async function deleteRule(id: string) {
  if (await rules.remove(id)) confirmingDelete.value = null
  if (editing.value === id) editing.value = null
}
function open(item: AccountNotification) {
  void center.markRead(item)
}

function loadAll() {
  if (!center.owner.value) return
  now.value = new Date()
  void center.load(true)
  void rules.load()
  void push.load()
  void digest.load()
}
// Start after hydration: loading flags must not differ from the server render.
onMounted(loadAll)
// Rules and push devices reload themselves when the owner changes.
watch(center.owner, (owner) => {
  if (!owner) return
  now.value = new Date()
  void center.load(true)
})
</script>

<template>
  <AccountShell
    title="Notifications"
    account-area
    back-to-account
    hide-explore
    hide-logout
  >
    <div
      role="tablist"
      aria-label="Notifications"
      class="mb-8 flex border-b-2 border-ink"
      @keydown="tabKeys"
    >
      <button
        id="notifications-tab-inbox"
        type="button"
        role="tab"
        aria-controls="notifications-panel-inbox"
        :aria-selected="tab === 'inbox'"
        :tabindex="tab === 'inbox' ? 0 : -1"
        class="-mb-0.5 min-h-12 border-2 border-b-0 px-4 font-mono text-[0.72rem] font-extrabold uppercase tracking-[0.06em]"
        :class="tab === 'inbox' ? 'border-ink bg-surface' : 'border-transparent text-muted hover:text-ink'"
        @click="selectTab('inbox')"
      >
        Reçues<span v-if="center.unreadCount.value">
          ({{ center.unreadCount.value }})</span
        >
      </button>
      <button
        id="notifications-tab-alerts"
        type="button"
        role="tab"
        aria-controls="notifications-panel-alerts"
        :aria-selected="tab === 'alerts'"
        :tabindex="tab === 'alerts' ? 0 : -1"
        class="-mb-0.5 min-h-12 border-2 border-b-0 px-4 font-mono text-[0.72rem] font-extrabold uppercase tracking-[0.06em]"
        :class="tab === 'alerts' ? 'border-ink bg-surface' : 'border-transparent text-muted hover:text-ink'"
        @click="selectTab('alerts')"
      >
        Alertes<span v-if="rules.snapshot.value">
          ({{ rules.rules.value.length }})</span
        >
      </button>
    </div>

    <section
      v-show="tab === 'inbox'"
      id="notifications-panel-inbox"
      role="tabpanel"
      aria-labelledby="notifications-tab-inbox"
      :aria-busy="center.loading.value"
    >
      <div v-if="center.loading.value && !center.loaded.value">
        <p role="status" class="sr-only">Chargement des notifications…</p>
        <div class="space-y-3 motion-safe:animate-pulse" aria-hidden="true">
          <div v-for="n in 3" :key="n" class="h-20 bg-subtle" />
        </div>
      </div>
      <div
        v-else-if="center.error.value && !center.items.value.length"
        class="space-y-4"
      >
        <p role="alert" class="account-alert">{{ center.error.value }}</p>
        <button
          type="button"
          class="account-secondary"
          :disabled="account.writesBlocked.value"
          @click="center.load(true)"
        >
          Réessayer
        </button>
      </div>
      <div v-else-if="!center.items.value.length" class="space-y-4">
        <h2 class="account-heading">Aucune notification</h2>
        <p class="max-w-prose text-sm leading-relaxed">
          Les notifications apparaissent ici quand une de vos alertes se
          déclenche : nouveautés dans vos cinémas ou première séance d’un film
          de votre watchlist.
        </p>
        <button
          type="button"
          class="account-primary"
          @click="selectTab('alerts')"
        >
          <BellPlus :size="17" aria-hidden="true" />Configurer une alerte
        </button>
      </div>
      <template v-else>
        <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
          <p class="text-sm text-muted">Conservées 90 jours.</p>
          <button
            v-if="center.unreadCount.value"
            type="button"
            class="account-link"
            :disabled="center.pendingRead.value || account.writesBlocked.value"
            @click="center.markAllRead()"
          >
            Tout marquer comme lu
          </button>
        </div>
        <p
          v-if="center.actionError.value"
          role="alert"
          class="account-alert mb-4"
        >
          {{ center.actionError.value }}
        </p>
        <ul class="divide-y divide-ink/15 border-y-2 border-ink">
          <li v-for="item in center.items.value" :key="item.id">
            <NotificationListItem :item="item" :now="now" @open="open" />
          </li>
        </ul>
        <div v-if="center.nextCursor.value" class="mt-6 space-y-3">
          <p v-if="center.error.value" role="alert" class="account-alert">
            Impossible de charger la suite. {{ center.error.value }}
          </p>
          <button
            type="button"
            class="account-secondary"
            :disabled="center.morePending.value || account.writesBlocked.value"
            @click="center.loadMore()"
          >
            <LoaderCircle
              v-if="center.morePending.value"
              :size="17"
              class="motion-safe:animate-spin"
              aria-hidden="true"
            />
            {{ center.morePending.value ? 'Chargement…' : 'Afficher plus' }}
          </button>
        </div>
      </template>
    </section>

    <section
      v-show="tab === 'alerts'"
      id="notifications-panel-alerts"
      role="tabpanel"
      aria-labelledby="notifications-tab-alerts"
      :aria-busy="rules.loading.value || rules.saving.value"
      class="space-y-6"
    >
      <p class="max-w-prose text-sm leading-relaxed">
        Chaque alerte choisit ses cinémas. Elle ne concerne que les
        programmations détectées après sa création. Les notifications
        s’affichent dans la cloche en haut de page.
      </p>
      <div v-if="rules.loading.value && !rules.snapshot.value">
        <p role="status" class="sr-only">Chargement des alertes…</p>
        <div
          class="h-24 bg-subtle motion-safe:animate-pulse"
          aria-hidden="true"
        />
      </div>
      <div
        v-else-if="rules.error.value && !rules.snapshot.value"
        class="space-y-4"
      >
        <p role="alert" class="account-alert">{{ rules.error.value }}</p>
        <button
          type="button"
          class="account-secondary"
          :disabled="account.writesBlocked.value"
          @click="rules.load()"
        >
          Réessayer
        </button>
      </div>
      <template v-else-if="rules.snapshot.value">
        <p v-if="rules.actionError.value" role="alert" class="account-alert">
          {{ rules.actionError.value }}
        </p>
        <ul
          v-if="rules.rules.value.length"
          class="space-y-4"
          aria-label="Mes alertes"
        >
          <li v-for="rule in rules.rules.value" :key="rule.id">
            <NotificationRuleForm
              v-if="editing === rule.id"
              :initial="formInitial"
              editing
              :saving="rules.saving.value"
              :disabled="writesDisabled"
              :max-custom-theaters="rules.snapshot.value.max_custom_theaters"
              :followed-count="followedCount"
              :preferred-count="preferredCount"
              :push-available="push.available.value"
              :push-device-count="push.devices.value.length"
              :email-available="emailAvailable"
              :digest-enabled="digestEnabled"
              :theaters="catalog"
              @submit="submitRule"
              @cancel="editing = null"
            />
            <article
              v-else
              class="flex flex-col gap-4 border-2 border-ink bg-surface p-4 sm:flex-row sm:items-start sm:justify-between"
              :class="rule.enabled ? '' : 'opacity-70'"
            >
              <div class="min-w-0 space-y-1">
                <h3 class="font-bold">
                  {{ notificationKindLabel(rule.kind) }}
                </h3>
                <p class="text-sm [overflow-wrap:anywhere]">
                  {{ ruleScope(rule) }}
                </p>
                <p
                  v-if="rule.include_returns || (rule.push && push.available.value) || (rule.email && emailAvailable)"
                  class="text-xs text-muted"
                >
                  {{
                    [
                      rule.include_returns ? 'Reprises incluses' : '',
                      rule.push && push.available.value ? 'Push' : '',
                      rule.email && emailAvailable ? 'E-mail' : '',
                    ]
                      .filter(Boolean)
                      .join(' · ')
                  }}
                </p>
              </div>
              <div class="flex shrink-0 flex-wrap items-center gap-2">
                <label
                  class="flex min-h-11 cursor-pointer items-center gap-2 px-2 text-sm font-semibold"
                >
                  <input
                    type="checkbox"
                    class="size-5 accent-ink"
                    :checked="rule.enabled"
                    :disabled="writesDisabled"
                    @change="toggleRule(rule, ($event.target as HTMLInputElement).checked)"
                  >
                  Active
                </label>
                <button
                  type="button"
                  class="grid size-11 place-items-center border-2 border-ink hover:bg-highlight disabled:opacity-50"
                  :aria-label="`Modifier l’alerte ${notificationKindLabel(rule.kind)}`"
                  :disabled="writesDisabled"
                  @click="editing = rule.id; confirmingDelete = null"
                >
                  <Pencil :size="17" aria-hidden="true" />
                </button>
                <template v-if="confirmingDelete === rule.id">
                  <button
                    type="button"
                    class="account-danger min-h-11"
                    :disabled="writesDisabled"
                    @click="deleteRule(rule.id)"
                  >
                    Supprimer
                  </button>
                  <button
                    type="button"
                    class="account-link"
                    @click="confirmingDelete = null"
                  >
                    Annuler
                  </button>
                </template>
                <button
                  v-else
                  type="button"
                  class="grid size-11 place-items-center border-2 border-ink hover:bg-primary-soft disabled:opacity-50"
                  :aria-label="`Supprimer l’alerte ${notificationKindLabel(rule.kind)}`"
                  :disabled="writesDisabled"
                  @click="confirmingDelete = rule.id"
                >
                  <Trash2 :size="17" aria-hidden="true" />
                </button>
              </div>
            </article>
          </li>
        </ul>
        <p v-else-if="editing !== 'new'" class="text-sm font-semibold">
          Aucune alerte configurée.
        </p>
        <NotificationRuleForm
          v-if="editing === 'new'"
          :initial="blankDraft"
          :editing="false"
          :saving="rules.saving.value"
          :disabled="writesDisabled"
          :max-custom-theaters="rules.snapshot.value.max_custom_theaters"
          :followed-count="followedCount"
          :preferred-count="preferredCount"
          :push-available="push.available.value"
          :push-device-count="push.devices.value.length"
          :email-available="emailAvailable"
          :digest-enabled="digestEnabled"
          :theaters="catalog"
          @submit="submitRule"
          @cancel="editing = null"
        />
        <div v-else>
          <button
            type="button"
            class="account-primary"
            :disabled="writesDisabled || rules.atLimit.value"
            @click="editing = 'new'; confirmingDelete = null"
          >
            <BellPlus :size="17" aria-hidden="true" />Créer une alerte
          </button>
          <p v-if="rules.atLimit.value" class="mt-2 text-xs text-muted">
            Nombre maximal d’alertes atteint ({{
              rules.snapshot.value.max_rules
            }}).
          </p>
        </div>
      </template>
      <NotificationPushDevices :state="push" />
      <NotificationDigestSettings :state="digest" />
      <NotificationTestSend :push="push" :digest="digest" />
    </section>
  </AccountShell>
</template>
