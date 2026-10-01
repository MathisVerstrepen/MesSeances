# MesSeances design guidelines

Public pages, account screens and admin tools share one editorial design. This document describes that visual language and how to extend it without introducing a second theme. Different content densities are appropriate; different visual identities are not.

## 1. Design principles

- **Editorial, not generic dashboard:** strong typography, visible structure, bold ink borders and hard offset shadows.
- **Content first:** movie posters, titles, showtimes and actions take precedence over decorative containers.
- **One visual system:** reuse colors, controls and state components across public, account and admin routes.
- **Simple hierarchy:** separate sections with spacing, headings and rules. Avoid nested cards and the box-in-box pattern.
- **Minimal copy:** use one concise, self-explanatory heading or label. Do not add subtitles, helper text or descriptions by default. Add supporting text only when requested or necessary to prevent misunderstanding or error.
- **Functional density:** browsing can be spacious; scheduling and review tables can be compact. Preserve readable text, clear actions and keyboard access in both.

## 2. Source of truth

Use the existing implementation before creating new visual primitives. Tailwind is the default styling tool. Add scoped CSS for component-specific needs; do not create route-wide overrides or a separate admin stylesheet.

| Source | Responsibility |
| --- | --- |
| [main.css](web/app/assets/css/main.css) | Color tokens, body/focus defaults, shared `editorial-*` classes and reduced-motion defaults |
| [AppHeader.vue](web/app/components/AppHeader.vue) and [AppFooter.vue](web/app/components/AppFooter.vue) | Shared brand, navigation and site framing |
| [StaticPageLayout.vue](web/app/components/StaticPageLayout.vue) | Large editorial hero, section rules and grid-paper background |
| [EditorialStatePanel.vue](web/app/components/EditorialStatePanel.vue) | Loading, empty, error and other standalone states |
| [AccountShell.vue](web/app/components/AccountShell.vue) | Account layouts and scoped account control variants |
| [MovieCatalogCard.vue](web/app/components/MovieCatalogCard.vue) | Poster framing, movie-title hierarchy and compact metadata |
| [AdminMoviesGrid.client.vue](web/app/components/admin/AdminMoviesGrid.client.vue) | Dense AG Grid adaptation using shared color tokens |

Treat these as working examples, not interchangeable page layouts. In particular, `AccountShell` manages account-session behavior; do not reuse it merely to obtain its appearance on an unrelated route. Its scoped `account-*` controls follow the same design as the global editorial controls, not an alternative theme.

## 3. Color

Prefer semantic Tailwind utilities such as `bg-canvas`, `text-ink`, `border-ink` and `bg-highlight`. Tokens are defined in `main.css`; change token values there when an intentional system-wide change is needed.

| Token | Value | Intended use |
| --- | --- | --- |
| `canvas` | `#fcfaf8` | Warm page background |
| `surface` | `#ffffff` | Fields, cards, dialogs and table surfaces |
| `ink` | `#27272a` | Main text, structural borders and solid primary controls |
| `muted` | `#71717a` | Secondary metadata, never essential information made unreadable |
| `highlight` | `#a8bfa3` | Selected states, header fills, hover accents and decorative blocks |
| `primary` | `#991b1b` | Red brand accent, emphasized links and error/destructive states |
| `primary-hover` | `#7f1d1d` | Darker red interaction variant |
| `primary-soft` / `primary-line` | `#fef2f2` / `#fecaca` | Red-tinted semantic surfaces and accents |
| `accent` | `#1f6f78` | Teal focus and supporting accents |
| `accent-hover` | `#185a62` | Darker teal variant |
| `accent-soft` / `accent-line` | `#eff7f8` / `#a8cdd1` | Teal-tinted semantic surfaces and accents |
| `subtle` | `#f4f4f5` | Secondary neutral fill |
| `line` / `line-hover` | `#e4e4e7` / `#a1a1aa` | Available subtle dividers, not default card/control borders |

Existing decorative colors include paper `#f8f7f2`, brand yellow `#ffcf3f`, and poster-placeholder neutral `#e8e6de`. Keep these confined to their existing roles; do not introduce arbitrary page palettes. The app currently uses a light color scheme.

Green, amber and red may distinguish operational statuses when needed. Always accompany color with a text label or meaningful icon. Do not use color alone to communicate selection, errors or availability.

## 4. Typography

- **Body:** system sans-serif via `font-sans`; preserve normal case and comfortable line height for reading.
- **Page titles:** heavy weight, tight tracking and compact line height. `editorial-title` uses Noto Sans Variable, `font-black`, `clamp(2rem,6vw,3.5rem)`, line height `1.05` and tracking `-0.065em`.
- **Section headings:** `editorial-heading` uses Noto Sans Variable, `text-xl`, `font-black`, tight leading and tracking `-0.035em`.
- **Large public heroes:** use the established `StaticPageLayout` treatment, with oversized responsive uppercase headings. Do not apply hero-scale typography to forms or dense admin tools.
- **Labels and metadata:** use `font-mono`, bold weight and selective uppercase/letter spacing for navigation, dates, counters and short labels. Keep long content and prose in sans-serif.
- **Numeric columns:** use tabular numerals and right alignment when comparing counts, durations or totals.

The Noto Sans Variable font is loaded locally in `main.css`; it is explicitly selected for shared editorial headings rather than replacing the default body font. Maintain semantic heading order, one main page heading, and readable wrapping for long French titles.

## 5. Layout, spacing and responsive behavior

- Reuse the shared app header/footer. Choose a content width appropriate to the task rather than forcing all pages into one container: public layouts commonly cap at `1440px`, while forms and admin tools use narrower or task-specific widths.
- Start with mobile layout. Common outer gutters are `px-4`, then `sm:px-6` and larger desktop gutters. Use Tailwind spacing consistently: gaps of `2`/`3` within controls, `4`/`6` between related blocks, and larger gaps or padding for major sections.
- Separate major sections with `border-b-2`, `border-t-2` or `border-y-2 border-ink` where appropriate. Use whitespace instead of wrapping every section in a card.
- Stack actions and filters on narrow screens; allow wrapping. Use `min-w-0`, flexible grid tracks and explicit long-text wrapping to prevent page overflow.
- Dense tables, grids and timelines may scroll horizontally inside their own container. Do not cause document-wide horizontal scrolling or shrink controls until they become unusable.
- Keep `min-h-11` (44px) for standard standalone controls; account inputs/actions often use `min-h-12` (48px). Compact grid-cell controls are task-specific variants, not a new default for forms.
- Verify narrow mobile widths, including 320px and 375px, plus a desktop viewport such as 1440px. Check long labels, loading/empty/error states and open dialogs, not only ideal content.

Grid-paper backgrounds are an existing editorial motif: paper fill, low-opacity ink lines and a `28px` grid. Use them for framing sections, not behind dense text or every card. Small rotated accent blocks are decorative; keep them away from important content and interactions.

## 6. Borders, corners and shadows

- Use **2px ink borders** for controls, key cards, poster frames, state panels and dialogs: `border-2 border-ink`.
- Prefer square corners (`rounded-none`). The brand mark and some existing components use a very small `rounded-[3px]` radius; this is not permission to introduce rounded dashboard cards or pill-shaped controls throughout the app.
- Use **hard offset shadows without blur**. Existing examples include `shadow-[3px_3px_0_#27272a]` for small accents, `shadow-[5px_5px_0_#27272a]` for poster cards, and 6px-8px offsets for prominent state panels.
- Primary editorial buttons use a 3px highlight-colored offset shadow. Error controls can use red borders rather than ink when their semantic role requires it.
- Reserve shadows for useful emphasis. Do not stack shadows on every nested block or substitute soft `shadow-sm` cards for the editorial treatment.

## 7. Controls and interaction

Use the shared classes from `main.css` instead of copying old soft-style control recipes.

| Class | Use and appearance |
| --- | --- |
| `editorial-field` | Full-width square field, 2px ink border, white surface, minimum 44px height and inset highlight on focus |
| `editorial-button` | Primary action, ink fill, white text, mono bold label, highlight offset shadow, red hover |
| `editorial-button-outline` | Secondary action, white fill, ink border/text, highlight hover |
| `editorial-button-danger` | Destructive action, red border/text and soft red fill, solid red/white hover |
| `editorial-title` | Standard responsive page title |
| `editorial-heading` | Section heading |
| `editorial-alert` | Red-bordered semantic alert styling; caller provides spacing and appropriate alert semantics |

```vue
<label for="movie-search" class="block font-mono text-xs font-bold">
  Rechercher un film
</label>
<input id="movie-search" type="search" class="editorial-field mt-2" />
<button type="button" class="editorial-button mt-4">Rechercher</button>
```

- Use buttons for actions and links for navigation. Keep labels concise, specific and in French, consistent with the product.
- Use `@lucide/vue` icons. Decorative icons beside visible labels use `aria-hidden="true"`; icon-only controls require an accessible name.
- Preserve visible hover, focus, active and disabled states. Shared button classes include disabled opacity and cursor treatment; bind real `disabled` state when an action cannot run.
- Do not remove keyboard focus indicators. Global focus uses an accent ring with canvas offset; account controls use an ink outline. Either established treatment is valid when clearly visible.
- Active navigation uses clear contrast and `aria-current`; hover color alone is insufficient to indicate the current page.
- Prefer color transitions and small purposeful transforms. Honor reduced-motion preferences, including disabling decorative hover movement where applicable.

Do not restore the removed `.field`, `.button-primary` or `.state-panel` soft-style primitives. New reusable variants must remain part of the shared editorial language.

## 8. Cards, imagery and dense data

**Movie cards:** reuse `MovieCatalogCard` and `PosterImage`. Posters use a 2:3 frame, strong border, hard shadow and consistent placeholder. Titles remain prominent; compact runtime/count metadata is subordinate. Keep useful poster alternative text and stable image dimensions. Use existing image components rather than creating inconsistent loading/error fallbacks.

**Tool cards:** keep one clear action per card, bold borders and restrained offset shadows. Avoid additional internal card shells when a row, divider or heading suffices.

**Tables and grids:** favor strong headers, clear alignment and restrained separators. Use mono/bold headers and shared highlight/canvas/surface colors. Preserve useful row density and keep actions discoverable. Adapt third-party widgets with their supported theming API; `AdminMoviesGrid.client.vue` demonstrates AG Grid theme parameters for square controls, ink borders, highlight headers and offset popup shadows. Do not replace functional grid behavior to achieve visual consistency.

## 9. Feedback and dialogs

- Reuse `EditorialStatePanel` for standalone loading, empty or error views. Its `semantic` prop accepts `neutral`, `status` or `alert`; `live` controls announcements, and size/shadow variants adapt existing layouts.
- Use `semantic="status"` for progress or ordinary status feedback and `semantic="alert"` for actionable errors when appropriate. Avoid repeated assertive announcements during polling.
- Distinguish loading, genuinely empty results, failed requests and disabled actions. Keep status copy specific; offer retry or another action when useful.
- Use `editorial-alert` with meaningful text and appropriate semantics for inline errors. Connect field errors to their fields; never rely only on a red border.
- Dialogs follow the same surface, typography and border rules. Keep them within the viewport, provide scrollable content and an accessible close control, preserve focus containment, support appropriate Escape dismissal and restore focus to the trigger.
- Destructive actions retain explicit confirmation and clear labels. A visual refactor must not remove confirmation or alter mutation behavior.

Update this document alongside intentional changes to shared tokens, primitives or design conventions. Do not add a second theme to solve an isolated layout problem.
